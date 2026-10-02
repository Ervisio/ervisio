package server

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// statusFor maps an rpc error code to an HTTP status.
func statusFor(code rpc.Code) int {
	switch code {
	case rpc.Unauthenticated:
		return http.StatusUnauthorized
	case rpc.NeedsAdmin, rpc.Forbidden:
		return http.StatusForbidden
	case rpc.NotFound:
		return http.StatusNotFound
	case rpc.Invalid:
		return http.StatusBadRequest
	case rpc.Conflict:
		return http.StatusConflict
	case rpc.Unavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Error *rpc.Error `json:"error"`
}

// writeError writes {"error":{code,message,data?}} with a matching status.
func writeError(w http.ResponseWriter, e *rpc.Error) {
	writeJSON(w, statusFor(e.Code), errorBody{Error: e})
}

func writeErrorStatus(w http.ResponseWriter, status int, e *rpc.Error) {
	writeJSON(w, status, errorBody{Error: e})
}

// decodeJSON reads a JSON body of at most limit bytes into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) *rpc.Error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return rpc.Errorf(rpc.Invalid, "Content-Type must be application/json")
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return rpc.Errorf(rpc.Invalid, "request body too large")
		}
		if errors.Is(err, io.EOF) {
			return rpc.Errorf(rpc.Invalid, "empty request body")
		}
		return rpc.Errorf(rpc.Invalid, "invalid JSON: %v", err)
	}
	if dec.More() {
		return rpc.Errorf(rpc.Invalid, "trailing data after JSON body")
	}
	return nil
}

// clientIP is the peer address (no proxy headers are trusted).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// securityHeaders sets headers that apply to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
