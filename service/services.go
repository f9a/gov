package service

import (
	"context"
	"errors"
	"net"
	"net/http"

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
		Kill: server.Stop,
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
	}
}

type HTTPListenerServer interface {
	Serve(net.Listener) error
}

// NewListener returns basic service which close listener on stop
func NewListener(server HTTPListenerServer, listener net.Listener) gov.Service {
	return gov.Service{
		Start: func() error {
			return server.Serve(listener)
		},
		Stop: func(ctx context.Context) {
			listener.Close()
		},
	}
}
