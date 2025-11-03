package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"dynamic-gateway/internal/balancer"
	"dynamic-gateway/internal/config"
	"dynamic-gateway/internal/pool"
)

// GRPCHandler handles gRPC requests
type GRPCHandler struct {
	config         *config.Config
	connectionPool *pool.ConnectionPool
	balancers      map[string]*balancer.RoundRobinBalancer
	converter      *ProtocolConverter
	mu             sync.RWMutex
}

// NewGRPCHandler creates a new gRPC handler
func NewGRPCHandler(cfg *config.Config, pool *pool.ConnectionPool) *GRPCHandler {
	handler := &GRPCHandler{
		config:         cfg,
		connectionPool: pool,
		balancers:      make(map[string]*balancer.RoundRobinBalancer),
		converter:      NewProtocolConverter(pool),
	}

	// Initialize balancers for each service
	for _, svc := range cfg.GRPCServices {
		backends := make([]string, len(svc.Backends))
		for i, b := range svc.Backends {
			backends[i] = b.Address
		}
		handler.balancers[svc.ServiceName] = balancer.NewRoundRobinBalancer(backends)
	}

	return handler
}

// HandleGRPCRequest handles incoming gRPC requests
func (h *GRPCHandler) HandleGRPCRequest(ctx context.Context, serviceName, methodName string, req proto.Message) (proto.Message, error) {
	// Find service configuration
	var serviceConfig *config.GRPCService
	for i := range h.config.GRPCServices {
		if h.config.GRPCServices[i].ServiceName == serviceName {
			serviceConfig = &h.config.GRPCServices[i]
			break
		}
	}

	if serviceConfig == nil {
		return nil, status.Errorf(codes.NotFound, "service %s not found", serviceName)
	}

	// Get next backend
	balancer := h.balancers[serviceName]
	if balancer == nil {
		return nil, status.Errorf(codes.Internal, "no balancer for service %s", serviceName)
	}

	backendAddr := balancer.Next()
	if backendAddr == "" {
		return nil, status.Errorf(codes.Unavailable, "no backends available for service %s", serviceName)
	}

	// Route based on target protocol
	if serviceConfig.IsGRPC {
		// gRPC → gRPC
		return h.routeGRPCToGRPC(ctx, serviceName, methodName, req, backendAddr, serviceConfig)
	} else {
		// gRPC → HTTP
		return h.routeGRPCToHTTP(ctx, serviceName, methodName, req, backendAddr)
	}
}

// routeGRPCToGRPC routes gRPC request to gRPC backend
func (h *GRPCHandler) routeGRPCToGRPC(ctx context.Context, serviceName, methodName string, req proto.Message, backendAddr string, svcConfig *config.GRPCService) (proto.Message, error) {
	// Get connection
	conn, err := h.connectionPool.GetConnection(ctx, backendAddr, false, false)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "failed to connect to backend: %v", err)
	}

	// Forward metadata
	md, _ := metadata.FromIncomingContext(ctx)
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Invoke method
	fullMethod := fmt.Sprintf("/%s/%s", serviceName, methodName)

	var resp structpb.Struct
	err = conn.Invoke(
		ctx,
		fullMethod,
		req,
		&resp,
		grpc.WaitForReady(true),
		grpc.MaxCallRecvMsgSize(svcConfig.MaxCallRecvMsgSize),
	)

	if err != nil {
		log.Printf("gRPC invocation failed for %s: %v", fullMethod, err)
		return nil, err
	}

	return &resp, nil
}

// routeGRPCToHTTP routes gRPC request to HTTP backend
func (h *GRPCHandler) routeGRPCToHTTP(ctx context.Context, serviceName, methodName string, req proto.Message, backendURL string) (proto.Message, error) {
	// Convert gRPC to HTTP
	responseBytes, err := h.converter.GRPCToHTTP(ctx, serviceName, methodName, req, backendURL)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "protocol conversion failed: %v", err)
	}

	// Convert response back to protobuf
	var responseData map[string]interface{}
	if err := json.Unmarshal(responseBytes, &responseData); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to unmarshal response: %v", err)
	}

	responseStruct, err := structpb.NewStruct(responseData)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create response struct: %v", err)
	}

	return responseStruct, nil
}

// RegisterService registers the dynamic service
func (h *GRPCHandler) RegisterService(grpcServer *grpc.Server) {
	// Register a generic handler for all services
	grpcServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "dynamic.Gateway",
		HandlerType: (*interface{})(nil),
		Methods:     []grpc.MethodDesc{},
		Streams:     []grpc.StreamDesc{},
		Metadata:    "dynamic.proto",
	}, h)
}
