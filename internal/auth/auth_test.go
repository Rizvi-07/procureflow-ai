package auth

import (
	"context"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
	"time"
)

func TestPasswordAndSessions(t *testing.T) {
	hash, err := HashPassword("fictional-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "fictional-test-password") || VerifyPassword(hash, "wrong") || VerifyPassword(strings.Replace(hash, "m=19456", "m=99999999", 1), "fictional-test-password") {
		t.Fatal("password verification failed")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("weak password accepted")
	}
	u := User{ID: "emp-101", Username: "employee.demo", Name: "Demo", Role: Employee}
	s := NewSessions([]Account{{User: u, PasswordHash: hash}}, true)
	if _, ok := s.Authenticate("employee.demo", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if _, ok := s.Authenticate("unknown", "fictional-test-password"); ok {
		t.Fatal("unknown account accepted")
	}
	if got, ok := s.Authenticate(" Employee.Demo ", "fictional-test-password"); !ok || got != u {
		t.Fatal("valid login failed")
	}
	token, session, err := s.Create(u)
	if err != nil {
		t.Fatal(err)
	}
	if token == session.CSRF || len(token) < 40 {
		t.Fatal("session secrets not independent")
	}
	if _, ok := s.Lookup(token); !ok {
		t.Fatal("session missing")
	}
	s.Revoke(token)
	if _, ok := s.Lookup(token); ok {
		t.Fatal("revoked session accepted")
	}
	token, _, _ = s.Create(u)
	now := s.Now()
	s.Now = func() time.Time { return now.Add(9 * time.Hour) }
	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired session accepted")
	}
	for i := 0; i < 10; i++ {
		if !s.AllowLogin("127.0.0.1", "employee.demo") {
			t.Fatal("early rate limit")
		}
	}
	if s.AllowLogin("different-ip", "employee.demo") {
		t.Fatal("username rate limit bypass")
	}
}

func TestSignedIdentityBoundary(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	method := "/procureflow.v1.PurchaseService/ListPurchases"
	valid, _ := Sign(key, "actor", "emp-101", Employee)
	expired := Claims{Kind: "actor", Role: Employee, RegisteredClaims: jwt.RegisteredClaims{Subject: "emp-101", Issuer: "procureflow-gateway", Audience: jwt.ClaimStrings{"procureflow-internal"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
	old, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, expired).SignedString(key)
	forged, _ := Sign([]byte("wrongwrongwrongwrongwrongwrong12"), "actor", "emp-101", Manager)
	employee, _ := Sign(key, "actor", "emp-101", Employee)
	for _, tc := range []struct {
		name, token, method string
		want                codes.Code
	}{{"missing", "", method, codes.Unauthenticated}, {"forged", forged, method, codes.Unauthenticated}, {"expired", old, method, codes.Unauthenticated}, {"allowed", valid, method, codes.OK}, {"employee decision", employee, "/procureflow.v1.ApprovalService/DecideApproval", codes.PermissionDenied}, {"actor status update", valid, "/procureflow.v1.PurchaseService/SetApprovalStatus", codes.PermissionDenied}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+tc.token))
			called := false
			_, err := ServerInterceptor(key)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: tc.method}, func(ctx context.Context, _ any) (any, error) {
				called = true
				if c, ok := Identity(ctx); !ok || c.Subject != "emp-101" {
					t.Fatal("verified actor not propagated")
				}
				return nil, nil
			})
			if status.Code(err) != tc.want || called != (tc.want == codes.OK) {
				t.Fatalf("code=%v called=%v", status.Code(err), called)
			}
		})
	}
	service := Claims{Kind: "service", RegisteredClaims: jwt.RegisteredClaims{Subject: "approval-service"}}
	if !Allowed(service, "/procureflow.v1.PurchaseService/SetApprovalStatus") || Allowed(service, method) {
		t.Fatal("service permission boundary")
	}
}
