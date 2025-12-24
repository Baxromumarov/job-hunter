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

	"github.com/gocolly/colly/v2"
	"golang.org/x/time/rate"
)

// CollyFetcher wraps Colly for polite HTML fetching and CSS-based parsing.
type CollyFetcher struct {
	userAgent    string
	timeout      time.Duration
	mu           sync.Mutex
	defaultRate  rate.Limit
	defaultBurst int
	hosts        map[string]*hostPolicy
}

type hostPolicy struct {
	limiter     *rate.Limiter
	nextAllowed time.Time
	mu          sync.Mutex
}

type FetchError struct {
	Status int
	Err    error
}

func (e *FetchError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("fetch error (status %d)", e.Status)
	}
	return fmt.Sprintf("fetch error (status %d): %v", e.Status, e.Err)
}

func (e *FetchError) Unwrap() error {
	return e.Err
}

func NewCollyFetcher(userAgent string) *CollyFetcher {
	if userAgent == "" {
		userAgent = "job-hunter-bot/1.0"
	}
	return &CollyFetcher{
		userAgent:    userAgent,
		timeout:      15 * time.Second,
		defaultRate:  rate.Every(time.Second),
		defaultBurst: 2,
		hosts:        make(map[string]*hostPolicy),
	}
}

func (f *CollyFetcher) SetHostLimit(host string, per time.Duration, burst int) {
	if host == "" || per <= 0 || burst <= 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := normalizeHost(host)
	policy := f.getOrCreatePolicyLocked(key)
	policy.mu.Lock()
	policy.limiter = rate.NewLimiter(rate.Every(per), burst)
	policy.mu.Unlock()
}

func (f *CollyFetcher) Fetch(ctx context.Context, rawURL string, register func(*colly.Collector)) error {
	_, err := f.fetchWithRetry(ctx, rawURL, register)
	return err
}

func (f *CollyFetcher) FetchBytes(ctx context.Context, rawURL string) ([]byte, int, error) {
	var body []byte
	status, err := f.fetchWithRetry(ctx, rawURL, func(c *colly.Collector) {
		c.OnResponse(func(r *colly.Response) {
			body = append([]byte(nil), r.Body...)
		})
	})
	return body, status, err
}

func (f *CollyFetcher) fetchWithRetry(ctx context.Context, rawURL string, register func(*colly.Collector)) (int, error) {
	target, err := normalizeURL(rawURL)
	if err != nil {
		return 0, err
	}
	host := hostKey(target)

	var lastErr error
	var status int
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if err := f.waitForHost(ctx, host); err != nil {
			return 0, err
		}
		status, lastErr = f.fetchOnce(ctx, target, register)
		if lastErr == nil {
			return status, nil
		}
		if shouldBackoff(status) {
			f.applyBackoff(host, attempt)
			continue
		}
		return status, lastErr
	}

	if lastErr == nil {
		lastErr = errors.New("colly fetch failed")
	}
	return status, &FetchError{Status: status, Err: lastErr}
}

func (f *CollyFetcher) fetchOnce(ctx context.Context, target string, register func(*colly.Collector)) (int, error) {
	c := f.newCollector()
	if register != nil {
		register(c)
	}

	status := 0
	var reqErr error
	c.OnResponse(func(r *colly.Response) {
		status = r.StatusCode
	})
	c.OnError(func(r *colly.Response, err error) {
		if r != nil {
			status = r.StatusCode
		}
		reqErr = err
	})

	collyCtx := colly.NewContext()
	collyCtx.Put("ctx", ctx)

	if err := c.Request(http.MethodGet, target, nil, collyCtx, nil); err != nil {
		return status, err
	}
	if reqErr != nil {
		return status, reqErr
	}
	if status >= 400 {
		return status, fmt.Errorf("status %d", status)
	}
	if status == 0 {
		status = http.StatusOK
	}
	return status, nil
}

func (f *CollyFetcher) newCollector() *colly.Collector {
	c := colly.NewCollector(colly.UserAgent(f.userAgent))
	c.IgnoreRobotsTxt = false
	c.SetRequestTimeout(f.timeout)

	c.OnRequest(func(r *colly.Request) {
		ctx := context.Background()
		if v := r.Ctx.GetAny("ctx"); v != nil {
			if reqCtx, ok := v.(context.Context); ok {
				ctx = reqCtx
			}
		}
		if ctx.Err() != nil {
			r.Abort()
		}
	})

	return c
}

func (f *CollyFetcher) waitForHost(ctx context.Context, host string) error {
	policy := f.hostPolicy(host)
	if err := policy.waitBackoff(ctx); err != nil {
		return err
	}
	return policy.limiter.Wait(ctx)
}

func (f *CollyFetcher) hostPolicy(host string) *hostPolicy {
	key := normalizeHost(host)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getOrCreatePolicyLocked(key)
}

func (f *CollyFetcher) getOrCreatePolicyLocked(host string) *hostPolicy {
	if host == "" {
		host = "default"
	}
	if policy, ok := f.hosts[host]; ok {
		return policy
	}
	policy := &hostPolicy{
		limiter: rate.NewLimiter(f.defaultRate, f.defaultBurst),
	}
	f.hosts[host] = policy
	return policy
}

func (f *CollyFetcher) applyBackoff(host string, attempt int) {
	if attempt < 0 {
		attempt = 0
	}
	policy := f.hostPolicy(host)
	delay := time.Duration(500*(1<<attempt)) * time.Millisecond
	policy.mu.Lock()
	next := time.Now().Add(delay)
	if next.After(policy.nextAllowed) {
		policy.nextAllowed = next
	}
	policy.mu.Unlock()
}

func normalizeURL(rawURL string) (string, error) {
	if rawURL == "" {
		return "", errors.New("empty url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	return u.String(), nil
}

func normalizeHost(host string) string {
	host = strings.ToLower(host)
	host = strings.TrimPrefix(host, "www.")
	return host
}

func hostKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "default"
	}
	return normalizeHost(u.Hostname())
}

func shouldBackoff(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 && status <= 599 {
		return true
	}
	return false
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *hostPolicy) waitBackoff(ctx context.Context) error {
	for {
		p.mu.Lock()
		next := p.nextAllowed
		p.mu.Unlock()
		now := time.Now()
		if !now.Before(next) {
			return nil
		}
		if err := sleepWithContext(ctx, next.Sub(now)); err != nil {
			return err
		}
	}
}
