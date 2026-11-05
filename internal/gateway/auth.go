package gateway

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"procureflow-ai/internal/domain"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"procureflow-ai/internal/auth"
)

type sessionKey struct{}

func (h *Handler) setCookie(w http.ResponseWriter, token string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: token, Path: "/api", HttpOnly: true, Secure: h.accounts.Secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge})
}
func (h *Handler) authenticateRequest(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if h.accounts == nil {
		writeError(w, 503, "AUTH_NOT_CONFIGURED", "authentication is not configured", false)
		return r, false
	}
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		writeError(w, 401, "UNAUTHENTICATED", "sign in to continue", false)
		return r, false
	}
	session, ok, lookupErr := h.accounts.Resolve(r.Context(), cookie.Value)
	if lookupErr != nil {
		identityError(w, lookupErr)
		return r, false
	}
	if !ok {
		h.setCookie(w, "", time.Unix(0, 0), -1)
		writeError(w, 401, "UNAUTHENTICATED", "session expired; sign in again", false)
		return r, false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			writeError(w, 403, "CSRF_FAILED", "valid session CSRF token required", false)
			return r, false
		}
	}
	claims := auth.Claims{Kind: "actor", Role: session.User.Role, RegisteredClaims: jwt.RegisteredClaims{Subject: session.User.ID}}
	ctx := auth.WithIdentity(r.Context(), claims)
	return r.WithContext(context.WithValue(ctx, sessionKey{}, session)), true
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if h.accounts == nil {
		writeError(w, 503, "AUTH_NOT_CONFIGURED", "local accounts are not configured", false)
		return
	}
	input, ok := decodeJSON[struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}](w, r, min(h.maxBody, 4096))
	if !ok {
		return
	}
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if len(username) < 3 || len(username) > 80 || len(input.Password) < 1 || len(input.Password) > 128 {
		writeError(w, 400, "INVALID_ARGUMENT", "username and password are required and must fit their length limits", false)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !h.accounts.AllowLogin(ip, username) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "LOGIN_RATE_LIMITED", "too many sign-in attempts; wait a minute", true)
		return
	}
	user, ok, authErr := h.accounts.AuthenticateContext(r.Context(), username, input.Password)
	if authErr != nil {
		identityError(w, authErr)
		return
	}
	if !ok {
		writeError(w, 401, "INVALID_CREDENTIALS", "invalid username or password", false)
		return
	}
	token, session, err := h.accounts.Create(user)
	if err != nil {
		writeError(w, 503, "AUTH_UNAVAILABLE", "unable to create a session", true)
		return
	}
	if previous, err := r.Cookie(auth.CookieName); err == nil {
		h.accounts.Revoke(previous.Value)
	}
	h.setCookie(w, token, session.Expires, int(h.accounts.TTL.Seconds()))
	writeJSON(w, 200, map[string]any{"user": user, "csrf_token": session.CSRF, "expires_at": session.Expires})
}
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey{}).(auth.Session)
	writeJSON(w, 200, map[string]any{"user": s.User, "csrf_token": s.CSRF, "expires_at": s.Expires})
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(auth.CookieName)
	h.accounts.Revoke(cookie.Value)
	h.setCookie(w, "", time.Unix(0, 0), -1)
	writeJSON(w, 200, map[string]bool{"signed_out": true})
}

func identityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		upstreamError(w, err)
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, 400, "INVALID_ARGUMENT", "invalid username, name, password or role; check the field requirements", false)
	case errors.Is(err, domain.ErrExists):
		writeError(w, 409, "USERNAME_EXISTS", "username is already registered", false)
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, 404, "NOT_FOUND", "user not found", false)
	case errors.Is(err, auth.ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "admin role required", false)
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, 409, "LAST_ADMIN", "the last admin cannot be demoted", false)
	default:
		writeError(w, 503, "AUTH_UNAVAILABLE", "identity storage or password processing unavailable; retry shortly", true)
	}
}
func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	if h.accounts == nil {
		writeError(w, 503, "AUTH_NOT_CONFIGURED", "authentication is not configured", false)
		return
	}
	input, ok := decodeJSON[struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}](w, r, min(h.maxBody, 4096))
	if !ok {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !h.accounts.AllowLogin("signup:"+ip, "signup:"+auth.NormalizeUsername(input.Username)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "SIGNUP_RATE_LIMITED", "too many signup attempts; wait a minute", true)
		return
	}
	user, err := h.accounts.Register(r.Context(), input.Username, input.Name, input.Password)
	if err != nil {
		identityError(w, err)
		return
	}
	// Registration and sign-in are separate, avoiding an ambiguous session failure after creation.
	writeJSON(w, 201, map[string]any{"user": user})
}
func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	actor, ok := auth.Identity(r.Context())
	if !ok || actor.Role != auth.Admin {
		writeError(w, 403, "FORBIDDEN", "admin role required", false)
		return false
	}
	return true
}
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	users, err := h.accounts.Users.List(r.Context())
	if err != nil {
		identityError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users})
}
func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	id := r.PathValue("id")
	if !validID(w, "user_id", id) {
		return
	}
	input, ok := decodeJSON[struct {
		Role string `json:"role"`
	}](w, r, min(h.maxBody, 4096))
	if !ok {
		return
	}
	if !auth.ValidRole(input.Role) {
		identityError(w, domain.ErrInvalid)
		return
	}
	actor, _ := auth.Identity(r.Context())
	user, err := h.accounts.Users.ChangeRole(r.Context(), actor.Subject, id, input.Role)
	if err != nil {
		identityError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"user": user})
}
