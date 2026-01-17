package oai

import (
	"encoding/json"
	"net/http"
	"strings"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, message string, typ string, param string, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = http.StatusText(status)
	}
	t := strings.TrimSpace(typ)
	if t == "" {
		t = "invalid_request_error"
	}

	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error: errorBody{
			Message: msg,
			Type:    t,
			Param:   strings.TrimSpace(param),
			Code:    strings.TrimSpace(code),
		},
	})
}
