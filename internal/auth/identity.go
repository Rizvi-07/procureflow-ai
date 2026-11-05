package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Claims struct {
	Kind string `json:"kind"`
	Role string `json:"role,omitempty"`
	jwt.RegisteredClaims
}
type identityKey struct{}

func Identity(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(identityKey{}).(Claims)
	return c, ok
}
func WithIdentity(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, identityKey{}, c)
}
func LoadServiceKey() ([]byte, error) {
	path := os.Getenv("SERVICE_AUTH_KEY_FILE")
	if path == "" {
		path = "secrets/service-auth.key"
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SERVICE_AUTH_KEY_FILE; run go run ./cmd/auth-setup first")
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("service signing key must encode 32 random bytes")
	}
	return key, nil
}
func Sign(key []byte, kind, id, role string) (string, error) {
	now := time.Now()
	c := Claims{Kind: kind, Role: role, RegisteredClaims: jwt.RegisteredClaims{Subject: id, Issuer: "procureflow-gateway", Audience: jwt.ClaimStrings{"procureflow-internal"}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(key)
}
func Outgoing(ctx context.Context, key []byte, kind, id, role string) (context.Context, error) {
	token, err := Sign(key, kind, id, role)
	if err != nil {
		return nil, err
	}
	// Replace rather than append: never forward a caller-supplied identity.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(ctx, md), nil
}

// ActorClient signs only the identity already established by session middleware.
func ActorClient(key []byte) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		c, ok := Identity(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "authenticated identity required")
		}
		out, err := Outgoing(ctx, key, c.Kind, c.Subject, c.Role)
		if err != nil {
			return status.Error(codes.Internal, "identity signing failed")
		}
		return invoke(out, method, req, reply, cc, opts...)
	}
}

// ServiceClient replaces the actor with the calling service for internal handoffs.
func ServiceClient(key []byte, name string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		out, err := Outgoing(ctx, key, "service", name, "")
		if err != nil {
			return err
		}
		return invoke(out, method, req, reply, cc, opts...)
	}
}
func ServerInterceptor(key []byte) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if info.FullMethod == "/grpc.health.v1.Health/Check" {
			return next(ctx, req)
		}
		md, _ := metadata.FromIncomingContext(ctx)
		values := md.Get("authorization")
		if len(values) != 1 || len(values[0]) > 4096 || !strings.HasPrefix(values[0], "Bearer ") {
			return nil, status.Error(codes.Unauthenticated, "authenticated internal identity required")
		}
		var c Claims
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(values[0], "Bearer "), &c, func(*jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("procureflow-gateway"), jwt.WithAudience("procureflow-internal"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second))
		if err != nil || !token.Valid || c.Subject == "" {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired internal identity")
		}
		if !Allowed(c, info.FullMethod) {
			return nil, status.Error(codes.PermissionDenied, "identity cannot perform this operation")
		}
		return next(WithIdentity(ctx, c), req)
	}
}

// Allowed defaults to deny; new RPCs must explicitly declare their callers.
func Allowed(c Claims, method string) bool {
	if c.Kind == "service" {
		return (c.Subject == "purchase-service" && method == "/procureflow.v1.ApprovalService/CreateApproval") || (c.Subject == "approval-service" && (method == "/procureflow.v1.PurchaseService/GetPurchase" || method == "/procureflow.v1.PurchaseService/SetApprovalStatus"))
	}
	if c.Kind != "actor" || !ValidRole(c.Role) {
		return false
	}
	switch method {
	case "/procureflow.v1.PurchaseService/CreatePurchase", "/procureflow.v1.PurchaseService/GetPurchase", "/procureflow.v1.PurchaseService/ListPurchases", "/assistant.v1.AssistantService/AnswerPolicy":
		return true
	case "/procureflow.v1.ApprovalService/DecideApproval", "/procureflow.v1.ApprovalService/GetApproval", "/procureflow.v1.ApprovalService/ListApprovals":
		return CanReview(c.Role)
	default:
		return false
	}
}
