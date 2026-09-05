// Package iiqapi contains shared behavior for IIQ HTTP APIs.
package iiqapi

import (
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
	if e.Err == nil {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func ErrorLabels(err error) (kind, status string) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return string(ErrorTransport), ""
	}
	if apiErr.Status != 0 {
		status = strconv.Itoa(apiErr.Status)
	}
	return string(apiErr.Kind), status
}
