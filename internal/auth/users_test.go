package auth

import (
	"context"
	"errors"
	"procureflow-ai/internal/domain"
	"testing"
)

func TestSignupRolesAndSessionRevocation(t *testing.T) {
	ctx := context.Background()
	admin := User{ID: "admin-demo", Username: "admin.demo", Name: "Admin", Role: Admin}
	s := NewSessions([]Account{{User: admin}}, false)
	for _, in := range []struct{ username, name, password string }{{"ab", "Demo", "fictional-test-password"}, {"valid.name", " ", "fictional-test-password"}, {"valid.name", "Demo", "short"}, {"bad name", "Demo", "fictional-test-password"}} {
		if _, err := s.Register(ctx, in.username, in.name, in.password); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid signup: %v", err)
		}
	}
	user, err := s.Register(ctx, " Employee.New ", " New Employee ", "fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != Employee || user.Username != "employee.new" || user.ID == "" || user.Name != "New Employee" {
		t.Fatalf("unexpected new identity: %+v", user)
	}
	if _, err := s.Register(ctx, "EMPLOYEE.NEW", "Demo", "fictional-test-password"); !errors.Is(err, domain.ErrExists) {
		t.Fatal("case-insensitive duplicate accepted", err)
	}
	user, ok, err := s.AuthenticateContext(ctx, "employee.new", "fictional-test-password")
	if err != nil || !ok {
		t.Fatal("signup login failed", err)
	}
	token, _, _ := s.Create(user)
	if _, err := s.Users.ChangeRole(ctx, user.ID, admin.ID, Employee); !errors.Is(err, ErrForbidden) {
		t.Fatal("employee granted roles", err)
	}
	if _, err := s.Users.ChangeRole(ctx, admin.ID, admin.ID, Employee); !errors.Is(err, ErrLastAdmin) {
		t.Fatal("last admin demoted", err)
	}
	changed, err := s.Users.ChangeRole(ctx, admin.ID, user.ID, Manager)
	if err != nil || changed.Role != Manager {
		t.Fatal(err)
	}
	if _, ok, err := s.Resolve(ctx, token); ok || err != nil {
		t.Fatal("stale session was accepted", err)
	}
	user, ok, err = s.AuthenticateContext(ctx, "employee.new", "fictional-test-password")
	if !ok || err != nil || user.Role != Manager {
		t.Fatal("new login did not load new role", err)
	}
	token, _, _ = s.Create(user)
	if _, err := s.Users.ChangeRole(ctx, admin.ID, user.ID, Manager); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Resolve(ctx, token); !ok || err != nil {
		t.Fatal("identical role retry invalidated session", err)
	}
}
