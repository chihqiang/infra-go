package breaker

import (
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Google SRE 熔断算法参数。
const (
	// window 统计窗口总时长：10 秒。
	window = time.Second * 10
	// buckets 滑动窗口桶数量。
	buckets = 40
	// forcePassDuration 强制放行间隔：超过该时长未放行则放行一个探测请求（半开状态）。
	forcePassDuration = time.Second
	// k 拒绝公式中的请求放大系数，默认 1.5。
	k = 1.5
	// minK 放大系数下限，防止权重过小。
	minK = 1.1
	// protection 小流量保护：总请求数低于该值时不被拒绝。
	protection = 5
)

// googleBreaker 基于 Google SRE 客户端限流算法（见
// https://landing.google.com/sre/sre-book/chapters/handling-overload/ 的
// Client-Side Throttling 一节）。
//
// 拒绝概率由滑动窗口内的请求/成功/失败统计计算：
//
//	weightedAccepts = max(k - (k-minK) * failingBuckets/buckets, minK) * accepts
//	dropRatio = (total - protection - weightedAccepts) / (total + 1)
//
// dropRatio > 0 时按概率拒绝请求；冷却期后强制放行一个探测请求。
type googleBreaker struct {
	k          float64
	minK       float64
	stat       *rollingWindow
	proba      *proba
	lastPass   *atomicNano
	protection int64
}

// windowResult 滑动窗口聚合结果。
type windowResult struct {
	accepts        int64 // 成功数
	total          int64 // 总请求数
	failingBuckets int64 // 连续失败桶数
	workingBuckets int64 // 有成功的桶数
}

// newGoogleBreaker 创建 Google SRE 熔断器，使用 cfg 中的算法参数。
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

// accept 判断请求是否允许通过；返回 nil 允许，返回 ErrServiceUnavailable 拒绝。
func (b *googleBreaker) accept() error {
	history := b.history()

	// 计算加权成功数：失败越集中（连续失败桶越多），权重越低，越容易被拒绝
	w := b.k - (b.k-b.minK)*float64(history.failingBuckets)/buckets
	weightedAccepts := math.Max(w, b.minK) * float64(history.accepts)

	// Google SRE 拒绝公式；total 很小（< protection）时结果非正，天然不拒绝
	dropRatio := (float64(history.total-b.protection) - weightedAccepts) / float64(history.total+1)
	if dropRatio <= 0 {
		return nil
	}

	// 半开：距上次放行超过冷却期，强制放行一个探测请求
	lastPass := b.lastPass.Load()
	if lastPass > 0 && time.Now().UnixNano()-lastPass > int64(forcePassDuration) {
		b.lastPass.Set(time.Now().UnixNano())
		return nil
	}

	// 有成功请求的桶占比越高，拒绝概率越低（工作正常的时段被稀释）
	dropRatio *= float64(buckets-history.workingBuckets) / buckets

	if b.proba.TrueOnProba(dropRatio) {
		return ErrServiceUnavailable
	}

	b.lastPass.Set(time.Now().UnixNano())
	return nil
}

// allow 判断请求是否允许通过，返回内部 Promise 用于上报结果。
func (b *googleBreaker) allow() (internalPromise, error) {
	if err := b.accept(); err != nil {
		b.markDrop()
		return nil, err
	}
	return googlePromise{b: b}, nil
}

// doReq 执行请求：先判断是否允许，再执行并上报成功/失败。
// fallback 非空时，熔断打开走降级逻辑。
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
		// 请求 panic 时 succ 为 false，视为失败；同时 panic 会继续向上传播
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

// history 聚合滑动窗口内的统计结果。
// 内联遍历以去掉 reduce 的回调闭包与逐桶取模开销（accept 每次判定都调用）。
// 语义与原 reduce 完全等价：按时间从旧到新遍历所有有效桶。
// 注意：workingBuckets/failingBuckets 依赖遍历顺序（连续成功/失败桶计数），
// 因此无法增量维护，这里保持遍历；两段循环避免了每桶一次的 % size 取模。
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

// aggregate 将单个桶累加进聚合结果（供 history 内联循环调用，可被内联）。
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

// googlePromise 实现 internalPromise，将结果上报给熔断器。
type googlePromise struct {
	b *googleBreaker
}

func (p googlePromise) Accept() { p.b.markSuccess() }
func (p googlePromise) Reject() { p.b.markFailure() }

// proba 基于概率判断是否执行，用于按拒绝概率采样。
// 使用 math/rand/v2 的全局函数，并发安全且无额外锁。
type proba struct{}

func newProba() *proba {
	return &proba{}
}

// TrueOnProba 以 prob 概率返回 true。
func (p *proba) TrueOnProba(prob float64) bool {
	if prob <= 0 {
		return false
	}
	return rand.Float64() < prob
}

// atomicNano 原子存储 UnixNano 时刻，用于记录上次放行时间。
type atomicNano struct {
	val atomic.Int64
}

func newAtomicNano() *atomicNano { return &atomicNano{} }

func (a *atomicNano) Load() int64 { return a.val.Load() }

func (a *atomicNano) Set(v int64) { a.val.Store(v) }
