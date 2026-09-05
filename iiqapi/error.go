// Package iiqapi contains shared behavior for IIQ HTTP APIs.
package iiqapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

type ErrorKind string

const (
	ErrorRequest   ErrorKind = "request"
	ErrorTransport ErrorKind = "transport"
	ErrorTimeout   ErrorKind = "timeout"
	ErrorStatus    ErrorKind = "status"
	ErrorBodyRead  ErrorKind = "body_read"
	ErrorParse     ErrorKind = "parse"
)

type Error struct {
	Kind            ErrorKind
	Status          int
	ResponseSnippet string
	Err             error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	kind := string(e.Kind)
	if kind == "" {
		kind = "unknown"
	}
	if e.Err == nil {
		return kind
	}
	return fmt.Sprintf("%s: %v", kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func ErrorLabels(err error) (kind, status string) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		if errors.Is(err, context.DeadlineExceeded) {
			return string(ErrorTimeout), ""
		}
		return string(ErrorTransport), ""
	}
	if apiErr.Status != 0 {
		status = strconv.Itoa(apiErr.Status)
	}
	return string(apiErr.Kind), status
}
