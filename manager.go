package gov

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrAlreadyStarted is returned from Manager.Start when the service-manager was already started before.
var ErrAlreadyStarted = errors.New("service-manager already started")

// ErrAlreadyStopping is returned from Manager.Stop when the service-manager is already in stopping state.
var ErrAlreadyStopping = errors.New("service-manager is already stopping")

// ErrNotStarted is returned from Manager.Stop when it is called before Manager.Start is called.
var ErrNotStarted = errors.New("service-manager is not started yet")

// ErrAlreadyStopped is returned from Manager.Stop when it was called before.
var ErrAlreadyStopped = errors.New("service-manager is already stopped")

// ErrNoService is returned from Manager.Start when no service is added
var ErrNoService = errors.New("at least one service must be registered to start the service-manager")

// ServiceName name of a service
type ServiceName = string

// Service represents a service which is controlled by the Manager
type Service struct {
	// Name is a unique identifiyer for the service
	Name ServiceName
	// Start start a service and is required
	Start func() error
	// Stop stops a service and is required
	Stop func(context.Context)
	// When stop was not successfull after a given timeout kill is executed.
	// todo: Add context.Context argument
	Kill func()
	// StopTimeout max duration before Kill gets executed
	StopTimeout time.Duration
	// KillTimeout max duration before shutdown failes
	KillTimeout time.Duration

	StopAfter  []ServiceName
	StopBefore []ServiceName
}

type result struct {
	err  error
	from string
}

type shutdownRequest struct {
	from    chan struct{}
	service Service
}

// ServiceErrors contains all service errors
type ServiceErrors struct {
	Map map[ServiceName]error
}

func (serr *ServiceErrors) add(name ServiceName, err error) {
	if serr.Map == nil {
		serr.Map = map[string]error{}
	}

	serr.Map[name] = err
}

func (serr *ServiceErrors) err() error {
	if len(serr.Map) > 0 {
		return serr
	}

	return nil
}

func (serr ServiceErrors) Error() string {
	if len(serr.Map) == 0 {
		return "[]"
	}

	str := &strings.Builder{}

	str.WriteString("[")
	first := true
	for name, err := range serr.Map {
		if first {
			str.WriteString("(" + name + "," + err.Error() + ")")
			first = false
			continue
		}

		str.WriteString(",(" + name + "," + err.Error() + ")")
	}

	str.WriteString("]")

	return str.String()
}

// Manager represents a service-manager which controls the lifecycle for provided services.
type Manager struct {
	stopSignal <-chan struct{}

	services      []Service
	shutdownGraph *shutdownGraph

	lock      sync.Mutex
	isStarted bool
	isStopped bool

	err error

	errs               *ServiceErrors
	startedServices    map[ServiceName]func()
	stoppedFrom        chan error
	stopped            chan struct{}
	shutdownSuccessful chan result
	shutdownFailed     chan Service
	startStopping      chan chan error
}

// Add adds a new service to the manager.
func (mngr *Manager) Add(service Service) {
	mngr.services = append(mngr.services, service)
}

func (mngr *Manager) sendStoppedSignals() {
	close(mngr.stopped)
	if mngr.stoppedFrom != nil {
		mngr.stoppedFrom <- mngr.errs.err()
	}
}

type signalRule struct {
	to chan struct{}
	on ServiceName
}

type stateFunc func(*Manager) stateFunc

func pid1(sm *Manager) stateFunc {
	// fmt.Println("Enter pid1 state")
	if len(sm.services) == 0 {
		sm.err = ErrNoService
		return nil
	}

	// Verify services before starting goroutines so we don't have
	// to stop a lot of services when one invalid service exists.
	ss := map[string]struct{}{}
	for i, service := range sm.services {
		_, ok := ss[service.Name]
		if ok {
			sm.err = fmt.Errorf("service %s already exists", service.Name)
			return nil
		}

		if service.Name == "" {
			sm.err = fmt.Errorf("service %d Name is empty", i)
			return nil
		}

		if service.Start == nil {
			sm.err = fmt.Errorf("%s Start cannot be nil", service.Name)
			return nil
		}

		if service.Stop == nil {
			sm.err = fmt.Errorf("%s Stop cannot be nil", service.Name)
			return nil
		}

		ss[service.Name] = struct{}{}
	}

	sm.shutdownGraph, sm.err = newShutdownGraph(sm.services)
	if sm.err != nil {
		return nil
	}

	sm.startedServices = map[ServiceName]func(){}
	for _, service := range sm.services {
		shutdown := lifecycle(sm.shutdownSuccessful, sm.shutdownFailed, service)
		sm.startedServices[service.Name] = shutdown
	}

	for {
		select {
		case result := <-sm.shutdownSuccessful:
			delete(sm.startedServices, result.from)
			sm.errs.add(result.from, result.err)
			return stopping
		case from := <-sm.startStopping:
			sm.stoppedFrom = from
			return stopping
		}

	}
}

