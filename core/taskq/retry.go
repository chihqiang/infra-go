package taskq

import "github.com/hibiken/asynq"

// RetryDelayFunc is the signature of asynq's retry backoff function; it is
// re-exported so callers do not need to import asynq just to tune the backoff.
type RetryDelayFunc = asynq.RetryDelayFunc

// SkipRetry stops retrying the current task immediately. Return it directly or
// wrap it with %w from a handler for permanent failures (validation errors,
// malformed payloads, ...). Without it, a permanently failing task is retried
// according to DefaultMaxRetry (25 times by default, about 25 days with asynq's
// default backoff).
var SkipRetry = asynq.SkipRetry

// DefaultRetryDelayFunc is asynq's default exponential backoff. Wrap it through
// Config.RetryDelayFunc to cap the delay while keeping the default shape.
var DefaultRetryDelayFunc = asynq.DefaultRetryDelayFunc
