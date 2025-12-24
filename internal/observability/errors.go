package observability

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/baxromumarov/job-hunter/internal/httpx"
)

const (
	ErrorNetwork   = "network"
	ErrorParsing   = "parsing"
	ErrorAI        = "ai"
	ErrorRateLimit = "rate_limit"
	ErrorStore     = "store"
	ErrorUnknown   = "unknown"
)

func ClassifyFetchError(err error) string {
	if err == nil {
		return ErrorUnknown
	}
	var fe *httpx.FetchError
	if errors.As(err, &fe) {
		switch {
		case fe.Status == http.StatusTooManyRequests:
			return ErrorRateLimit
		case fe.Status >= 500:
			return ErrorNetwork
		default:
			return ErrorNetwork
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorNetwork
	}
	return ErrorUnknown
}

func ClassifyScrapeError(err error) string {
	if err == nil {
		return ErrorUnknown
	}
	if kind := ClassifyFetchError(err); kind != ErrorUnknown {
		return kind
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "parse failed") ||
		strings.Contains(msg, "decode failed") ||
		strings.Contains(msg, "unmarshal") ||
		strings.Contains(msg, "invalid character") {
		return ErrorParsing
	}
	return ErrorNetwork
}
