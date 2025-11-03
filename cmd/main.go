package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"dynamic-gateway/internal/config"
	"dynamic-gateway/internal/middleware"
	"dynamic-gateway/internal/pool"
	"dynamic-gateway/internal/router"
)

var (
	configPath = flag.String("config", "configs/config.json", "Path to configuration file")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("HTTP Server: %v (port %d)", cfg.RunHTTPServer, cfg.HTTPPort)
	log.Printf("TLS Server: %v (port %d)", cfg.RunTLSServer, cfg.TLSPort)
	log.Printf("gRPC Services: %d", len(cfg.GRPCServices))
	log.Printf("HTTP Routes: %d", len(cfg.HTTPRoutes))

	// Create connection pool
	connectionPool := pool.NewConnectionPool(cfg.MaxCallRecvMsgSize)
	defer connectionPool.CloseAll()

	// Create handlers
	grpcHandler := router.NewGRPCHandler(cfg, connectionPool)
	httpHandler := router.NewHTTPHandler(cfg, connectionPool)

	// Setup HTTP server
	var httpServer *http.Server
	if cfg.RunHTTPServer {
		mux := http.NewServeMux()

		// Add middleware
		handler := middleware.Recovery(
			middleware.Logging(
				middleware.CORS(cfg)(httpHandler),
			),
		)

		mux.Handle("/", handler)

		// Health check endpoint
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})

		// Connection pool health
		mux.HandleFunc("/health/connections", func(w http.ResponseWriter, r *http.Request) {
			health := connectionPool.HealthCheck()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(health)
		})

		httpServer = &http.Server{
			Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.HTTPPort),
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}

		go func() {
			log.Printf("Starting HTTP server on %s", httpServer.Addr)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}()
	}

	// Setup gRPC server
	var grpcServer *grpc.Server
	if cfg.RunTLSServer {
		grpcServer = grpc.NewServer(
			grpc.MaxRecvMsgSize(cfg.MaxCallRecvMsgSize),
			grpc.MaxSendMsgSize(cfg.MaxCallSendMsgSize),
		)

		grpcHandler.RegisterService(grpcServer)
		reflection.Register(grpcServer)

		lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.TLSPort))
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}

		go func() {
			log.Printf("Starting gRPC server on %s", lis.Addr())
			if err := grpcServer.Serve(lis); err != nil {
				log.Fatalf("gRPC server error: %v", err)
			}
		}()
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down servers...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}

	if grpcServer != nil {
		grpcServer.GracefulStop()
	}

	log.Println("Servers stopped")
}
