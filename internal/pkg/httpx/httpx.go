package httpx

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type HTTPStatusCoder interface {
	HTTPStatusCode() int
}

func IsRetryableHTTPStatus(code int) bool {
	if code == 408 || code == 429 {
		return true
	}
	return code >= 500 && code <= 599
}

func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || netErr.Temporary() {
			return true
		}
	}
	var sc HTTPStatusCoder
	if errors.As(err, &sc) {
		return IsRetryableHTTPStatus(sc.HTTPStatusCode())
	}
	return false
}

func RetryAfterDuration(resp *http.Response, fallback, max time.Duration) time.Duration {
	sleepFor := fallback
	if resp != nil {
		if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
				sleepFor = time.Duration(secs) * time.Second
			}
		}
	}
	if max > 0 && sleepFor > max {
		sleepFor = max
	}
	return sleepFor
}

func JitterSleep(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	j := 0.2
	delta := base.Seconds() * j
	low := base.Seconds() - delta
	high := base.Seconds() + delta
	if low < 0 {
		low = 0
	}
	v := low + rand.Float64()*(high-low)
	return time.Duration(v * float64(time.Second))
}
