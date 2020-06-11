package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/f9a/gov"
)

type GRPCServer interface {
	Serve(net.Listener) error
	GracefulStop()
	Stop()
}

// NewGRPC returns basic grpc-service
func NewGRPC(server GRPCServer, listener net.Listener) gov.Service {
	return gov.Service{
		Name: "grpc-server",
		Start: func() error {
			return server.Serve(listener)
		},
		Stop: func(context.Context) {
			server.GracefulStop()
		},
		Kill:        server.Stop,
		StopTimeout: 30 * time.Second,
		KillTimeout: 3 * time.Second,
	}
}

type HTTPServer interface {
	Close() error
	ListenAndServe() error
	Shutdown(context.Context) error
}

// NewHTTP returns basic http-service which first attempts to stop gracefully
// if this is not working in given time, Close is used to terminate all connections.
func NewHTTP(server HTTPServer) gov.Service {
	return gov.Service{
		Name: "http-server",
		Start: func() error {
			if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}

			return nil
		},
		Stop: func(ctx context.Context) {
			server.Shutdown(ctx)
		},
		Kill: func() {
			server.Close()
		},
		StopTimeout: 30 * time.Second,
		KillTimeout: 3 * time.Second,
	}
}

type HTTPListener interface {
	Close() error
	Serve(net.Listener) error
	Shutdown(context.Context) error
}

func NewHTTPListener(server HTTPListener, listener net.Listener) gov.Service {
	return gov.Service{
		Name: "http-server",
		Start: func() error {
			if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
				return err
			}

			return nil
		},
		Stop: func(ctx context.Context) {
			server.Shutdown(ctx)
		},
		Kill: func() {
			server.Close()
		},
		StopTimeout: 30 * time.Second,
		KillTimeout: 3 * time.Second,
	}
}

type Server interface {
	Serve(net.Listener) error
}

// NewListener returns basic service which close listener on stop
func NewListener(server Server, listener net.Listener) gov.Service {
	return gov.Service{
		Start: func() error {
			return server.Serve(listener)
		},
		Stop: func(ctx context.Context) {
			listener.Close()
		},
		StopTimeout: 30 * time.Second,
	}
}
