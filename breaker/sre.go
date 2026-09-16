package breaker

import (
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Google SRE breaker algorithm parameters.
const (
	// window is the total length of the statistical window: 10 seconds.
	window = time.Second * 10
	// buckets is the number of buckets in the rolling window.
	buckets = 40
	// forcePassDuration is the forced-pass interval: when nothing has passed for
	// longer than this, one probe request is let through (half-open state).
	forcePassDuration = time.Second
	// k is the request amplification factor of the rejection formula, default 1.5.
	k = 1.5
	// minK is the lower bound of the amplification factor, keeping the weight from
	// becoming too small.
	minK = 1.1
	// protection is the low-traffic guard: requests are not rejected while the total
	// number of requests is below this value.
	protection = 5
)

// googleBreaker implements the Google SRE client-side throttling algorithm (see
// the Client-Side Throttling section of
// https://landing.google.com/sre/sre-book/chapters/handling-overload/).
//
// The rejection probability is computed from the request/success/failure statistics
// of the rolling window:
//
//	weightedAccepts = max(k - (k-minK) * failingBuckets/buckets, minK) * accepts
//	dropRatio = (total - protection - weightedAccepts) / (total + 1)
//
// When dropRatio > 0 requests are rejected probabilistically; after a cool-down
// period one probe request is forced through.
type googleBreaker struct {
	k          float64
	minK       float64
	stat       *rollingWindow
	proba      *proba
	lastPass   *atomicNano
	protection int64
}

// windowResult is the aggregated result of the rolling window.
type windowResult struct {
	accepts        int64 // successes
	total          int64 // total requests
	failingBuckets int64 // number of consecutive failing buckets
	workingBuckets int64 // number of buckets with successes
}

// newGoogleBreaker creates a Google SRE breaker using the algorithm parameters in cfg.
func newGoogleBreaker(cfg sreConfig) *googleBreaker {
	return &googleBreaker{
		k:          cfg.k,
		minK:       cfg.minK,
		protection: cfg.protection,
		stat:       newRollingWindow(buckets, cfg.window/buckets),
		proba:      newProba(),
		lastPass:   newAtomicNano(),
	}
}

// accept decides whether the request may pass; nil means allowed and
// ErrServiceUnavailable means rejected.
func (b *googleBreaker) accept() error {
	history := b.history()

	// Weighted accepts: the more concentrated the failures (the more consecutive
	// failing buckets), the lower the weight and the more likely a rejection
	w := b.k - (b.k-b.minK)*float64(history.failingBuckets)/buckets
	weightedAccepts := math.Max(w, b.minK) * float64(history.accepts)

	// Google SRE rejection formula; when total is tiny (< protection) the result is
	// non-positive, so nothing is rejected
	dropRatio := (float64(history.total-b.protection) - weightedAccepts) / float64(history.total+1)
	if dropRatio <= 0 {
		return nil
	}

	// Half-open: more than a cool-down since the last pass, force one probe through
	lastPass := b.lastPass.Load()
	if lastPass > 0 && time.Now().UnixNano()-lastPass > int64(forcePassDuration) {
		b.lastPass.Set(time.Now().UnixNano())
		return nil
	}

	// The higher the share of buckets with successes, the lower the rejection
	// probability (healthy periods dilute it)
	dropRatio *= float64(buckets-history.workingBuckets) / buckets

	if b.proba.TrueOnProba(dropRatio) {
		return ErrServiceUnavailable
	}

	b.lastPass.Set(time.Now().UnixNano())
	return nil
}

// allow decides whether the request may pass and returns an internal Promise for
// reporting the outcome.
func (b *googleBreaker) allow() (internalPromise, error) {
	if err := b.accept(); err != nil {
		b.markDrop()
		return nil, err
	}
	return googlePromise{b: b}, nil
}

// doReq runs the request: first decide whether it is allowed, then run it and
// report success/failure.
// When fallback is non-nil, an open breaker goes through the fallback logic.
func (b *googleBreaker) doReq(req func() error, fallback Fallback, acceptable Acceptable) error {
	if err := b.accept(); err != nil {
		b.markDrop()
		if fallback != nil {
			return fallback(err)
		}
		return err
	}

	var succ bool
	defer func() {
		// When the request panics, succ stays false and it counts as a failure; the
		// panic itself keeps propagating upwards
		if succ {
			b.markSuccess()
		} else {
			b.markFailure()
		}
	}()

	err := req()
	if acceptable(err) {
		succ = true
	}
	return err
}

// history aggregates the statistics of the rolling window.
// The traversal is inlined here rather than exposed as a public method on
// rollingWindow: accept calls it on every decision, and on the hot path the cost
// of a callback closure plus a modulo per bucket is not negligible.
// Note: workingBuckets/failingBuckets depend on the traversal order (they count
// consecutive success/failure buckets), so they cannot be maintained
// incrementally and the traversal stays; the two loops avoid one % size per bucket.
func (b *googleBreaker) history() windowResult {
	var result windowResult
	rw := b.stat

	rw.lock.RLock()
	span := rw.span()
	diff := rw.size - span
	if diff > 0 {
		start := (rw.offset + span + 1) % rw.size
		end := start + diff
		for i := start; i < rw.size && i < end; i++ {
			aggregate(&result, &rw.buckets[i])
		}
		for i := 0; i < end-rw.size; i++ {
			aggregate(&result, &rw.buckets[i])
		}
	}
	rw.lock.RUnlock()
	return result
}

// aggregate accumulates a single bucket into the result (called from the inlined
// loop in history, so it can be inlined).
func aggregate(result *windowResult, bk *bucket) {
	result.accepts += bk.Success
	result.total += bk.Sum
	if bk.Failure > 0 {
		result.workingBuckets = 0
	} else if bk.Success > 0 {
		result.workingBuckets++
	}
	if bk.Success > 0 {
		result.failingBuckets = 0
	} else if bk.Failure > 0 {
		result.failingBuckets++
	}
}

func (b *googleBreaker) markDrop()    { b.stat.add(drop) }
func (b *googleBreaker) markFailure() { b.stat.add(fail) }
func (b *googleBreaker) markSuccess() { b.stat.add(success) }

// googlePromise implements internalPromise, reporting the outcome to the breaker.
type googlePromise struct {
	b *googleBreaker
}

func (p googlePromise) Accept() { p.b.markSuccess() }
func (p googlePromise) Reject() { p.b.markFailure() }

// proba decides whether to act based on a probability, used to sample by
// rejection ratio.
// It uses the global functions of math/rand/v2, which are concurrency-safe and
// need no extra lock.
type proba struct{}

func newProba() *proba {
	return &proba{}
}

// TrueOnProba returns true with probability prob.
func (p *proba) TrueOnProba(prob float64) bool {
	if prob <= 0 {
		return false
	}
	return rand.Float64() < prob
}

// atomicNano atomically stores a UnixNano timestamp, used to record the last pass
// time.
type atomicNano struct {
	val atomic.Int64
}

func newAtomicNano() *atomicNano { return &atomicNano{} }

func (a *atomicNano) Load() int64 { return a.val.Load() }

func (a *atomicNano) Set(v int64) { a.val.Store(v) }
