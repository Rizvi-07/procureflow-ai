package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// GatewayConfig deliberately has no database configuration.
type GatewayConfig struct {
	Addr              string
	PurchaseTarget    string
	ApprovalTarget    string
	AssistantTarget   string
	AssistantTimeout  time.Duration
	VueOrigin         string
	RPCTimeout        time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
}

func LoadGateway() (GatewayConfig, error) {
	c := GatewayConfig{
		Addr:            env("API_GATEWAY_HTTP_ADDR", "127.0.0.1:8080"),
		PurchaseTarget:  env("PURCHASE_SERVICE_GRPC_TARGET", "localhost:50051"),
		ApprovalTarget:  env("APPROVAL_SERVICE_GRPC_TARGET", "localhost:50052"),
		AssistantTarget: env("ASSISTANT_GRPC_TARGET", ""),
		VueOrigin:       env("VUE_ORIGIN", "http://localhost:5173"),
	}
	for _, entry := range []struct {
		key, value string
		target     bool
	}{
		{"API_GATEWAY_HTTP_ADDR", c.Addr, false},
		{"PURCHASE_SERVICE_GRPC_TARGET", c.PurchaseTarget, true},
		{"APPROVAL_SERVICE_GRPC_TARGET", c.ApprovalTarget, true},
	} {
		host, port, err := net.SplitHostPort(entry.value)
		if err != nil {
			return GatewayConfig{}, fmt.Errorf("%s must be host:port", entry.key)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || (entry.target && host == "") {
			return GatewayConfig{}, fmt.Errorf("%s requires a valid port and targets require a host", entry.key)
		}
	}
	if !localOrigin(c.VueOrigin) {
		return GatewayConfig{}, fmt.Errorf("VUE_ORIGIN must be an exact http(s) loopback origin without a path, credentials, query, or fragment")
	}
	if c.AssistantTarget != "" {
		host, port, err := net.SplitHostPort(c.AssistantTarget)
		n, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || host == "" || n < 1 || n > 65535 {
			return GatewayConfig{}, fmt.Errorf("ASSISTANT_GRPC_TARGET must be host:port")
		}
	}
	for _, entry := range []struct {
		key, fallback string
		dst           *time.Duration
	}{
		{"GRPC_CALL_TIMEOUT", "2s", &c.RPCTimeout},
		{"ASSISTANT_CALL_TIMEOUT", "10s", &c.AssistantTimeout},
		{"HTTP_READ_HEADER_TIMEOUT", "5s", &c.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", "10s", &c.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", "15s", &c.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", "60s", &c.IdleTimeout},
		{"SHUTDOWN_TIMEOUT", "5s", &c.ShutdownTimeout},
	} {
		value, err := duration(entry.key, entry.fallback)
		if err != nil {
			return GatewayConfig{}, err
		}
		*entry.dst = value
	}
	if c.ReadHeaderTimeout > c.ReadTimeout {
		return GatewayConfig{}, fmt.Errorf("HTTP_READ_HEADER_TIMEOUT must not exceed HTTP_READ_TIMEOUT")
	}
	if c.WriteTimeout <= c.RPCTimeout {
		return GatewayConfig{}, fmt.Errorf("HTTP_WRITE_TIMEOUT must exceed GRPC_CALL_TIMEOUT")
	}
	if c.AssistantTarget != "" && c.WriteTimeout <= c.AssistantTimeout {
		return GatewayConfig{}, fmt.Errorf("HTTP_WRITE_TIMEOUT must exceed ASSISTANT_CALL_TIMEOUT")
	}
	var err error
	c.MaxBodyBytes, err = strconv.ParseInt(env("HTTP_MAX_BODY_BYTES", "65536"), 10, 64)
	if err != nil || c.MaxBodyBytes < 1 || c.MaxBodyBytes > 1<<20 {
		return GatewayConfig{}, fmt.Errorf("HTTP_MAX_BODY_BYTES must be 1-1048576")
	}
	return c, nil
}

func localOrigin(value string) bool {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	host := u.Hostname()
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}
