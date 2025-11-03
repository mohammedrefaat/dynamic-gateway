package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// ConnectionPool manages gRPC connections
type ConnectionPool struct {
	connections sync.Map // map[string]*grpc.ClientConn
	mu          sync.RWMutex
	maxMsgSize  int
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(maxMsgSize int) *ConnectionPool {
	return &ConnectionPool{
		maxMsgSize: maxMsgSize,
	}
}

// GetConnection gets or creates a gRPC connection
func (p *ConnectionPool) GetConnection(ctx context.Context, address string, useTLS bool, skipVerify bool) (*grpc.ClientConn, error) {
	// Check if connection exists and is ready
	if conn, ok := p.connections.Load(address); ok {
		clientConn := conn.(*grpc.ClientConn)
		state := clientConn.GetState()

		// Reuse if connection is ready or connecting
		if state == connectivity.Ready || state == connectivity.Connecting || state == connectivity.Idle {
			return clientConn, nil
		}

		// Close and remove stale connection
		clientConn.Close()
		p.connections.Delete(address)
	}

	// Create new connection
	conn, err := p.createConnection(ctx, address, useTLS, skipVerify)
	if err != nil {
		return nil, err
	}

	p.connections.Store(address, conn)
	return conn, nil
}

// createConnection creates a new gRPC connection
func (p *ConnectionPool) createConnection(ctx context.Context, address string, useTLS bool, skipVerify bool) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(p.maxMsgSize),
			grpc.MaxCallSendMsgSize(p.maxMsgSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	// Configure TLS
	if useTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: skipVerify,
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Create connection with timeout
	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %w", address, err)
	}

	return conn, nil
}

// CloseAll closes all connections
func (p *ConnectionPool) CloseAll() {
	p.connections.Range(func(key, value interface{}) bool {
		conn := value.(*grpc.ClientConn)
		conn.Close()
		p.connections.Delete(key)
		return true
	})
}

// HealthCheck checks connection health
func (p *ConnectionPool) HealthCheck() map[string]string {
	health := make(map[string]string)

	p.connections.Range(func(key, value interface{}) bool {
		address := key.(string)
		conn := value.(*grpc.ClientConn)
		state := conn.GetState()
		health[address] = state.String()
		return true
	})

	return health
}
