package taskq

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToAsynqConfigPropagatesRetryControls verifies the retry policy can actually
// reach asynq; it used to be hard-coded to asynq's defaults.
func TestToAsynqConfigPropagatesRetryControls(t *testing.T) {
	isFailureCalled := false
	delayCalled := false

	cfg := fillDefault(Config{
		IsFailure: func(error) bool {
			isFailureCalled = true
			return false
		},
		RetryDelayFunc: func(int, error, *asynq.Task) time.Duration {
			delayCalled = true
			return time.Second
		},
		StrictPriority: true,
	})
	asynqCfg := cfg.toAsynqConfig(nil)

	require.NotNil(t, asynqCfg.IsFailure)
	assert.False(t, asynqCfg.IsFailure(errors.New("ignored")))
	assert.True(t, isFailureCalled)

	require.NotNil(t, asynqCfg.RetryDelayFunc)
	assert.Equal(t, time.Second, asynqCfg.RetryDelayFunc(1, errors.New("x"), nil))
	assert.True(t, delayCalled)

	assert.True(t, asynqCfg.StrictPriority)
}

// TestSkipRetryWrapsAsynqSentinel makes the permanent-failure shortcut available
// without importing asynq.
func TestSkipRetryWrapsAsynqSentinel(t *testing.T) {
	err := fmt.Errorf("payload is invalid: %w", SkipRetry)
	assert.ErrorIs(t, err, asynq.SkipRetry)
	assert.ErrorIs(t, err, SkipRetry)
}

// TestDefaultRetryDelayFunc delegates to asynq's backoff (which contains jitter,
// so only its direction is asserted).
func TestDefaultRetryDelayFunc(t *testing.T) {
	delay := DefaultRetryDelayFunc(1, errors.New("x"), nil)
	assert.Positive(t, delay)
	// n=1 yields 16s..74s because of the random jitter.
	assert.Less(t, delay, 2*time.Minute)
}

// TestFillDefaultKeepsRetryPolicy makes sure fillDefault does not drop the
// non-JSON retry controls (they are function fields skipped by the mapping layer).
func TestFillDefaultKeepsRetryPolicy(t *testing.T) {
	isFailure := func(error) bool { return false }
	delay := func(int, error, *asynq.Task) time.Duration { return time.Minute }

	c := fillDefault(Config{IsFailure: isFailure, RetryDelayFunc: delay, StrictPriority: true})
	assert.NotNil(t, c.IsFailure)
	assert.NotNil(t, c.RetryDelayFunc)
	assert.True(t, c.StrictPriority)
}
