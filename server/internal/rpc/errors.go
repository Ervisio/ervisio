package rpc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// Code is a machine-readable error code shared by the bridge, the daemon and
// the web client.
type Code string

// Error codes. The web client reacts to NeedsAdmin by offering to unlock
// administrator rights and retrying the call on the root bridge.
const (
	NeedsAdmin      Code = "needs_admin"
	Forbidden       Code = "forbidden"
	NotFound        Code = "not_found"
	Invalid         Code = "invalid"
	Conflict        Code = "conflict"
	Unavailable     Code = "unavailable"
	Internal        Code = "internal"
	Unauthenticated Code = "unauthenticated"
)

// Error is the typed error carried on the wire.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// Errorf builds an *Error with a formatted message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WithData returns a copy of e carrying extra structured data.
func (e *Error) WithData(data any) *Error {
	cp := *e
	cp.Data = data
	return &cp
}

// IsCode reports whether err is an *Error with the given code.
func IsCode(err error, code Code) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// ToError converts any error into an *Error. Filesystem permission errors map
// to NeedsAdmin on a user bridge (so the client can retry as administrator)
// and to Forbidden on the root bridge.
func ToError(err error, admin bool) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	switch {
	case errors.As(err, &e):
		return e
	case errors.Is(err, fs.ErrPermission):
		if admin {
			return &Error{Code: Forbidden, Message: err.Error()}
		}
		return &Error{Code: NeedsAdmin, Message: err.Error()}
	case errors.Is(err, fs.ErrNotExist):
		return &Error{Code: NotFound, Message: err.Error()}
	case errors.Is(err, fs.ErrExist):
		return &Error{Code: Conflict, Message: err.Error()}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: Unavailable, Message: err.Error()}
	default:
		return &Error{Code: Internal, Message: err.Error()}
	}
}
