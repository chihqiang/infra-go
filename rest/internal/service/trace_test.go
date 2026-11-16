package service

import (
	"path/filepath"
	"testing"

	"github.com/chihqiang/infra-go/core/trace"
)

func TestTraceAgentReusesApplicationAgent(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	agent := trace.Config{
		Name:     "test-service",
		Batcher:  trace.BatcherFile,
		Endpoint: filepath.Join(t.TempDir(), "traces.json"),
	}
	if !trace.EnsureAgent(agent) {
		t.Fatal("failed to start the application agent")
	}

	svc := NewTraceAgent(agent)
	svc.Start()
	if !trace.IsAgentRunning() {
		t.Fatal("existing agent disappeared")
	}
	svc.Stop()
	if !trace.IsAgentRunning() {
		t.Fatal("trace service must not stop an application-owned agent")
	}
}
