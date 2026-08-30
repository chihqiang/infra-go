package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var bgCtx = context.Background()

// newTestCache 创建一个内存缓存，测试结束后自动 Close。
func newTestCache(t *testing.T, expire time.Duration, opts ...MemCacheOption) *MemCache {
	t.Helper()
	c := NewMemCache(bgCtx, expire, opts...)
	t.Cleanup(c.Close)
	return c
}

// TestCacheInterface 编译期断言：MemCache 实现 Cache 接口。
func TestCacheInterface(t *testing.T) {
	var _ Cache = NewMemCache(bgCtx, time.Minute)
}

func TestSetGet(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "name", "chihqiang"))
	v, err := c.Get(bgCtx, "name")
	assert.NoError(t, err)
	assert.Equal(t, "chihqiang", v)

	// 非泛型：一个实例可存多种类型值
	assert.NoError(t, c.Set(bgCtx, "count", 42))
	iv, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, 42, iv)
}

func TestGetMiss(t *testing.T) {
	c := newTestCache(t, time.Minute)

	_, err := c.Get(bgCtx, "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDel(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	assert.NoError(t, c.Delete(bgCtx, "k"))
	_, err := c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteMultipleKeys(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.NoError(t, c.Delete(bgCtx, "a", "b"))
	assert.Equal(t, 0, c.Size())
}

func TestExpire(t *testing.T) {
	c := newTestCache(t, 50*time.Millisecond)

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)

	// 等过期（考虑 5% 抖动，多等一些时间）
	time.Sleep(120 * time.Millisecond)
	_, err = c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetExOverride(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// 短过期写入，随后用默认过期重写，旧定时器不应误删新值
	assert.NoError(t, c.SetEx(bgCtx, "k", "old", 30*time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	assert.NoError(t, c.Set(bgCtx, "k", "new"))

	time.Sleep(50 * time.Millisecond)
	v, err := c.Get(bgCtx, "k")
	assert.NoError(t, err, "新值不应被旧定时器删除")
	assert.Equal(t, "new", v)
}

func TestLruEvict(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.Equal(t, 2, c.Size())

	// 访问 a，使 a 成为最近使用，b 变成最久未使用
	_, _ = c.Get(bgCtx, "a")
	assert.NoError(t, c.Set(bgCtx, "c", "3")) // 触发淘汰，应淘汰 b

	_, err := c.Get(bgCtx, "b")
	assert.ErrorIs(t, err, ErrNotFound, "b 应被 LRU 淘汰")
	_, err = c.Get(bgCtx, "a")
	assert.NoError(t, err)
	_, err = c.Get(bgCtx, "c")
	assert.NoError(t, err)
}

func TestTake(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "db-value", nil
	}

	v, err := c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))

	// 第二次命中缓存，不再调用 fetch
	v, err = c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "db-value", v)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestTakeConcurrent(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond) // 模拟慢查询
		return "db-value", nil
	}

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			v, err := c.Take(bgCtx, "k", fetch)
			// 注意：子 goroutine 内只能用 assert，不能用 require（FailNow
			// 会在子 goroutine 内 Goexit，跳过 wg.Done() 导致挂死）。
			assert.NoError(t, err)
			assert.Equal(t, "db-value", v)
		}()
	}
	wg.Wait()

	// 并发 Take 只执行一次 fetch（防缓存击穿）
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Equal(t, 1, c.Size())
}

func TestTakeFetchError(t *testing.T) {
	c := newTestCache(t, time.Minute)

	_, err := c.Take(bgCtx, "k", func() (any, error) {
		return nil, assert.AnError
	})
	assert.Error(t, err)
	assert.Equal(t, 0, c.Size(), "fetch 失败不应写入缓存")
}

func TestNoExpire(t *testing.T) {
	c := newTestCache(t, 0) // 默认不过期

	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	time.Sleep(20 * time.Millisecond)
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err, "expire=0 时永不过期")
}

func TestClose(t *testing.T) {
	c := NewMemCache(bgCtx, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))
	c.Close()
	c.Close() // 幂等，不 panic
}

// --- 补充用例：选项、边界行为、覆盖缺口、bug 回归 ---

