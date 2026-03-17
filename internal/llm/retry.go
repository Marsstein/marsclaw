package llm

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// RetryableError indicates the LLM call can be retried.
type RetryableError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string { return e.Message }

// IsRetryable checks if an error should trigger a retry.
func IsRetryable(err error) (*RetryableError, bool) {
	var rErr *RetryableError
	if errors.As(err, &rErr) {
		return rErr, true
	}

	msg := err.Error()
	if strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "timeout") {
		return &RetryableError{Message: msg}, true
	}

	return nil, false
}

// RetryConfig holds retry parameters.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

// WithRetry executes fn with exponential backoff + jitter.
func WithRetry[T any](ctx context.Context, rc RetryConfig, fn func(ctx context.Context) (T, error)) (T, error) {
	var lastErr error
	var zero T

	for attempt := range rc.MaxRetries + 1 {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err
		rErr, retryable := IsRetryable(err)
		if !retryable {
			return zero, err
		}

		if attempt == rc.MaxRetries {
			break
		}

		delay := rc.BaseDelay * time.Duration(math.Pow(2, float64(attempt)))
		if rErr.RetryAfter > 0 && rErr.RetryAfter > delay {
			delay = rErr.RetryAfter
		}

		// Add 0-25% jitter.
		jitter := time.Duration(rand.Int64N(int64(delay) / 4))
		delay += jitter

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}

	return zero, lastErr
}
