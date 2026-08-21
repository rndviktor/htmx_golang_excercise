package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName = "htmxgolangexcercise_session"
	// Replace with a secure random key loaded from your app config/ENV in production
	sessionSecretKey = "super-secret-key-change-me-in-production"
)

var (
	ErrInvalidSession = errors.New("invalid or tampered session cookie")
)

// -----------------------------------------------------------------------------
// 1. Session Helper Functions (Signed Cookies)
// -----------------------------------------------------------------------------

// signValue creates an HMAC-SHA256 signature for a cookie string
func signValue(value string) string {
	h := hmac.New(sha256.New, []byte(sessionSecretKey))
	h.Write([]byte(value))
	signature := h.Sum(nil)

	encodedSig := base64.RawURLEncoding.EncodeToString(signature)
	return fmt.Sprintf("%s|%s", value, encodedSig)
}

// verifySignedValue checks an HMAC signature and extracts the original value
func verifySignedValue(signedValue string) (string, error) {
	// 1. Fixed delimiter: Split on "|" instead of "."
	parts := strings.Split(signedValue, "|")
	if len(parts) != 2 {
		return "", ErrInvalidSession
	}

	value, signature := parts[0], parts[1]

	// 2. Compute expected signature using Write()
	h := hmac.New(sha256.New, []byte(sessionSecretKey))
	h.Write([]byte(value))
	expectedBytes := h.Sum(nil)

	// 3. Fixed encoding: Use RawURLEncoding to match signValue
	expectedSignature := base64.RawURLEncoding.EncodeToString(expectedBytes)

	// 4. Constant time comparison to prevent timing attacks
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return "", ErrInvalidSession
	}

	return value, nil
}

func setSessionCookie(w http.ResponseWriter, username string) {
	signedCookie := signValue(username)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    signedCookie,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // 1. Prevents transmission over unencrypted HTTP
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400, // 2. Ensures modern browser persistent behavior (24 hours)
		Expires:  time.Now().Add(24 * time.Hour),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

// -----------------------------------------------------------------------------
// 2. Authentication Middleware
// -----------------------------------------------------------------------------

// RequireAuth middleware protects routes.
// If unauthenticated, it redirects normal requests to /login or sets HX-Redirect for HTMX.
func (s *Server) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			s.redirectToLogin(w, r)
			return
		}

		username, err := verifySignedValue(cookie.Value)
		if err != nil || username == "" {
			clearSessionCookie(w)
			s.redirectToLogin(w, r)
			return
		}

		// Valid session; continue request
		next(w, r)
	}
}

// redirectToLogin handles redirection for standard HTTP requests vs HTMX requests
func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// If the request was triggered by HTMX, send HX-Redirect header
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Standard browser navigation redirect
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// GetAuthenticatedUser extracts username from request cookie if present
func GetAuthenticatedUser(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	username, err := verifySignedValue(cookie.Value)
	if err != nil {
		return ""
	}
	return username
}

// -----------------------------------------------------------------------------
// 3. HTTP Auth Handlers
// -----------------------------------------------------------------------------

// GET /login - Displays login form (or redirects home if logged in)
func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if username := GetAuthenticatedUser(r); username != "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	Render(w, "login.html", map[string]any{
		"Title":         "Sign In",
		"Authenticated": false,
	})
}

// POST /login - Processes credentials
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	// Demo authentication check (Replace with real user store validation)
	if username != "admin" || password != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		Render(w, "login.html", map[string]any{
			"Title":         "Sign In",
			"Authenticated": false,
			"Error":         "Invalid username or password.",
		})
		return
	}

	// 1. Create session cookie
	setSessionCookie(w, username)

	// 2. Respond based on caller type
	if r.Header.Get("HX-Request") == "true" {
		// HTMX full page render output swap to main dashboard shell
		Render(w, "index.html", map[string]any{
			"Title":         "Dashboard",
			"Authenticated": true,
			"Username":      username,
		})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// POST /logout - Clears session cookie
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)

	if r.Header.Get("HX-Request") == "true" {
		Render(w, "login.html", map[string]any{
			"Title":         "Sign In",
			"Authenticated": false,
		})
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
