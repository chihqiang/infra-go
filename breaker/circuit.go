package breaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chihqiang/infra-go/logger"
)

// numHistoryReasons is how many recent failure reasons are reported when the
// breaker opens.
const numHistoryReasons = 5

// internalPromise is the internal Promise; it does not need to report a reason
// (the doReq path records it internally).
type internalPromise interface {
	Accept()
	Reject()
}

// internalThrottle is the internal throttler used by the low-level algorithm
// implementations.
type internalThrottle interface {
	allow() (internalPromise, error)
	doReq(req func() error, fallback Fallback, acceptable Acceptable) error
}

// throttle is the public wrapper around internalThrottle; its Promise must carry
// a failure reason.
type throttle interface {
	allow() (Promise, error)
	doReq(req func() error, fallback Fallback, acceptable Acceptable) error
}

// circuitBreaker is the facade implementation of the breaker, combining the
// low-level algorithm with logging.
type circuitBreaker struct {
	name string
	sre  sreConfig
	throttle
}

// NewBreaker creates a breaker that uses the Google SRE algorithm by default.
// It can be customised with Options such as WithName("payment-gateway") or
// WithSREDefaults().
func NewBreaker(opts ...Option) Breaker {
	b := circuitBreaker{sre: defaultSREConfig()}
	for _, opt := range opts {
		opt(&b)
	}
	if b.name == "" {
		b.name = "breaker"
	}
	b.throttle = newLoggedThrottle(b.name, newGoogleBreaker(b.sre))
	return &b
}

// Name returns the breaker name.
func (cb *circuitBreaker) Name() string {
	return cb.name
}

func (cb *circuitBreaker) Allow() (Promise, error) {
	return cb.throttle.allow()
}

func (cb *circuitBreaker) AllowCtx(ctx context.Context) (Promise, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return cb.Allow()
	}
}

func (cb *circuitBreaker) Do(req func() error) error {
	return cb.throttle.doReq(req, nil, defaultAcceptable)
}

func (cb *circuitBreaker) DoCtx(ctx context.Context, req func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.Do(req)
	}
}

func (cb *circuitBreaker) DoWithAcceptable(req func() error, acceptable Acceptable) error {
	return cb.throttle.doReq(req, nil, acceptable)
}

func (cb *circuitBreaker) DoWithAcceptableCtx(ctx context.Context, req func() error,
	acceptable Acceptable) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithAcceptable(req, acceptable)
	}
}

func (cb *circuitBreaker) DoWithFallback(req func() error, fallback Fallback) error {
	return cb.throttle.doReq(req, fallback, defaultAcceptable)
}

func (cb *circuitBreaker) DoWithFallbackCtx(ctx context.Context, req func() error,
	fallback Fallback) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithFallback(req, fallback)
	}
}

func (cb *circuitBreaker) DoWithFallbackAcceptable(req func() error, fallback Fallback,
	acceptable Acceptable) error {
	return cb.throttle.doReq(req, fallback, acceptable)
}

func (cb *circuitBreaker) DoWithFallbackAcceptableCtx(ctx context.Context, req func() error,
	fallback Fallback, acceptable Acceptable) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithFallbackAcceptable(req, fallback, acceptable)
	}
}

// loggedThrottle records the recent failure reasons on top of the low-level
// algorithm, so they can be logged when the breaker opens.
type loggedThrottle struct {
	name string
	internalThrottle
	errWin *errorWindow
}

func newLoggedThrottle(name string, t internalThrottle) *loggedThrottle {
	return &loggedThrottle{
		name:             name,
		internalThrottle: t,
		errWin:           newErrorWindow(),
	}
}

// allow decides whether the request may pass and returns an internal Promise for
// reporting the outcome.
//
// On rejection the low-level implementation returns a nil promise, and this must
// return a nil Promise too: wrapping it into a non-nil promiseWithReason (with a
// nil inner promise) would panic with a nil dereference for callers that ignore
// err and use the returned value directly, and it would also violate the
// Breaker.Allow contract ("return a Promise when allowed").
func (lt *loggedThrottle) allow() (Promise, error) {
	promise, err := lt.internalThrottle.allow()
	if err != nil {
		return nil, lt.logError(err)
	}
	return promiseWithReason{
		promise: promise,
		errWin:  lt.errWin,
	}, nil
}

func (lt *loggedThrottle) doReq(req func() error, fallback Fallback, acceptable Acceptable) error {
	return lt.logError(lt.internalThrottle.doReq(req, fallback, func(err error) bool {
		accept := acceptable(err)
		if !accept && err != nil {
			lt.errWin.add(err.Error())
		}
		return accept
	}))
}

// logError logs a warning when the breaker is open, including the recent failure
// reasons.
func (lt *loggedThrottle) logError(err error) error {
	if errors.Is(err, ErrServiceUnavailable) {
		logger.Error("breaker: circuit breaker is open, requests dropped",
			logger.String("breaker", lt.name),
			logger.String("last_errors", lt.errWin.String()))
	}
	return err
}

// errorWindow keeps the most recent failure reasons in a ring buffer.
type errorWindow struct {
	reasons [numHistoryReasons]string
	index   int
	count   int
	lock    sync.Mutex
}

func newErrorWindow() *errorWindow { return &errorWindow{} }

func (ew *errorWindow) add(reason string) {
	ew.lock.Lock()
	ew.reasons[ew.index] = fmt.Sprintf("%s %s", time.Now().Format(time.TimeOnly), reason)
	ew.index = (ew.index + 1) % numHistoryReasons
	ew.count = min(ew.count+1, numHistoryReasons)
	ew.lock.Unlock()
}

func (ew *errorWindow) String() string {
	ew.lock.Lock()
	defer ew.lock.Unlock()

	// The capacity must be read while holding the lock: count is updated by add
	// under the lock, so reading it before locking to make the slice would be a
	// data race.
	reasons := make([]string, 0, ew.count)
	// Newest first: the most recent failure reason comes first
	for i := ew.index - 1; i >= ew.index-ew.count; i-- {
		reasons = append(reasons, ew.reasons[(i+numHistoryReasons)%numHistoryReasons])
	}

	return strings.Join(reasons, "\n")
}

// promiseWithReason wraps an internal Promise and records the failure reason on
// Reject.
type promiseWithReason struct {
	promise internalPromise
	errWin  *errorWindow
}

func (p promiseWithReason) Accept() {
	p.promise.Accept()
}

func (p promiseWithReason) Reject(reason string) {
	p.errWin.add(reason)
	p.promise.Reject()
}
