package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"htmx-golang-excercise/internal/db"
	"htmx-golang-excercise/internal/passwords"
	sqlite "htmx-golang-excercise/internal/sqlc/sqlite/db"
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

// userContextKey carries the authenticated email through the request context.
type userContextKey struct{}

// userFromContext returns the authenticated user's email set by RequireAuth,
// or "" when the request is not authenticated.
func userFromContext(r *http.Request) string {
	if v, ok := r.Context().Value(userContextKey{}).(string); ok {
		return v
	}
	return ""
}

func setSessionCookie(w http.ResponseWriter, sessionToken string) {
	signedCookie := signValue(sessionToken)
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

// sessionUser validates the signed cookie and checks that its token still
// maps to an active user in the database. The check against the DB is what
// revokes stale cookies: when the SQLite database is deleted and recreated,
// the fresh user row carries a new random session token, so a cookie handed
// out against the old database no longer matches anything.
func (s *Server) sessionUser(r *http.Request) (email string, ok bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}

	token, err := verifySignedValue(cookie.Value)
	if err != nil || token == "" {
		return "", false
	}

	user, err := s.DB.GetUserByToken(r.Context(), token)
	if err != nil || !user.Active {
		return "", false
	}

	return user.Email, true
}

// RequireAuth middleware protects routes.
// If unauthenticated, it redirects normal requests to /login or sets HX-Redirect for HTMX.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, ok := s.sessionUser(r)
		if !ok {
			clearSessionCookie(w)
			s.redirectToLogin(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey{}, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

// -----------------------------------------------------------------------------
// 3. HTTP Auth Handlers
// -----------------------------------------------------------------------------

// GET /login - Displays login form (or redirects home if logged in)
func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.sessionUser(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	Render(w, "login.html", map[string]any{
		"Title":         "Sign In",
		"Authenticated": false,
	})
}

// POST /login - Processes credentials against the database. The stored
// password is a PBKDF2 hash, never plain text.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	wrong := func() {
		// Returns 200, not 401: htmx 2.x treats 4xx/5xx as errors and does
		// not swap the response, so the inline error below would never show.
		Render(w, "login.html", map[string]any{
			"Title":         "Sign In",
			"Authenticated": false,
			"Error":         "Wrong email or password.",
		})
	}

	// The same "Wrong email or password." message is shown both for unknown
	// emails and for bad passwords, so the response does not reveal which
	// addresses exist.
	user, err := s.DB.GetUserByEmail(r.Context(), email)
	if err != nil {
		// Burn roughly the same time verifying a hash as a wrong password
		// would, to keep login timing independent of email existence.
		if h, herr := passwords.Hash(password); herr == nil {
			passwords.Verify(h, password)
		}
		wrong()
		return
	}

	if !user.Active || !passwords.Verify(user.Password, password) {
		wrong()
		return
	}

	// 1. Rotate the session token and store it in the DB, then put the
	// signed token in the cookie. Because the cookie's token must match the
	// user row, deleting the database invalidates every existing session.
	token, err := db.NewSessionToken()
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetUserSessionToken(r.Context(), sqlite.SetUserSessionTokenParams{
		SessionToken: token,
		ID:           user.ID,
	}); err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)

	// 2. Respond based on caller type
	if r.Header.Get("HX-Request") == "true" {
		// HTMX full page render output swap to main dashboard shell
		Render(w, "index.html", map[string]any{
			"Title":         "Dashboard",
			"Authenticated": true,
			"Username":      user.Email,
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
