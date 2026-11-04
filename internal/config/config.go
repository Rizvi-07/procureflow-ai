package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	PurchaseAddr    string
	ApprovalAddr    string
	PurchaseTarget  string
	ApprovalTarget  string
	RPCTimeout      time.Duration
	ShutdownTimeout time.Duration
}

// Listen addresses and peer targets are separate: :50051 is a bind address,
// while localhost:50051 identifies the destination of an outgoing call.
func Load() (Config, error) {
	c := Config{
		PurchaseAddr:   env("PURCHASE_SERVICE_GRPC_ADDR", ":50051"),
		ApprovalAddr:   env("APPROVAL_SERVICE_GRPC_ADDR", ":50052"),
		PurchaseTarget: env("PURCHASE_SERVICE_GRPC_TARGET", "localhost:50051"),
		ApprovalTarget: env("APPROVAL_SERVICE_GRPC_TARGET", "localhost:50052"),
	}
	for _, entry := range []struct {
		name, value string
		target      bool
	}{
		{"PURCHASE_SERVICE_GRPC_ADDR", c.PurchaseAddr, false},
		{"APPROVAL_SERVICE_GRPC_ADDR", c.ApprovalAddr, false},
		{"PURCHASE_SERVICE_GRPC_TARGET", c.PurchaseTarget, true},
		{"APPROVAL_SERVICE_GRPC_TARGET", c.ApprovalTarget, true},
	} {
		host, port, err := net.SplitHostPort(entry.value)
		if err != nil {
			return Config{}, fmt.Errorf("%s must be host:port: %w", entry.name, err)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || (entry.target && strings.TrimSpace(host) == "") {
			return Config{}, fmt.Errorf("%s requires port 1-65535 and targets require a host", entry.name)
		}
	}
	var err error
	if c.RPCTimeout, err = duration("GRPC_CALL_TIMEOUT", "2s"); err != nil {
		return Config{}, err
	}
	if c.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", "5s"); err != nil {
		return Config{}, err
	}
	return c, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func duration(key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(env(key, fallback))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, such as 2s", key)
	}
	return d, nil
}
