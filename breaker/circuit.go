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

// numHistoryReasons 熔断打开时输出的最近失败原因数量。
const numHistoryReasons = 5

// internalPromise 内部 Promise，无需上报原因（doReq 路径内部已记录）。
type internalPromise interface {
	Accept()
	Reject()
}

// internalThrottle 内部节流器，用于底层算法实现。
type internalThrottle interface {
	allow() (internalPromise, error)
	doReq(req func() error, fallback Fallback, acceptable Acceptable) error
}

// throttle 对 internalThrottle 的公开包装，Promise 需携带失败原因。
type throttle interface {
	allow() (Promise, error)
	doReq(req func() error, fallback Fallback, acceptable Acceptable) error
}

// circuitBreaker 熔断器的门面实现，组合底层算法与日志记录。
type circuitBreaker struct {
	name string
	sre  sreConfig
	throttle
}

// NewBreaker 创建熔断器，默认使用 Google SRE 算法。
// 可通过 Option 自定义，如 WithName("payment-gateway")、WithSREDefaults()。
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

// Name 返回熔断器名称。
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

// loggedThrottle 在底层算法之上记录最近失败原因，熔断打开时输出便于排查。
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

// allow 判断请求是否允许通过，返回内部 Promise 用于上报结果。
//
// 拒绝时底层返回 nil promise，此处必须同样返回 nil Promise：
// 若包装成非 nil 的 promiseWithReason（内部 promise 为 nil），
// 调用方忽略 err 直接使用返回值时会 nil 解引用 panic，
// 也与 Breaker.Allow 的契约（"允许时返回 Promise"）不符。
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

// logError 熔断打开时输出告警日志，包含最近失败原因。
func (lt *loggedThrottle) logError(err error) error {
	if errors.Is(err, ErrServiceUnavailable) {
		logger.Error("breaker: circuit breaker is open, requests dropped",
			logger.String("breaker", lt.name),
			logger.String("last_errors", lt.errWin.String()))
	}
	return err
}

// errorWindow 环形记录最近若干条失败原因。
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

	// 容量必须在锁内读取：count 由 add 在锁内更新，
	// 在加锁之前读它来 make 切片会构成数据竞争。
	reasons := make([]string, 0, ew.count)
	// 倒序输出：最近的失败原因在前
	for i := ew.index - 1; i >= ew.index-ew.count; i-- {
		reasons = append(reasons, ew.reasons[(i+numHistoryReasons)%numHistoryReasons])
	}

	return strings.Join(reasons, "\n")
}

// promiseWithReason 包装内部 Promise，Reject 时记录失败原因。
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
