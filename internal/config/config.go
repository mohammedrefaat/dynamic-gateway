package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config represents the gateway configuration
type Config struct {
	Host                string        `json:"host"`
	HTTPPort            int           `json:"http_port"`
	TLSPort             int           `json:"tls_port"`
	RunTLSServer        bool          `json:"run_tls_server"`
	RunHTTPServer       bool          `json:"run_http_server"`
	AllowAllOrigin      bool          `json:"allow_all_origin"`
	AllowedOrigins      []string      `json:"allowed_origins"`
	AllowedHeaders      []string      `json:"allowed_headers"`
	MaxCallRecvMsgSize  int           `json:"max_call_recv_msg_size"`
	MaxCallSendMsgSize  int           `json:"max_call_send_msg_size"`
	GRPCServices        []GRPCService `json:"grpc_services"`
	HTTPRoutes          []HTTPRoute   `json:"http_routes"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	ConnectionTimeout   time.Duration `json:"connection_timeout"`
}

// GRPCService represents a gRPC service configuration
type GRPCService struct {
	ServiceName        string    `json:"service_name"`
	IsGRPC             bool      `json:"is_grpc"`
	MaxCallRecvMsgSize int       `json:"max_call_recv_msg_size"`
	MaxCallSendMsgSize int       `json:"max_call_send_msg_size"`
	Backends           []Backend `json:"backends"`
	Timeout            string    `json:"timeout"`
	RetryAttempts      int       `json:"retry_attempts"`
}

// HTTPRoute represents an HTTP route configuration
type HTTPRoute struct {
	Path           string    `json:"path"`
	Methods        []string  `json:"methods"`
	TargetProtocol string    `json:"target_protocol"` // "http" or "grpc"
	StripPath      bool      `json:"strip_path"`
	Backends       []Backend `json:"backends"`
	Timeout        string    `json:"timeout"`
}

// Backend represents a backend server
type Backend struct {
	Address         string `json:"address"`
	Weight          int    `json:"weight"`
	TLS             bool   `json:"tls"`
	TLSServerName   string `json:"tls_server_name"`
	TLSSkipVerify   bool   `json:"tls_skip_verify"`
	HealthCheckPath string `json:"health_check_path"`
	MaxConnections  int    `json:"max_connections"`
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// Set defaults
	if config.MaxCallRecvMsgSize == 0 {
		config.MaxCallRecvMsgSize = 10 * 1024 * 1024 // 10MB
	}
	if config.MaxCallSendMsgSize == 0 {
		config.MaxCallSendMsgSize = 10 * 1024 * 1024 // 10MB
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.ConnectionTimeout == 0 {
		config.ConnectionTimeout = 10 * time.Second
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.HTTPPort == 0 && c.TLSPort == 0 {
		return fmt.Errorf("at least one port (http_port or tls_port) must be specified")
	}

	if !c.RunHTTPServer && !c.RunTLSServer {
		return fmt.Errorf("at least one server (http or tls) must be enabled")
	}

	// Validate gRPC services
	for i, svc := range c.GRPCServices {
		if svc.ServiceName == "" {
			return fmt.Errorf("service_name is required for grpc_services[%d]", i)
		}
		if len(svc.Backends) == 0 {
			return fmt.Errorf("at least one backend is required for service %s", svc.ServiceName)
		}
		for j, backend := range svc.Backends {
			if backend.Address == "" {
				return fmt.Errorf("address is required for service %s, backend[%d]", svc.ServiceName, j)
			}
		}
	}

	// Validate HTTP routes
	for i, route := range c.HTTPRoutes {
		if route.Path == "" {
			return fmt.Errorf("path is required for http_routes[%d]", i)
		}
		if len(route.Backends) == 0 {
			return fmt.Errorf("at least one backend is required for route %s", route.Path)
		}
	}

	return nil
}