func TestWithName(t *testing.T) {
	c := NewMemCache(bgCtx, time.Minute, WithName("users"))
	assert.Equal(t, "users", c.name)
	t.Cleanup(c.Close)

	d := NewMemCache(bgCtx, time.Minute)
	assert.Equal(t, defaultCacheName, d.name)
	t.Cleanup(d.Close)
}

func TestNoLimitKeepsAll(t *testing.T) {
	c := newTestCache(t, time.Minute)
	for i := 0; i < 1000; i++ {
		assert.NoError(t, c.Set(bgCtx, fmt.Sprintf("k%d", i), i))
	}
	assert.Equal(t, 1000, c.Size(), "无容量限制时不应淘汰任何 key")
}

func TestOverwrite(t *testing.T) {
	c := newTestCache(t, time.Minute)

	assert.NoError(t, c.Set(bgCtx, "k", "old"))
	assert.NoError(t, c.Set(bgCtx, "k", "new"))
	v, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)
	assert.Equal(t, "new", v)
	assert.Equal(t, 1, c.Size(), "覆盖写不应增加元素数量")
}

func TestDeleteMissingKey(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Delete(bgCtx, "nope"))
}

func TestDeleteOnLimitedCache(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	assert.NoError(t, c.Delete(bgCtx, "a")) // 触发 keyLru.remove

	_, err := c.Get(bgCtx, "a")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = c.Get(bgCtx, "b")
	assert.NoError(t, err)
	assert.Equal(t, 1, c.Size())

	// 删除后 a 的 LRU 记录已清理，再次 Set 应正常
	assert.NoError(t, c.Set(bgCtx, "a", "new"))
	assert.Equal(t, 2, c.Size())
}

func TestExpireOnLimitedCache(t *testing.T) {
	c := newTestCache(t, 30*time.Millisecond, WithLimit(2))

	assert.NoError(t, c.Set(bgCtx, "a", "1"))
	assert.NoError(t, c.Set(bgCtx, "b", "2"))
	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 0, c.Size(), "过期后容量受限缓存应清空（expireKey 走 keyLru.remove）")
}

// TestLruEvictThenReSet 回归测试：LRU 淘汰后旧定时器仍挂起，
// 重新写入同一 key 时，旧定时器不得误删新值（版本号必须单调递增）。
func TestLruEvictThenReSet(t *testing.T) {
	c := newTestCache(t, time.Minute, WithLimit(2))

	// a 被短过期定时器跟踪
	assert.NoError(t, c.SetEx(bgCtx, "a", "old", 100*time.Millisecond))
	assert.NoError(t, c.Set(bgCtx, "b", "x"))
	assert.NoError(t, c.Set(bgCtx, "c", "y")) // LRU 淘汰 a（a 最久未使用）

	_, err := c.Get(bgCtx, "a")
	assert.ErrorIs(t, err, ErrNotFound, "a 应已被 LRU 淘汰")

	// 在旧定时器触发前重新写入 a
	assert.NoError(t, c.SetEx(bgCtx, "a", "new", time.Minute))

	time.Sleep(150 * time.Millisecond) // 越过旧定时器触发点
	v, err := c.Get(bgCtx, "a")
	assert.NoError(t, err, "旧定时器不应误删重新写入的值")
	assert.Equal(t, "new", v)
}

func TestSetExFallback(t *testing.T) {
	// 默认 1 分钟，显式传 0/负数应回退到默认，不会立即过期
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.SetEx(bgCtx, "a", "v1", 0))
	assert.NoError(t, c.SetEx(bgCtx, "b", "v2", -time.Second))

	time.Sleep(20 * time.Millisecond)
	_, err := c.Get(bgCtx, "a")
	assert.NoError(t, err)
	_, err = c.Get(bgCtx, "b")
	assert.NoError(t, err)
}

func TestTakeAfterExpire(t *testing.T) {
	c := newTestCache(t, 30*time.Millisecond)

	var calls int32
	fetch := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		return "v", nil
	}

	v, err := c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)

	time.Sleep(80 * time.Millisecond) // 过期
	v, err = c.Take(bgCtx, "k", fetch)
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "过期后应重新拉取")
}

func TestTakeDistinctKeys(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var callsA, callsB int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = c.Take(bgCtx, "a", func() (any, error) { atomic.AddInt32(&callsA, 1); return "a", nil })
		}()
		go func() {
			defer wg.Done()
			_, _ = c.Take(bgCtx, "b", func() (any, error) { atomic.AddInt32(&callsB, 1); return "b", nil })
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&callsA), "不同 key 的 SingleFlight 应相互隔离")
	assert.Equal(t, int32(1), atomic.LoadInt32(&callsB))
}

