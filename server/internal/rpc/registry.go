package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
)

// Level says which bridge may run a method.
type Level int

const (
	// User methods run in the user bridge (or the root bridge when the
	// client asks for admin:true).
	User Level = iota
	// Admin methods always run in the root bridge.
	Admin
)

func (l Level) String() string {
	if l == Admin {
		return "admin"
	}
	return "user"
}

// ParseLevel parses "user" / "admin".
func ParseLevel(s string) (Level, bool) {
	switch s {
	case "user":
		return User, true
	case "admin":
		return Admin, true
	}
	return User, false
}

// Handler serves a request/response method.
type Handler func(ctx context.Context, c *Call) (any, error)

// StreamHandler serves a streaming method. Returning nil ends the stream
// normally ("end" event); returning an error ends it with that error.
type StreamHandler func(ctx context.Context, c *Call, s Stream) error

// Stream is the bridge side of a streaming call.
type Stream interface {
	// Send emits one data event. It blocks while the flow-control window is
	// full and fails once the stream is cancelled.
	Send(v any) error
	// SendBytes emits binary data as a base64 string with "b64":true.
	SendBytes(b []byte) error
	// Input delivers input messages sent by the client. It is closed when the
	// stream is cancelled.
	Input() <-chan json.RawMessage
}

// Call is an incoming request.
type Call struct {
	ID     uint64
	Method string
	Params json.RawMessage
	// Admin is true when this bridge is the root bridge.
	Admin bool
}

// Bind decodes the params into v. Missing params decode as {}.
// It returns an Invalid error on malformed input.
func (c *Call) Bind(v any) error {
	p := bytes.TrimSpace(c.Params)
	if len(p) == 0 || bytes.Equal(p, []byte("null")) {
		p = []byte("{}")
	}
	if err := json.Unmarshal(p, v); err != nil {
		return Errorf(Invalid, "invalid params for %s: %v", c.Method, err)
	}
	return nil
}

type method struct {
	level  Level
	call   Handler
	stream StreamHandler
}

// Registry maps method names to handlers.
type Registry struct {
	methods map[string]*method
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{methods: map[string]*method{}} }

// Handle registers a request/response method. It panics on duplicates
// (a programming error caught at start-up).
func (r *Registry) Handle(name string, level Level, h Handler) {
	r.add(name, &method{level: level, call: h})
}

// Stream registers a streaming method.
func (r *Registry) Stream(name string, level Level, h StreamHandler) {
	r.add(name, &method{level: level, stream: h})
}

func (r *Registry) add(name string, m *method) {
	if !ValidMethodName(name) {
		panic(fmt.Sprintf("rpc: invalid method name %q", name))
	}
	if _, dup := r.methods[name]; dup {
		panic(fmt.Sprintf("rpc: method %q registered twice", name))
	}
	r.methods[name] = m
}

// Methods returns every method with its level.
func (r *Registry) Methods() map[string]Level {
	out := make(map[string]Level, len(r.methods))
	for n, m := range r.methods {
		out[n] = m.level
	}
	return out
}

func (r *Registry) hello(admin bool, uid int, version string) *Hello {
	h := &Hello{Version: version, UID: uid, Admin: admin, Methods: map[string]string{}}
	for n, m := range r.methods {
		h.Methods[n] = m.level.String()
		if m.stream != nil {
			h.Streams = append(h.Streams, n)
		}
	}
	sort.Strings(h.Streams)
	return h
}

// ValidMethodName reports whether s looks like "module.method".
func ValidMethodName(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	dot := false
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9', c == '_', c == '-':
			if i == 0 {
				return false
			}
		case c == '.':
			if i == 0 || i == len(s)-1 {
				return false
			}
			dot = true
		default:
			return false
		}
	}
	return dot
}
