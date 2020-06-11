package gov_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/f9a/gov"
	"github.com/f9a/gov/service"
)

func newMoonRabbit() (func(), chan struct{}) {
	stop := make(chan struct{})
	ticker := time.NewTicker(1 * time.Second)
	moonRabbit := func() {
		for {
			select {
			case <-ticker.C:
				log.Println("moon-rabbit is eating some vegan bacon")
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}

	return moonRabbit, stop
}

func ExampleStartStop() {
	moonRabbit, stop := newMoonRabbit()

	sm := gov.New()
	sm.Add(gov.Service{
		Name: "moon-rabbit",
		Start: func() error {
			moonRabbit()
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(4 * time.Second)

	err := sm.Stop()
	if err != nil {
		log.Fatalf("Sad moon-rabbit catched fire: %v", err)
	}

	<-done
}

func ExampleStopOnOsSignal() {
	moonRabbit, stop := newMoonRabbit()

	sm := gov.New(gov.StopOnOSSignal())
	sm.Add(gov.Service{
		Name: "moon-rabbit",
		Start: func() error {
			moonRabbit()
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(4 * time.Second)

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		log.Fatal(err)
	}
	err = process.Signal(syscall.SIGTERM)
	if err != nil {
		log.Fatal(err)
	}

	err = <-done
	if err != nil {
		log.Fatalf("Sad moon-rabbit catched fire: %v", err)
	}
}

func ExampleGracefullShutdownHTTPServer() {
	server := &http.Server{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "moon-rabbit says hello!")
		}),
	}

	sm := gov.New(
		gov.StopOnSignal(gov.SignalFromTime(time.After(4 * time.Second))),
	)
	sm.Add(gov.Service{
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
	})

	err := sm.Start()
	if err != nil {
		log.Fatalf("moon-rabbit not found: %v", err)
	}
}

func ExampleGracefullShutdownWithTemplate() {
	server := &http.Server{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "moon-rabbit says hello!")
		}),
	}

	sm := gov.New(
		gov.StopOnSignal(gov.SignalFromTime(time.After(4 * time.Second))),
	)
	sm.Add(service.NewHTTP(server))

	err := sm.Start()
	if err != nil {
		log.Fatalf("moon-rabbit not found: %v", err)
	}
}
