package router

import (
	"bytes"
	"context"
	"dynamic-gateway/internal/pool"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// ProtocolConverter handles protocol conversion between HTTP and gRPC
type ProtocolConverter struct {
	connectionPool *pool.ConnectionPool
}

// NewProtocolConverter creates a new protocol converter
func NewProtocolConverter(pool *pool.ConnectionPool) *ProtocolConverter {
	return &ProtocolConverter{
		connectionPool: pool,
	}
}

// HTTPToGRPC converts HTTP request to gRPC call
func (pc *ProtocolConverter) HTTPToGRPC(ctx context.Context, serviceName, methodName string, httpReq *http.Request, backendAddr string) ([]byte, error) {
	// Read HTTP body
	bodyBytes, err := io.ReadAll(httpReq.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	defer httpReq.Body.Close()

	// Parse JSON to map
	var requestData map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Convert to protobuf Struct
	requestStruct, err := structpb.NewStruct(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to create struct: %w", err)
	}

	// Get gRPC connection
	conn, err := pc.connectionPool.GetConnection(ctx, backendAddr, false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Prepare metadata from HTTP headers
	md := metadata.New(nil)
	for key, values := range httpReq.Header {
		md.Append(key, values...)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Create dynamic method path
	fullMethod := fmt.Sprintf("/%s/%s", serviceName, methodName)

	// Invoke gRPC method
	var responseStruct structpb.Struct
	err = conn.Invoke(ctx, fullMethod, requestStruct, &responseStruct, grpc.WaitForReady(true))
	if err != nil {
		return nil, fmt.Errorf("gRPC invocation failed: %w", err)
	}

	// Convert response to JSON
	responseJSON, err := json.Marshal(responseStruct.AsMap())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return responseJSON, nil
}

// GRPCToHTTP converts gRPC call to HTTP request
func (pc *ProtocolConverter) GRPCToHTTP(ctx context.Context, serviceName, methodName string, grpcReq proto.Message, backendURL string) ([]byte, error) {
	// Convert protobuf to JSON
	var requestData map[string]interface{}

	// Handle dynamic messages
	if dynMsg, ok := grpcReq.(*dynamicpb.Message); ok {
		requestData = protoToMap(dynMsg)
	} else if structMsg, ok := grpcReq.(*structpb.Struct); ok {
		requestData = structMsg.AsMap()
	} else {
		return nil, fmt.Errorf("unsupported message type")
	}

	requestJSON, err := json.Marshal(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpURL := fmt.Sprintf("%s/%s/%s", backendURL, serviceName, methodName)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", httpURL, bytes.NewReader(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add headers from gRPC metadata
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for key, values := range md {
			for _, value := range values {
				httpReq.Header.Add(key, value)
			}
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute HTTP request
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(responseBytes))
	}

	return responseBytes, nil
}

// protoToMap converts dynamic protobuf message to map
func protoToMap(msg *dynamicpb.Message) map[string]interface{} {
	result := make(map[string]interface{})
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		result[string(fd.Name())] = v.Interface()
		return true
	})
	return result
}
