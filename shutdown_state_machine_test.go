package gov

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type iWasHereInOrder struct {
	log []string
	m   sync.Mutex
}

func (tm *iWasHereInOrder) visit(where string) {
	tm.m.Lock()
	tm.log = append(tm.log, where)
	tm.m.Unlock()
}

func (tm *iWasHereInOrder) compareLog(t *testing.T, want []string) {
	tm.m.Lock()
	defer tm.m.Unlock()
	require.Equal(t, want, tm.log)
}

func newStopService(places *iWasHereInOrder, name string) Service {
	stop := make(chan struct{})
	return Service{
		Name: name,
		Start: func() error {
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			places.visit(name)
			stop <- struct{}{}
		},
		StopTimeout: 1 * time.Second,
	}
}

func newStopServiceDelay(places *iWasHereInOrder, name string, delay time.Duration) Service {
	stop := make(chan struct{})
	return Service{
		Name: name,
		Start: func() error {
			<-stop
			return nil
		},
		Stop: func(context.Context) {
			time.Sleep(delay)
			places.visit(name)
			stop <- struct{}{}
		},
		StopTimeout: delay + 1*time.Second,
	}
}

func newKillService(places *iWasHereInOrder, name string) Service {
	kill := make(chan struct{})
	return Service{
		Name: name,
		Start: func() error {
			<-kill
			return nil
		},
		Stop: func(context.Context) {
			places.visit(name)
			kill <- struct{}{}
		},
		KillTimeout: 1 * time.Second,
	}
}

func TestShutdownStateMachine(t *testing.T) {
	tests := []struct {
		name     string
		services func(*iWasHereInOrder, *Manager)
		log      []string
		err      string
	}{
		{
			name: "stop-before",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.StopBefore = []ServiceName{"s2"}
				m.Add(s1)

				m.Add(newStopService(p, "s2"))
			},
			log: []string{"s1", "s2"},
		},
		{
			name: "stop-after",
			services: func(p *iWasHereInOrder, m *Manager) {
				m.Add(newStopService(p, "s1"))

				s2 := newStopService(p, "s2")
				s2.StopAfter = []ServiceName{"s1"}
				m.Add(s2)
			},
			log: []string{"s1", "s2"},
		},
		{
			name: "stop-after-services",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				m.Add(s1)

				s2 := newStopServiceDelay(p, "s2", 100*time.Millisecond)
				m.Add(s2)

				s3 := newStopService(p, "s3")
				s3.StopAfter = []ServiceName{"s1", "s2"}
				m.Add(s3)
			},
			log: []string{"s1", "s2", "s3"},
		},
		{
			name: "stop-before-services",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				m.Add(s1)

				s2 := newStopServiceDelay(p, "s2", 100*time.Millisecond)
				m.Add(s2)

				s3 := newStopService(p, "s3")
				s3.StopBefore = []ServiceName{"s1", "s2"}
				m.Add(s3)
			},
			log: []string{"s3", "s1", "s2"},
		},
		{
			name: "complex",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				m.Add(s1)

				s2 := newStopServiceDelay(p, "s2", 100*time.Millisecond)
				m.Add(s2)

				s3 := newStopService(p, "s3")
				s3.StopAfter = []ServiceName{"s2"}
				m.Add(s3)

				s4 := newStopService(p, "s4")
				s4.StopAfter = []ServiceName{"s3"}
				m.Add(s4)

				s5 := newStopServiceDelay(p, "s5", 200*time.Millisecond)
				m.Add(s5)

				s6 := newKillService(p, "s6")
				s6.StopAfter = []ServiceName{"s5"}
				s6.StopBefore = []ServiceName{"s4"}
				m.Add(s6)

				s7 := newKillService(p, "s7")
				s7.StopAfter = []ServiceName{"s4"}
				m.Add(s7)
			},
			log: []string{"s1", "s2", "s3", "s5", "s6", "s4", "s7"},
		},
		{
			name: "cycle",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.StopAfter = []ServiceName{"s2"}
				m.Add(s1)

				s2 := newStopService(p, "s2")
				s2.StopAfter = []ServiceName{"s1"}
				m.Add(s2)
			},
			err: "found cycle in shutdown graph",
		},
		{
			name: "unknown service in StopBefore",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.StopBefore = []ServiceName{"s2"}
				m.Add(s1)
			},
			err: "s1 mention unknown service s2 in StopBefore",
		},
		{
			name: "unknown service in StopAfter",
			services: func(p *iWasHereInOrder, m *Manager) {
				s1 := newStopService(p, "s1")
				s1.StopAfter = []ServiceName{"s2"}
				m.Add(s1)
			},
			err: "s1 mention unknown service s2 in StopAfter",
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
			time.Sleep(1 * time.Second)

			err := sm.Stop()
			if err != nil {
				if test.err == "" {
					t.Fatal(err)
				}
			}

			err = <-done
			if err != nil || test.err != "" {
				if test.err != "" {
					require.EqualError(t, err, test.err)
					return
				}

				t.Fatal(err)
			}
			places.compareLog(t, test.log)
		})
	}
}
