// Package ai contains provider-agnostic HTTP behavior for AI integrations.
package ai

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultAttempts = 3

type RetryOptions struct {
	MaxAttempts int
}

// Do sends a request and retries only responses that the provider explicitly
// marks as temporary. The request body must be replayable through GetBody.
func Do(ctx context.Context, client *http.Client, request *http.Request, options RetryOptions) (*http.Response, error) {
	attempts := options.MaxAttempts
	if attempts <= 0 {
		attempts = defaultAttempts
	}
	if request.GetBody == nil && request.Body != nil && request.Body != http.NoBody {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		attemptRequest, err := requestForAttempt(ctx, request)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(attemptRequest)
		if err != nil || !retryable(response) || attempt == attempts-1 {
			return response, err
		}
		delay := retryDelay(response, attempt)
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		response.Body.Close()
		if err := wait(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, context.Canceled
}

func requestForAttempt(ctx context.Context, request *http.Request) (*http.Request, error) {
	copy := request.Clone(ctx)
	if request.GetBody == nil {
		return copy, nil
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	copy.Body = body
	return copy, nil
}

func retryable(response *http.Response) bool {
	if response == nil {
		return false
	}
	return response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
}

func retryDelay(response *http.Response, attempt int) time.Duration {
	retryAfter := strings.TrimSpace(response.Header.Get("Retry-After"))
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(retryAfter); err == nil {
		return max(time.Until(retryAt), 0)
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
