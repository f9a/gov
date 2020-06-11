package gov

import (
	"context"
	"testing"
	"time"
)

func TestLifecycleStop(t *testing.T) {
	shutdownSuccessful := make(chan result)
	shutdownFailed := make(chan Service)

	stop := make(chan struct{})
	shutdown := lifecycle(shutdownSuccessful, shutdownFailed, Service{
		Name: "s1",
		Start: func() error {
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 1 * time.Second,
		KillTimeout: 1 * time.Second,
	})

	shutdown()
	select {
	case <-shutdownSuccessful:
		return
	case <-shutdownFailed:
		t.Fatal("Shutdown shouldn't fail")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestLifecycleKill(t *testing.T) {
	shutdownSuccessful := make(chan result)
	shutdownFailed := make(chan Service)

	kill := make(chan struct{})
	shutdown := lifecycle(shutdownSuccessful, shutdownFailed, Service{
		Name: "s1",
		Start: func() error {
			<-kill
			return nil
		},
		Stop: func(context.Context) {},
		Kill: func() {
			kill <- struct{}{}
		},
		StopTimeout: 1 * time.Second,
		KillTimeout: 1 * time.Second,
	})

	shutdown()
	select {
	case <-shutdownSuccessful:
		return
	case <-shutdownFailed:
		t.Fatal("Shutdown shouldn't fail")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestLifecycleShutdownFailed(t *testing.T) {
	shutdownSuccessful := make(chan result)
	shutdownFailed := make(chan Service)

	kill := make(chan struct{})
	shutdown := lifecycle(shutdownSuccessful, shutdownFailed, Service{
		Name: "s1",
		Start: func() error {
			<-kill
			return nil
		},
		Stop:        func(context.Context) {},
		Kill:        func() {},
		StopTimeout: 1 * time.Second,
		KillTimeout: 1 * time.Second,
	})

	shutdown()
	select {
	case <-shutdownSuccessful:
		t.Fatal("Shutdown should fail")
	case <-shutdownFailed:
		return
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}
