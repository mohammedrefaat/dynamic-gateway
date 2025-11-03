package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"dynamic-gateway/internal/balancer"
	"dynamic-gateway/internal/config"
	"dynamic-gateway/internal/pool"
)

// HTTPHandler handles HTTP requests
type HTTPHandler struct {
	config         *config.Config
	connectionPool *pool.ConnectionPool
	balancers      map[string]*balancer.RoundRobinBalancer
	converter      *ProtocolConverter
	mu             sync.RWMutex
}

// NewHTTPHandler creates a new HTTP handler
func NewHTTPHandler(cfg *config.Config, pool *pool.ConnectionPool) *HTTPHandler {
	handler := &HTTPHandler{
		config:         cfg,
		connectionPool: pool,
		balancers:      make(map[string]*balancer.RoundRobinBalancer),
		converter:      NewProtocolConverter(pool),
	}

	// Initialize balancers for each route
	for i, route := range cfg.HTTPRoutes {
		backends := make([]string, len(route.Backends))
		for j, b := range route.Backends {
			backends[j] = b.Address
		}
		routeKey := fmt.Sprintf("route_%d", i)
		handler.balancers[routeKey] = balancer.NewRoundRobinBalancer(backends)
	}

	return handler
}

// ServeHTTP implements http.Handler
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Find matching route
	route, routeKey := h.findRoute(r.URL.Path, r.Method)
	if route == nil {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}

	// Get next backend
	balancer := h.balancers[routeKey]
	if balancer == nil {
		http.Error(w, "no balancer configured", http.StatusInternalServerError)
		return
	}

	backendAddr := balancer.Next()
	if backendAddr == "" {
		http.Error(w, "no backends available", http.StatusServiceUnavailable)
		return
	}

	// Route based on target protocol
	if route.TargetProtocol == "grpc" {
		// HTTP → gRPC
		h.routeHTTPToGRPC(w, r, route, backendAddr)
	} else {
		// HTTP → HTTP
		h.routeHTTPToHTTP(w, r, route, backendAddr)
	}
}

// routeHTTPToHTTP forwards HTTP request to HTTP backend
func (h *HTTPHandler) routeHTTPToHTTP(w http.ResponseWriter, r *http.Request, route *config.HTTPRoute, backendAddr string) {
	// Build target URL
	targetURL := backendAddr + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "failed to create proxy request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Execute request
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("HTTP proxy error: %v", err)
		http.Error(w, "backend request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Failed to copy response: %v", err)
	}
}

// routeHTTPToGRPC converts HTTP request to gRPC call
func (h *HTTPHandler) routeHTTPToGRPC(w http.ResponseWriter, r *http.Request, route *config.HTTPRoute, backendAddr string) {
	// Extract service and method from path
	// Expected format: /grpc/{service}/{method}
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) < 3 {
		http.Error(w, "invalid path format, expected /grpc/{service}/{method}", http.StatusBadRequest)
		return
	}

	serviceName := pathParts[1]
	methodName := pathParts[2]

	// Convert HTTP to gRPC
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	responseBytes, err := h.converter.HTTPToGRPC(ctx, serviceName, methodName, r, backendAddr)
	if err != nil {
		log.Printf("HTTP to gRPC conversion failed: %v", err)
		http.Error(w, fmt.Sprintf("protocol conversion failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseBytes)
}

// findRoute finds a matching route for the given path and method
func (h *HTTPHandler) findRoute(path, method string) (*config.HTTPRoute, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for i, route := range h.config.HTTPRoutes {
		// Check path match
		if !h.pathMatches(path, route.Path) {
			continue
		}

		// Check method match
		if len(route.Methods) > 0 {
			methodMatch := false
			for _, m := range route.Methods {
				if m == method {
					methodMatch = true
					break
				}
			}
			if !methodMatch {
				continue
			}
		}

		routeKey := fmt.Sprintf("route_%d", i)
		return &route, routeKey
	}

	return nil, ""
}

// pathMatches checks if request path matches route path pattern
func (h *HTTPHandler) pathMatches(requestPath, routePath string) bool {
	// Simple prefix matching (can be enhanced with parameter matching)
	if strings.HasSuffix(routePath, "*") {
		prefix := strings.TrimSuffix(routePath, "*")
		return strings.HasPrefix(requestPath, prefix)
	}

	// Exact match or prefix match
	return strings.HasPrefix(requestPath, routePath)
}
