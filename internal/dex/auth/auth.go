// Package auth provides basic authentication helpers for HTTP and WebSocket endpoints.
package auth

import (
	"net/http"
	"strings"

	"github.com/AnuBookDEX/engine/internal/infra/common"
	"github.com/AnuBookDEX/engine/internal/infra/config"
)

// Middleware returns an HTTP middleware that validates a Bearer token or API key
// against the configured auth token. If auth is not enabled (no token configured),
// all requests pass through.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		token := extractToken(r)
		expected := config.GetString("auth.token", "")
		if token == "" || token != expected {
			common.Warn("auth: rejected unauthenticated request from", r.RemoteAddr)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MiddlewareFunc is the same as Middleware but accepts http.HandlerFunc directly.
func MiddlewareFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isEnabled() {
			next(w, r)
			return
		}

		token := extractToken(r)
		expected := config.GetString("auth.token", "")
		if token == "" || token != expected {
			common.Warn("auth: rejected unauthenticated request from", r.RemoteAddr)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// ValidateToken checks a token for WebSocket authentication.
func ValidateToken(token string) bool {
	if !isEnabled() {
		return true
	}
	expected := config.GetString("auth.token", "")
	return token == expected
}

func isEnabled() bool {
	return config.GetString("auth.token", "") != ""
}

func extractToken(r *http.Request) string {
	// Try Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
		return authHeader
	}

	// Fall back to query parameter
	return r.URL.Query().Get("token")
}
