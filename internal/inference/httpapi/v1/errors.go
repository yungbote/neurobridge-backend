package v1

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
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, message string, code string, param string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = http.StatusText(status)
	}

	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error: errorBody{
			Message: msg,
			Code:    strings.TrimSpace(code),
			Param:   strings.TrimSpace(param),
		},
	})
}