func stopping(sm *Manager) stateFunc {
	// fmt.Println("Enter stopping state")
	if len(sm.startedServices) == 0 {
		sm.sendStoppedSignals()
		return nil
	}

	shutdownService := make(chan shutdownRequest)

	ssm := shutdownStateMachine{
		shutdownService: shutdownService,
		graph:           sm.shutdownGraph,
	}
	go ssm.Start()

	waitForShutdown := map[ServiceName]chan struct{}{}
	for {
		select {
		case from := <-sm.startStopping:
			from <- ErrAlreadyStopping
			continue
		case req := <-shutdownService:
			shutdown, ok := sm.startedServices[req.service.Name]
			if !ok {
				// Service is already down because of an error while starting.
				req.from <- struct{}{}
				continue
			}

			waitForShutdown[req.service.Name] = req.from
			shutdown()
		case result := <-sm.shutdownSuccessful:
			if result.err != nil {
				sm.errs.add(result.from, result.err)
			}

			delete(sm.startedServices, result.from)

			if from, ok := waitForShutdown[result.from]; ok {
				from <- struct{}{}
			}

			if len(sm.startedServices) == 0 {
				sm.sendStoppedSignals()
				return nil
			}
		case service := <-sm.shutdownFailed:
			delete(sm.startedServices, service.Name)

			if from, ok := waitForShutdown[service.Name]; ok {
				from <- struct{}{}
			}

			if len(sm.startedServices) == 0 {
				sm.sendStoppedSignals()
				return nil
			}
		}
	}
}

func (mngr *Manager) loop() error {
	state := pid1
	for {
		state = state(mngr)
		if state == nil {
			mngr.lock.Lock()
			mngr.isStopped = true
			mngr.lock.Unlock()

			if mngr.err != nil {
				return mngr.err
			}

			return mngr.errs.err()
		}
	}
}

// Start executes the Start method for each previously added service in a seperated goroutine.
// A ServiceErrors is returned when at least one Service.Start method failed.
// If a signal-channel is given, the channel waits for signal, if received Manager.Stop is executed.
// Start can only be called once otherwise ErrAlreadyStarted error is returned.
// If a start-method fails during start process (service return error before Manager.Stop was called)
// Manager.Stop will be exectued to stop all Service which have been started already.
// If no service is registred ErrNoService is returned.
// Before any service is started, it is ensured that the Service.Start and Service.Stop is not nil.
// The Service.Name must not be empty and must be unique, otherwise a error is returned
func (mngr *Manager) Start() error {
	mngr.lock.Lock()
	if mngr.isStarted == true {
		mngr.lock.Unlock()
		return ErrAlreadyStarted
	}
	mngr.isStarted = true
	mngr.lock.Unlock()

	if mngr.stopSignal != nil {
		go func() {
			select {
			case <-mngr.stopSignal:
				_ = mngr.Stop()
			case <-mngr.stopped:
				return
			}
		}()
	}

	return mngr.loop()
}

// Stop executes the Stop method for each previously added service.
// Each Service.Stop is executed in its own goroutine,
// the goroutine can only be finished when the Service.Stop returns.
// This means that a stop-goroutine can continue to run in the background
// even if stop-timeout has been triggered and Manager.Stop already starts to execute Service.Kill methods.
// If the Service.Stop is not terminated after the stop-timeout duration,
// the Kill method will be executed for that service, if available.
// It will return a ServiceErrors error when at least on Service.Start method failed
// Manager is considered stopped when either all Service.Start methods are returned
// or all kill-timeouts for all services are elapsed.
// If service-manager is already stopped ErrAlreadyStopped error is returned.
// If service-manager in stopping state ErrAlreadyStopping error is returned.
func (mngr *Manager) Stop() error {
	mngr.lock.Lock()
	if !mngr.isStarted {
		mngr.lock.Unlock()
		return ErrNotStarted
	}

	if mngr.isStopped {
		mngr.lock.Unlock()
		return ErrAlreadyStopped
	}
	mngr.lock.Unlock()

	err := make(chan error)
	mngr.startStopping <- err
	return <-err
}

// Option a service-manager option
type Option interface {
	apply(*Manager)
}

// OptionFunc is an adapter to allow the use of ordinary functions as Option.
type OptionFunc func(*Manager)

func (fun OptionFunc) apply(mngr *Manager) {
	fun(mngr)
}

// StopOnSignal set channel to trigger the service-manager to stop.
func StopOnSignal(sig <-chan struct{}) Option {
	return OptionFunc(func(mngr *Manager) {
		mngr.stopSignal = sig
	})
}

// StopOnOSSignal uses OSStopSignal for StopOnSignal channel
func StopOnOSSignal() Option {
	return OptionFunc(func(mngr *Manager) {
		StopOnSignal(OSStopSignal()).apply(mngr)
	})
}

// New creates a new service-manager
func New(options ...Option) *Manager {
	mngr := &Manager{
		startStopping:      make(chan chan error),
		shutdownSuccessful: make(chan result),
		shutdownFailed:     make(chan Service),
		stopped:            make(chan struct{}),
		errs:               &ServiceErrors{},
	}

	for _, opt := range options {
		opt.apply(mngr)
	}

	return mngr
}

// OSStopSignal returns a channel which will be closed when SIGTERM or SIGINT is send to the program.
func OSStopSignal() <-chan struct{} {
	osStopSignal := make(chan os.Signal, 1)
	signal.Notify(osStopSignal, syscall.SIGTERM, syscall.SIGINT)

	stopSignal := make(chan struct{})
	go func() {
		<-osStopSignal
		close(stopSignal)
	}()

	return stopSignal
}

// SignalFromTime wraps channels returned from time async functions so that they can be used with StopOnSignal.
func SignalFromTime(sig <-chan time.Time) <-chan struct{} {
	stopSignal := make(chan struct{})
	go func() {
		<-sig
		close(stopSignal)
	}()

	return stopSignal
}
