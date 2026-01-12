package apierr

import "fmt"

type Error struct {
	Status int
	Code   string
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Code != "" {
		return e.Code
	}
	if e.Status != 0 {
		return fmt.Sprintf("api error (%d)", e.Status)
	}
	return "api error"
}

func (e *Error) Unwrap() error { return e.Err }

func New(status int, code string, err error) *Error {
	return &Error{Status: status, Code: code, Err: err}
}
