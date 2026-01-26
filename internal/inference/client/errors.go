package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrScoreNotConfigured = errors.New("missing NB_INFERENCE_SCORE_MODEL")
var ErrScoreNotSupported = errors.New("text score endpoint not supported")

type HTTPError struct {
	StatusCode int
	Message    string
	Type       string
	Param      string
	Code       string
	Body       string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "http error"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if msg == "" {
		msg = "http error"
	}
	if strings.TrimSpace(e.Code) != "" {
		return fmt.Sprintf("http error: status=%d code=%s message=%s", e.StatusCode, strings.TrimSpace(e.Code), msg)
	}
	return fmt.Sprintf("http error: status=%d message=%s", e.StatusCode, msg)
}

func parseHTTPError(status int, raw []byte) error {
	body := strings.TrimSpace(string(raw))

	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Param   string `json:"param,omitempty"`
			Code    string `json:"code,omitempty"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && strings.TrimSpace(env.Error.Message) != "" {
		return &HTTPError{
			StatusCode: status,
			Message:    strings.TrimSpace(env.Error.Message),
			Type:       strings.TrimSpace(env.Error.Type),
			Param:      strings.TrimSpace(env.Error.Param),
			Code:       strings.TrimSpace(env.Error.Code),
			Body:       body,
		}
	}

	return &HTTPError{
		StatusCode: status,
		Message:    "",
		Type:       "",
		Param:      "",
		Code:       "",
		Body:       body,
	}
}