func TestConcurrentMixedOps(t *testing.T) {
	c := newTestCache(t, time.Minute)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("k%d", (seed+i)%20)
				switch (seed + i) % 3 {
				case 0:
					_ = c.Set(bgCtx, key, i)
				case 1:
					_, _ = c.Get(bgCtx, key)
				default:
					_ = c.Delete(bgCtx, key)
				}
			}
		}(g)
	}
	wg.Wait()

	// 主要目的是配合 -race 检测数据竞争；不 panic 即通过
	assert.True(t, c.Size() >= 0)
}

func TestNewUnstableClamp(t *testing.T) {
	u := newUnstable(-0.5)
	assert.Equal(t, 0.0, u.deviation, "负偏差应被钳制为 0")

	u = newUnstable(2.0)
	assert.Equal(t, 1.0, u.deviation, "超上限偏差应被钳制为 1")
}

func TestCacheStatLoop(t *testing.T) {
	stop := make(chan struct{})
	st := &cacheStat{
		name:         "test",
		sizeCallback: func() int { return 3 },
		interval:     10 * time.Millisecond,
		stop:         stop,
		ctx:          bgCtx,
	}
	go st.statLoop()
	defer close(stop)

	// 第一个周期无命中：走 total==0 continue 分支
	time.Sleep(25 * time.Millisecond)

	// 制造命中与未命中：走统计输出分支
	st.IncrementHit()
	st.IncrementMiss()
	st.IncrementHit()
	time.Sleep(25 * time.Millisecond)

	// 统计被 swap 清零后无新计数，最终归零
	time.Sleep(25 * time.Millisecond)
	assert.Zero(t, atomic.LoadUint64(&st.hit))
	assert.Zero(t, atomic.LoadUint64(&st.miss))
}

func TestIncrement(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// key 不存在：初始化为 delta
	assert.NoError(t, c.Increment(bgCtx, "count", 5))
	v, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(5), v)

	// 已存在：累加
	assert.NoError(t, c.Increment(bgCtx, "count", 3))
	v, err = c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(8), v)
}

func TestDecrement(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// key 不存在：初始化为 -delta
	assert.NoError(t, c.Decrement(bgCtx, "count", 2))
	v, err := c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(-2), v)

	// 已存在：累减
	assert.NoError(t, c.Decrement(bgCtx, "count", 3))
	v, err = c.Get(bgCtx, "count")
	assert.NoError(t, err)
	assert.Equal(t, int64(-5), v)
}

func TestIncrementKeepsType(t *testing.T) {
	c := newTestCache(t, time.Minute)

	// Set 存的 int 类型，Increment 后仍为 int
	assert.NoError(t, c.Set(bgCtx, "n", 10))
	assert.NoError(t, c.Increment(bgCtx, "n", 1))
	v, err := c.Get(bgCtx, "n")
	assert.NoError(t, err)
	assert.Equal(t, 11, v)

	// 非数值类型：返回错误，不改变原值
	assert.NoError(t, c.Set(bgCtx, "s", "hello"))
	assert.Error(t, c.Increment(bgCtx, "s", 1))
	s, err := c.Get(bgCtx, "s")
	assert.NoError(t, err)
	assert.Equal(t, "hello", s)
}

func TestExpireSetTTL(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))

	// 设置短过期，到期前可读
	assert.NoError(t, c.Expire(bgCtx, "k", 30*time.Millisecond))
	_, err := c.Get(bgCtx, "k")
	assert.NoError(t, err)

	// 等过期（考虑 5% 抖动，多等一些时间）
	time.Sleep(80 * time.Millisecond)
	_, err = c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)

	// key 不存在返回 ErrNotFound
	err = c.Expire(bgCtx, "missing", time.Minute)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestExpireImmediate(t *testing.T) {
	c := newTestCache(t, time.Minute)
	assert.NoError(t, c.Set(bgCtx, "k", "v"))

	// ttl <= 0 立即失效
	assert.NoError(t, c.Expire(bgCtx, "k", 0))
	_, err := c.Get(bgCtx, "k")
	assert.ErrorIs(t, err, ErrNotFound)
}
