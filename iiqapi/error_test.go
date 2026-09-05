package iiqapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestError(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{name: "kind and cause", err: &Error{Kind: ErrorStatus, Err: cause}, want: "status: boom"},
		{name: "kind only", err: &Error{Kind: ErrorStatus}, want: "status"},
		{name: "unknown kind", err: &Error{}, want: "unknown"},
		{name: "nil receiver", err: nil, want: "<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}

	wrapped := &Error{Kind: ErrorParse, Err: cause}
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped cause is not reachable through errors.Is")
	}
}

func TestErrorLabels(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantKind   string
		wantStatus string
	}{
		{
			name:       "typed status error",
			err:        &Error{Kind: ErrorStatus, Status: 500, Err: errors.New("failed")},
			wantKind:   "status",
			wantStatus: "500",
		},
		{
			name:     "wrapped typed error",
			err:      fmt.Errorf("outer: %w", &Error{Kind: ErrorParse, Err: errors.New("invalid")}),
			wantKind: "parse",
		},
		{
			name:     "wrapped deadline",
			err:      fmt.Errorf("outer: %w", context.DeadlineExceeded),
			wantKind: "timeout",
		},
		{
			name:     "unknown error",
			err:      errors.New("failed"),
			wantKind: "transport",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, status := ErrorLabels(tt.err)
			if kind != tt.wantKind || status != tt.wantStatus {
				t.Fatalf("ErrorLabels() = (%q, %q), want (%q, %q)", kind, status, tt.wantKind, tt.wantStatus)
			}
		})
	}
}
