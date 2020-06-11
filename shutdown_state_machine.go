package gov

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourbasic/graph"
)

type shutdownGraph struct {
	g          *graph.Mutable
	nameToNode map[ServiceName]int
	services   []Service
	// waitFor keeps track of incoming node connections.
	// We have to wait for all incoming connections to
	// finish the shutdown before we can shutdown the node (service) it self.
	waitFor []int
	lock    sync.Mutex
}

func (g *shutdownGraph) getService(n int) Service {
	return g.services[n-1]
}

// hasEveryoneArrived checks if anyone has arrived wich we are waiting for, if yes return true.
// If not, remove one from guest-list and return false.
func (g *shutdownGraph) hasEveryoneArrived(n int) bool {
	g.lock.Lock()
	defer g.lock.Unlock()
	if g.waitFor[n] == 1 {
		return true
	}

	g.waitFor[n]--
	return false
}

func newShutdownGraph(services []Service) (*shutdownGraph, error) {
	g := graph.New(len(services) + 1)
	m := map[ServiceName]int{}
	for i, service := range services {
		m[service.Name] = i + 1
	}

	waitFor := make([]int, len(services)+1)
	for i, service := range services {
		id := i + 1
		if len(service.StopAfter) == 0 && len(service.StopBefore) == 0 {
			g.Add(0, id)
		}

		for _, after := range service.StopAfter {
			if after == service.Name {
				return &shutdownGraph{}, fmt.Errorf("%s cannot mention his self in StopAfter", service.Name)
			}

			aid, ok := m[after]
			if !ok {
				return &shutdownGraph{}, fmt.Errorf("%s mention unknown service %s in StopAfter", service.Name, after)
			}

			g.Add(aid, id)
			waitFor[id]++
		}

		for _, before := range service.StopBefore {
			if before == service.Name {
				return &shutdownGraph{}, fmt.Errorf("%s cannot mention his self in StopBefore", service.Name)
			}

			bid, ok := m[before]
			if !ok {
				return &shutdownGraph{}, fmt.Errorf("%s mention unknown service %s in StopBefore", service.Name, before)
			}
			g.Add(id, bid)
			waitFor[bid]++
		}
	}

	// Fix connections to root-node (0).
	// If a service has no StopAfter or StopBefore relations defined,
	// the service is first linked to the root node. However, it can
	// happen that later on the service is defined as "After" by another
	// service. Since all services that are linked to the root node are
	// stopped right at the beginning, the link to the root node is no
	// longer correct, so the link to the root node must be deleted.
	// Services that have no incoming connections can be stopped directly
	// at begin and can therefore be linked to the root node.
	for i := 1; i < len(waitFor); i++ {
		in := waitFor[i]
		if in > 0 {
			g.Delete(0, i)
		} else if in == 0 {
			g.Add(0, i)
		}
	}

	if !graph.Acyclic(g) {
		return &shutdownGraph{}, errors.New("found cycle in shutdown graph")
	}

	return &shutdownGraph{
		g:          g,
		nameToNode: m,
		services:   services,
		waitFor:    waitFor,
	}, nil
}

type shutdownStateMachine struct {
	graph           *shutdownGraph
	shutdownService chan<- shutdownRequest
}

func shutdownService(sm shutdownStateMachine, node int) {
	service := sm.graph.getService(node)
	serviceShutdownDone := make(chan struct{})
	sm.shutdownService <- shutdownRequest{
		service: service,
		from:    serviceShutdownDone,
	}
	<-serviceShutdownDone

	sm.graph.g.Visit(node, func(n int, _ int64) bool {
		if sm.graph.hasEveryoneArrived(n) {
			go shutdownService(sm, n)
		}
		return false
	})
}

func (sm shutdownStateMachine) Start() {
	sm.graph.g.Visit(0, func(n int, _ int64) bool {
		go shutdownService(sm, n)
		return false
	})
}
