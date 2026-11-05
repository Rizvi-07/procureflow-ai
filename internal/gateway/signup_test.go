package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"procureflow-ai/internal/auth"
	"strings"
	"testing"
)

func TestSignupAndAdminRoleEndpoints(t *testing.T) {
	h := testHandler(fakePurchases{}, nil)
	admin := auth.User{ID: "admin-demo", Username: "admin.demo", Name: "Admin", Role: auth.Admin}
	h.accounts = auth.NewSessions([]auth.Account{{User: admin}}, false)
	adminToken, adminSession, _ := h.accounts.Create(admin)
	call := func(method, path, body, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	signup := `{"username":"employee.new","name":"New Employee","password":"fictional-test-password"}`
	for _, bad := range []string{strings.Replace(signup, `"employee.new"`, `"ab"`, 1), strings.Replace(signup, "fictional-test-password", "short", 1), strings.Replace(signup, `"name":`, `"role":"admin","name":`, 1), strings.Replace(signup, `"name":`, `"id":"admin-demo","name":`, 1)} {
		if w := call("POST", "/api/auth/signup", bad, "", ""); w.Code != 400 {
			t.Fatalf("invalid signup HTTP %d", w.Code)
		}
	}
	w := call("POST", "/api/auth/signup", signup, "", "")
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		User auth.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User.Role != auth.Employee || result.User.ID == "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("unsafe signup result")
	}
	if w := call("POST", "/api/auth/signup", strings.Replace(signup, "employee.new", "EMPLOYEE.NEW", 1), "", ""); w.Code != 409 {
		t.Fatal("duplicate signup accepted")
	}
	login := call("POST", "/api/auth/login", `{"username":"employee.new","password":"fictional-test-password"}`, "", "")
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	token := login.Result().Cookies()[0].Value
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(login.Body.Bytes(), &session)
	if w := call("GET", "/api/admin/users", "", token, ""); w.Code != 403 {
		t.Fatal("employee listed users")
	}
	route := "/api/admin/users/" + result.User.ID + "/role"
	if w := call("POST", route, `{"role":"admin"}`, token, session.CSRF); w.Code != 403 {
		t.Fatal("employee self-escalated")
	}
	if w := call("POST", route, `{"role":"manager"}`, adminToken, ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	for _, bad := range []string{`{"role":"owner"}`, `{"role":"manager","password":"unexpected"}`} {
		if w := call("POST", route, bad, adminToken, adminSession.CSRF); w.Code != 400 {
			t.Fatal("invalid role update accepted")
		}
	}
	if w := call("POST", route, `{"role":"manager"}`, adminToken, adminSession.CSRF); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("GET", "/api/auth/me", "", token, ""); w.Code != 401 {
		t.Fatal("old-role session remained valid")
	}
	if w := call("POST", "/api/admin/users/admin-demo/role", `{"role":"employee"}`, adminToken, adminSession.CSRF); w.Code != 409 {
		t.Fatal("last admin was demoted")
	}
	if w := call("POST", "/api/admin/users/missing/role", `{"role":"employee"}`, adminToken, adminSession.CSRF); w.Code != 404 {
		t.Fatal("missing user not 404")
	}
	w = call("GET", "/api/admin/users", "", adminToken, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "version") {
		t.Fatal("unsafe users list", w.Body.String())
	}
}
