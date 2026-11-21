package service

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chihqiang/ten-go/core/logger"
	"github.com/chihqiang/ten-go/core/trace"
)

func TestLoggerSync(t *testing.T) {
	// A nil logger is a no-op.
	var nilSync LoggerSync
	assert.NotPanics(t, func() {
		nilSync.Start()
		nilSync.Stop()
	})

	l := logger.New(logger.Config{})
	sync := NewLoggerSync(l)
	assert.NotNil(t, sync)
	assert.NotPanics(t, func() {
		sync.Start()
		sync.Stop()
	})
}

func TestTraceAgent_NoEndpoint(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	agent := NewTraceAgent(trace.Config{Name: "no-endpoint"})
	agent.Start()
	agent.Start() // idempotent
	assert.False(t, trace.IsAgentRunning())

	agent.Stop()
	agent.Stop() // idempotent
}

func TestTraceAgent_Disabled(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	agent := NewTraceAgent(trace.Config{Disabled: true, Endpoint: filepath.Join(t.TempDir(), "trace.log")})
	agent.Start()
	assert.False(t, trace.IsAgentRunning())
	agent.Stop()
}

func TestTraceAgent_OwnsLifecycle(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	agent := NewTraceAgent(trace.Config{
		Name:     "service-agent",
		Endpoint: filepath.Join(t.TempDir(), "trace.log"),
		Batcher:  trace.BatcherFile,
	})
	agent.Start()
	require.True(t, trace.IsAgentRunning())

	agent.Stop()
	assert.False(t, trace.IsAgentRunning())
}

func TestTraceAgent_DoesNotStopExistingAgent(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	// The application already started an agent.
	trace.StartAgent(trace.Config{Name: "app", Sampler: 1.0})
	require.True(t, trace.IsAgentRunning())

	agent := NewTraceAgent(trace.Config{
		Name:     "service-agent",
		Endpoint: filepath.Join(t.TempDir(), "trace.log"),
		Batcher:  trace.BatcherFile,
	})
	agent.Start()
	agent.Stop() // must not stop the application's agent

	assert.True(t, trace.IsAgentRunning(), "an agent started by the application must keep running")
}

func TestTraceAgent_StopBeforeStart(t *testing.T) {
	trace.StopAgent()
	t.Cleanup(trace.StopAgent)

	agent := NewTraceAgent(trace.Config{})
	agent.Stop()
	agent.Start() // stopped already: must be a no-op
	assert.False(t, trace.IsAgentRunning())
}
