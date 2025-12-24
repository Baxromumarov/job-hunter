package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
	"golang.org/x/time/rate"
)

// PoliteClient enforces per-host rate limits, robots.txt rules, and polite retries.
type PoliteClient struct {
	client      *http.Client
	ua          string
	limiters    map[string]*rate.Limiter
	robotsCache map[string]*robotstxt.RobotsData
	mu          sync.Mutex
}

func NewPoliteClient(userAgent string) *PoliteClient {
	return &PoliteClient{
		client:      &http.Client{Timeout: 15 * time.Second},
		ua:          userAgent,
		limiters:    map[string]*rate.Limiter{},
		robotsCache: map[string]*robotstxt.RobotsData{},
	}
}

func (p *PoliteClient) limiterFor(host string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.limiters[host]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Every(time.Second), 2) // 1 req/s, burst 2
	p.limiters[host] = l
	return l
}

// NewRequest builds an HTTP GET request with context and a safe URL defaulting to https.
func NewRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	if rawURL == "" {
		return nil, errors.New("empty url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
}

func (p *PoliteClient) robotsFor(ctx context.Context, u *url.URL) (*robotstxt.RobotsData, error) {
	host := u.Hostname()
	p.mu.Lock()
	if data, ok := p.robotsCache[host]; ok {
		p.mu.Unlock()
		return data, nil
	}
	p.mu.Unlock()

	robotsURL := fmt.Sprintf("%s://%s/robots.txt", u.Scheme, u.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.ua)

	if err := p.limiterFor(host).Wait(ctx); err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.robotsCache[host] = data
	p.mu.Unlock()
	return data, nil
}

// Do executes the request respecting robots.txt and rate limits.
func (p *PoliteClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", p.ua)
	}

	u := req.URL
	if u.Scheme == "" {
		u.Scheme = "https"
	}

	if ok := p.allowed(ctx, u, req.Method); !ok {
		return nil, fmt.Errorf("blocked by robots.txt: %s", u)
	}

	host := u.Hostname()
	limiter := p.limiterFor(host)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}

		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = fmt.Errorf("retryable status %d", resp.StatusCode)
			resp.Body.Close()
			backoff := time.Duration(500*(1<<attempt)) * time.Millisecond
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		return resp, nil
	}

	if lastErr == nil {
		lastErr = errors.New("polite client: failed without error")
	}
	return nil, lastErr
}

func (p *PoliteClient) allowed(ctx context.Context, u *url.URL, method string) bool {
	data, err := p.robotsFor(ctx, u)
	if err != nil {
		return true // fail open to avoid blocking everything
	}
	ua := p.ua
	group := data.FindGroup(ua)
	if group == nil {
		group = data.FindGroup("*")
	}
	if group == nil {
		return true
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	if !group.Test(path) {
		return false
	}
	// Disallow POST/PUT even if robots allows – we only read.
	if !strings.EqualFold(method, http.MethodGet) && !strings.EqualFold(method, http.MethodHead) {
		return false
	}
	return true
}
