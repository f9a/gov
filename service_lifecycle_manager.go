package gov

import (
	"golang.org/x/net/context"
)

type lifecycleStateMachine struct {
	service        Service
	shutdownFailed chan<- Service
	done           <-chan struct{}
}

func lifecycleKill(s lifecycleStateMachine) {
	if s.service.Kill == nil {
		s.shutdownFailed <- s.service
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.service.KillTimeout)

	go func() {
		s.service.Kill()
	}()

	select {
	case <-ctx.Done():
		cancel()
		s.shutdownFailed <- s.service
		return
	case <-s.done:
		cancel()
		return
	}
}

func lifecycleStop(s lifecycleStateMachine) {
	ctx, cancel := context.WithTimeout(context.Background(), s.service.StopTimeout)

	go func() {
		s.service.Stop(ctx)
	}()

	select {
	case <-ctx.Done():
		cancel()
		lifecycleKill(s)
		return
	case <-s.done:
		cancel()
		return
	}
}

func lifecycle(shutdownSuccessful chan<- result, shutdownFailed chan<- Service, service Service) (shutdown func()) {
	done := make(chan struct{})
	go func() {
		if err := service.Start(); err != nil {
			shutdownSuccessful <- result{
				from: service.Name,
				err:  err,
			}
		}

		done <- struct{}{}

		shutdownSuccessful <- result{
			from: service.Name,
		}
	}()

	startShutdown := make(chan struct{})

	go func() {
		<-startShutdown
		lifecycleStop(lifecycleStateMachine{
			service:        service,
			shutdownFailed: shutdownFailed,
			done:           done,
		})
	}()

	return func() {
		startShutdown <- struct{}{}
	}
}
