package config

import (
	"testing"
	"time"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"PURCHASE_SERVICE_GRPC_ADDR": ":50051", "APPROVAL_SERVICE_GRPC_ADDR": ":50052",
		"PURCHASE_SERVICE_GRPC_TARGET": "localhost:50051", "APPROVAL_SERVICE_GRPC_TARGET": "localhost:50052",
		"GRPC_CALL_TIMEOUT": "2s", "SHUTDOWN_TIMEOUT": "5s",
	} {
		t.Setenv(key, value)
	}
}

func TestLoad(t *testing.T) {
	setValidEnv(t)
	t.Setenv("PURCHASE_SERVICE_GRPC_ADDR", "127.0.0.1:60051")
	t.Setenv("GRPC_CALL_TIMEOUT", "250ms")
	c, err := Load()
	if err != nil || c.PurchaseAddr != "127.0.0.1:60051" || c.RPCTimeout != 250*time.Millisecond {
		t.Fatalf("config=%+v err=%v", c, err)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"GRPC_CALL_TIMEOUT", "0s"}, {"GRPC_CALL_TIMEOUT", "bad"}, {"SHUTDOWN_TIMEOUT", "-1s"},
		{"PURCHASE_SERVICE_GRPC_ADDR", "bad"}, {"APPROVAL_SERVICE_GRPC_ADDR", ":0"},
		{"APPROVAL_SERVICE_GRPC_ADDR", ":65536"}, {"PURCHASE_SERVICE_GRPC_TARGET", ":50051"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected config error")
			}
		})
	}
}
