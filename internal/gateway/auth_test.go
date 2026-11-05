package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"procureflow-ai/internal/auth"
	"strings"
	"testing"
	"time"
)

func TestLocalAuthenticationAndCSRF(t *testing.T) {
	h := testHandler(fakePurchases{}, nil)
	hash, err := auth.HashPassword("fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	h.accounts = auth.NewSessions([]auth.Account{{User: auth.User{ID: "emp-101", Username: "employee.demo", Name: "Demo", Role: auth.Employee}, PasswordHash: hash}}, true)
	call := func(method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/healthz", "", nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/api/purchases", "", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous=%d", w.Code)
	}
	if w := call("POST", "/api/auth/login", `{"username":"employee.demo","password":"wrong"}`, nil, ""); w.Code != 401 {
		t.Fatal("bad password accepted")
	}
	w := call("POST", "/api/auth/login", `{"username":"employee.demo","password":"fictional-test-password"}`, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api" {
		t.Fatalf("unsafe cookie flags: %+v", cookie)
	}
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "password_hash") || strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("session secret exposed")
	}
	if w := call("GET", "/api/auth/me", "", cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, csrf := range []string{"", "forged"} {
		if w := call("POST", "/api/purchases", createBody, cookie, csrf); w.Code != 403 {
			t.Fatal("CSRF accepted")
		}
	}
	if w := call("POST", "/api/purchases", strings.Replace(createBody, "emp-101", "emp-other", 1), cookie, session.CSRF); w.Code != 403 {
		t.Fatal("forged employee accepted")
	}
	if w := call("POST", "/api/purchases/purchase-demo/decision", decisionBody, cookie, session.CSRF); w.Code != 403 {
		t.Fatal("employee decision accepted")
	}
	if w := call("POST", "/api/auth/logout", `{}`, cookie, session.CSRF); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/api/auth/me", "", cookie, ""); w.Code != 401 {
		t.Fatal("revoked cookie accepted")
	}
	token, _, _ := h.accounts.Create(auth.User{ID: "emp-101", Role: auth.Employee})
	now := time.Now()
	h.accounts.Now = func() time.Time { return now.Add(9 * time.Hour) }
	if w := call("GET", "/api/auth/me", "", &http.Cookie{Name: auth.CookieName, Value: token}, ""); w.Code != 401 {
		t.Fatal("expired cookie accepted")
	}
}
