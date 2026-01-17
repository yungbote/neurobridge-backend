package oaihttp

import (
	"fmt"
)

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "upstream http error"
	}
	if e.Body == "" {
		return fmt.Sprintf("upstream http error: status=%d", e.StatusCode)
	}
	return fmt.Sprintf("upstream http error: status=%d body=%s", e.StatusCode, e.Body)
}
