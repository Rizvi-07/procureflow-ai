package config

import (
	"testing"
)

func gatewayEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"API_GATEWAY_HTTP_ADDR": "127.0.0.1:8080", "VUE_ORIGIN": "http://localhost:5173",
		"PURCHASE_SERVICE_GRPC_TARGET": "localhost:50051", "APPROVAL_SERVICE_GRPC_TARGET": "localhost:50052",
		"GRPC_CALL_TIMEOUT": "2s", "SHUTDOWN_TIMEOUT": "5s", "HTTP_READ_HEADER_TIMEOUT": "5s",
		"HTTP_READ_TIMEOUT": "10s", "HTTP_WRITE_TIMEOUT": "15s", "HTTP_IDLE_TIMEOUT": "60s", "HTTP_MAX_BODY_BYTES": "65536",
	} {
		t.Setenv(key, value)
	}
}

func TestGatewayConfig(t *testing.T) {
	gatewayEnv(t)
	// The gateway must never attempt to load a database URL or its credentials.
	t.Setenv("DATABASE_URL", "invalid-do-not-use")
	c, err := LoadGateway()
	if err != nil || c.Addr != "127.0.0.1:8080" || c.MaxBodyBytes != 65536 || c.VueOrigin != "http://localhost:5173" {
		t.Fatalf("gateway=%+v err=%v", c, err)
	}
	for _, origin := range []string{"http://localhost:5173", "http://127.0.0.1:5173", "https://[::1]:5173"} {
		t.Setenv("VUE_ORIGIN", origin)
		if _, err := LoadGateway(); err != nil {
			t.Fatalf("local origin rejected: %s", origin)
		}
	}
}

func TestInvalidGatewayConfig(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"API_GATEWAY_HTTP_ADDR", "bad"}, {"API_GATEWAY_HTTP_ADDR", ":0"}, {"PURCHASE_SERVICE_GRPC_TARGET", ":50051"},
		{"VUE_ORIGIN", "*"}, {"VUE_ORIGIN", "null"}, {"VUE_ORIGIN", "https://evil.example"},
		{"VUE_ORIGIN", "http://localhost:5173/"}, {"VUE_ORIGIN", "http://localhost:5173?query=1"},
		{"VUE_ORIGIN", "http://user@localhost:5173"}, {"VUE_ORIGIN", "http://localhost:65536"},
		{"GRPC_CALL_TIMEOUT", "0s"}, {"HTTP_READ_TIMEOUT", "0s"}, {"HTTP_READ_HEADER_TIMEOUT", "20s"},
		{"HTTP_WRITE_TIMEOUT", "1s"}, {"HTTP_IDLE_TIMEOUT", "-1s"}, {"SHUTDOWN_TIMEOUT", "bad"},
		{"HTTP_MAX_BODY_BYTES", "0"}, {"HTTP_MAX_BODY_BYTES", "1048577"}, {"HTTP_MAX_BODY_BYTES", "bad"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			gatewayEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := LoadGateway(); err == nil {
				t.Fatal("expected invalid config error")
			}
		})
	}
}
