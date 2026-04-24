package azure

import (
	"bytes"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// doWithRetry wraps an HTTP call with backoff on 429 / 503 / transient 5xx.
// Azure ARM and Cost Management can rate-limit aggressively when a user
// navigates subscriptions — retrying transparently avoids surfacing a
// transient "too many requests" error that would make the TUI feel brittle.
//
// Attempts: up to 3. Backoff: honors Retry-After when present, else
// exponential with full jitter.
func doWithRetry(req *http.Request) (*http.Response, error) {
	const maxAttempts = 3
	var (
		resp *http.Response
		err  error
	)

	// If the body is non-nil, capture it so we can replay on retry.
	var body []byte
	if req.Body != nil {
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err = httpClient.Do(req)
		if err != nil {
			// Network errors: one retry with a short pause, then give up.
			if attempt < maxAttempts-1 {
				sleepCtx(req, backoff(attempt, ""))
				continue
			}
			return nil, err
		}
		if !shouldRetry(resp.StatusCode) {
			return resp, nil
		}
		// Drain and close so the connection can be reused.
		retryAfter := resp.Header.Get("Retry-After")
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if attempt == maxAttempts-1 {
			// Re-issue one final time so the caller sees the last response
			// (with its status + body) instead of a drained one.
			if body != nil {
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
			return httpClient.Do(req)
		}
		sleepCtx(req, backoff(attempt, retryAfter))
	}
	return resp, err
}

func shouldRetry(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusBadGateway ||
		code == http.StatusGatewayTimeout
}

// backoff returns a wait duration. Prefers the server-hinted Retry-After
// (seconds); falls back to exponential 0.5s, 1s, 2s with ±25% jitter.
func backoff(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if s, err := strconv.Atoi(retryAfter); err == nil {
			return time.Duration(s) * time.Second
		}
	}
	base := time.Duration(500*(1<<attempt)) * time.Millisecond
	jitter := time.Duration(rand.Int63n(int64(base / 2)))
	return base + jitter - base/4
}

// sleepCtx waits d or returns early when the request context is cancelled,
// so retries never survive a user-initiated quit.
func sleepCtx(req *http.Request, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	if ctx := req.Context(); ctx != nil {
		select {
		case <-t.C:
		case <-ctx.Done():
		}
		return
	}
	<-t.C
}
