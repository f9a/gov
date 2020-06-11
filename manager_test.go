package gov

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func requireTouch(t *testing.T, m *sync.Map, key interface{}) {
	t.Helper()
	is, ok := m.Load(key)
	require.True(t, ok)
	require.True(t, is.(bool))
}

func requireUntouched(t *testing.T, m *sync.Map, key interface{}) {
	t.Helper()
	_, ok := m.Load(key)
	require.False(t, ok)
}

func TestNoService(t *testing.T) {
	sm := New()
	require.Equal(t, ErrNoService, sm.Start())
}

func TestStartStop(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			touched.Store("start", true)
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
			touched.Store("stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(1 * time.Second)

	err := sm.Stop()
	require.Nil(t, err)

	err = <-done
	require.Nil(t, err)

	requireTouch(t, touched, "start")
	requireTouch(t, touched, "stop")
}

func TestStopOnSignal(t *testing.T) {
	stopSignal := make(chan struct{})
	sm := New(StopOnSignal(stopSignal))

	touched := &sync.Map{}
	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			touched.Store("start", true)
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
			touched.Store("stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(1 * time.Second)

	stopSignal <- struct{}{}

	err := <-done
	require.Nil(t, err)

	requireTouch(t, touched, "start")
	requireTouch(t, touched, "stop")
}

func TestStartStopMultiServices(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	stop1 := make(chan struct{})
	sm.Add(Service{
		Name: "service1",
		Start: func() error {
			<-stop1
			touched.Store("service1_start", true)
			return nil
		},
		Stop: func(context.Context) {
			stop1 <- struct{}{}
			touched.Store("service1_stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	stop2 := make(chan struct{})
	sm.Add(Service{
		Name: "service2",
		Start: func() error {
			<-stop2
			touched.Store("service2_start", true)
			return nil
		},
		Stop: func(context.Context) {
			stop2 <- struct{}{}
			touched.Store("service2_stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(1 * time.Second)

	err := sm.Stop()
	require.Nil(t, err)

	err = <-done
	require.Nil(t, err)

	requireTouch(t, touched, "service1_start")
	requireTouch(t, touched, "service1_stop")
	requireTouch(t, touched, "service2_start")
	requireTouch(t, touched, "service2_stop")
}

func TestKill(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	stop := make(chan struct{})
	kill := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			<-kill
			touched.Store("start", true)
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
			touched.Store("stop", true)
		},
		Kill: func() {
			kill <- struct{}{}
			touched.Store("kill", true)
		},
		StopTimeout: 1 * time.Second,
		KillTimeout: 1 * time.Second,
	})

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(1 * time.Second)

	err := sm.Stop()
	require.Nil(t, err)

	err = <-done
	require.Nil(t, err)

	requireTouch(t, touched, "start")
	requireTouch(t, touched, "stop")
	requireTouch(t, touched, "kill")
}

func TestStartError(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			touched.Store("start", true)
			return errors.New("vegan-bacon not found")
		},
		Stop: func(context.Context) {
			touched.Store("stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	err := sm.Start()
	require.EqualError(t, err, "[(service-1,vegan-bacon not found)]")

	requireTouch(t, touched, "start")
	requireUntouched(t, touched, "stop")

	err = sm.Stop()
	require.Equal(t, ErrAlreadyStopped, err)
}

func TestStartErrorMultiServices(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service1",
		Start: func() error {
			touched.Store("service1_start", true)
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			touched.Store("service1_stop", true)
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	sm.Add(Service{
		Name: "service2",
		Start: func() error {
			touched.Store("service2_start", true)
			return errors.New("vegan-bacon not found")
		},
		Stop: func(context.Context) {
			touched.Store("service2_stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	err := sm.Start()
	require.EqualError(t, err, "[(service2,vegan-bacon not found)]")

	requireTouch(t, touched, "service1_start")
	requireTouch(t, touched, "service1_stop")
	requireTouch(t, touched, "service2_start")
	requireUntouched(t, touched, "service2_stop")

	err = sm.Stop()
	require.Equal(t, ErrAlreadyStopped, err)
}

func TestStartMultiErrors(t *testing.T) {
	sm := New()

	touched := &sync.Map{}
	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service1",
		Start: func() error {
			touched.Store("service1_start", true)
			<-stop
			return errors.New("long beard found")
		},
		Stop: func(context.Context) {
			touched.Store("service1_stop", true)
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	sm.Add(Service{
		Name: "service2",
		Start: func() error {
			touched.Store("service2_start", true)
			return errors.New("vegan-bacon not found")
		},
		Stop: func(context.Context) {
			touched.Store("service2_stop", true)
		},
		StopTimeout: 5 * time.Second,
	})

	err := sm.Start()
	require.EqualError(t, err, "[(service2,vegan-bacon not found),(service1,long beard found)]")

	requireTouch(t, touched, "service1_start")
	requireTouch(t, touched, "service1_stop")
	requireTouch(t, touched, "service2_start")
	requireUntouched(t, touched, "service2_stop")

	err = sm.Stop()
	require.Equal(t, ErrAlreadyStopped, err)
}

func TestAlreadyStarted(t *testing.T) {
	sm := New()

	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	var err error

	done := make(chan error)
	go func() {
		done <- sm.Start()
	}()

	time.Sleep(1 * time.Second)

	err = sm.Start()
	require.Equal(t, ErrAlreadyStarted, err)

	err = sm.Stop()
	require.Nil(t, err)

	err = <-done
	require.Nil(t, err, "Manager.Start shouldn't return an error")
}

func TestNotStarted(t *testing.T) {
	sm := New()

	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	err := sm.Stop()
	require.Equal(t, ErrNotStarted, err)
}

func TestAlreadyStopping(t *testing.T) {
	sm := New()

	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
			// Make sure that the Service.Start method is not immediately terminated
			// so that the second call to Manager.Stop has time to be executed before
			// the service manager changes to stopped state.
			time.Sleep(2 * time.Second)
			return nil
		},
		Stop: func(context.Context) {
			stop <- struct{}{}
		},
		StopTimeout: 5 * time.Second,
	})

	startDone := make(chan error)
	go func() {
		startDone <- sm.Start()
	}()

	// Increase the probability that the Manager.Start call has already been executed.
	// Otherwise the first Manager.Stop call will return ErrNoStarted error.
	time.Sleep(1 * time.Second)

	stop1Done := make(chan error)
	go func() {
		stop1Done <- sm.Stop()
	}()

	// Increase the probability that the first Manager.Stop call has already been executed.
	// Otherwise the second Manager.Stop call will return nil and the first Manager.Stop
	// will return the error which messes up the test
	time.Sleep(1 * time.Second)

	stop2Done := make(chan error)
	go func() {
		stop2Done <- sm.Stop()
	}()

	isStop2Called := false
	for {
		select {
		case err := <-stop1Done:
			require.Nil(t, err)
		case err := <-stop2Done:
			isStop2Called = true
			require.Equal(t, ErrAlreadyStopping, err)
		case err := <-startDone:
			require.Nil(t, err)
			require.True(t, isStop2Called)
			return
		}
	}
}

func TestAlreadyStopped(t *testing.T) {
	sm := New()

	stop := make(chan struct{})
	sm.Add(Service{
		Name: "service-1",
		Start: func() error {
			<-stop
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

	// Increase the probability that the Manager.Start call has already been executed.
	time.Sleep(1 * time.Second)

	err := sm.Stop()
	require.Nil(t, err)

	err = <-done
	require.Nil(t, err)

	err = sm.Stop()
	require.Equal(t, ErrAlreadyStopped, err)
}

func TestSetupErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      string
		services func(*iWasHereInOrder, *Manager)
	}{
		{
			name: "service already exists",
			err:  "service s1 already exists",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1a := newStopService(p, "s1")
				m.Add(s1a)

				s1b := newStopService(p, "s1")
				m.Add(s1b)
			},
		},
		{
			name: "mssing name",
			err:  "service 0 Name is empty",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.Name = ""
				m.Add(s1)
			},
		},
		{
			name: "missing Start",
			err:  "s1 Start cannot be nil",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.Start = nil
				m.Add(s1)
			},
		},
		{
			name: "missing Stop",
			err:  "s1 Stop cannot be nil",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.Stop = nil
				m.Add(s1)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			places := &iWasHereInOrder{}
			sm := New()

			test.services(places, sm)

			done := make(chan error)
			go func() {
				done <- sm.Start()
			}()

			// Increase the probability that the Manager.Start call has already been executed.
			time.Sleep(500 * time.Millisecond)

			err := sm.Stop()
			if err != nil {
				if test.err == "" {
					t.Fatal(err)
				}
			}

			err = <-done
			require.EqualError(t, err, test.err)
		})
	}

}
