// Package service adapts application resources to the core/service.Service
// lifecycle for rest.Server. Keeping these adapters here keeps both the rest
// package and core/trace free of auxiliary lifecycle details.
package service

import (
	"sync"

	"github.com/chihqiang/ten-go/core/trace"
)

// TraceAgent adapts the global tracing agent to the Service lifecycle. It is
// added to a ServiceGroup through rest.Server.Register.
type TraceAgent struct {
	config trace.Config

	mu      sync.Mutex
	started bool
	stopped bool
	owns    bool
}

// NewTraceAgent creates a trace agent service.
func NewTraceAgent(config trace.Config) *TraceAgent {
	return &TraceAgent{config: config}
}

// Start installs the agent once and returns immediately.
func (s *TraceAgent) Start() {
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	if s.config.Endpoint == "" {
		return
	}

	owns := trace.EnsureAgent(s.config)

	s.mu.Lock()
	s.owns = owns
	stopped := s.stopped
	s.mu.Unlock()
	if stopped && owns {
		// Stop raced with Start and could not see ownership yet.
		trace.StopAgent()
	}
}

// Stop releases the agent only when this service started it. It is idempotent.
func (s *TraceAgent) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	owns := s.owns
	s.mu.Unlock()

	if owns {
		trace.StopAgent()
	}
}
