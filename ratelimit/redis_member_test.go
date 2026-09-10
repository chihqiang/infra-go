package ratelimit

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件验证 Redis 滑动窗口的 ZSET 成员在**跨进程**场景下唯一。
//
// 背景：滑动窗口用 ZCARD 统计窗口内请求数，而 ZADD 对已存在的成员只更新 score、
// 不增加基数。旧实现的成员是 `<毫秒>:<进程内原子计数>`，不含任何节点标识，
// 两个进程在同一毫秒各自产生 counter=1 时成员完全相同 → ZADD 覆盖 →
// ZCARD 低估真实请求数 → 实际放行量超过 limit。

// TestSlidingWindow_MemberPrefixIsProcessUnique 验证成员前缀已包含进程级唯一标识。
func TestSlidingWindow_MemberPrefixIsProcessUnique(t *testing.T) {
	require.NotEmpty(t, memberPrefix, "memberPrefix must be initialised")

	// 前缀应足够长以具备低碰撞概率（crypto/rand 64 bit → 16 hex 字符）
	assert.GreaterOrEqual(t, len(memberPrefix), 16)
}

// TestSlidingWindow_RealMemberCarriesProcessPrefix 回归测试：真实调用写入 ZSET 的
// 成员必须带上进程唯一前缀。
//
// 这是本组测试中唯一确定性的回归断言：旧实现写入的成员形如 `<毫秒>:<计数>`
// （两段），本用例要求首段等于 memberPrefix，因此在旧代码上必然失败。
func TestSlidingWindow_RealMemberCarriesProcessPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:member-format"

	sw := NewRedisSlidingWindow(client, key, 10, time.Minute)
	require.True(t, sw.Allow())

	members, err := client.ZRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, members, 1)

	prefix, rest, ok := strings.Cut(members[0], ":")
	require.True(t, ok, "member must be colon-separated, got %q", members[0])
	assert.Equal(t, memberPrefix, prefix,
		"member must start with the process-unique prefix, got member %q", members[0])

	// 后缀仍是 <毫秒>:<进程内计数>
	assert.Regexp(t, `^\d+:\d+$`, rest, "suffix must remain <ms>:<counter>, got %q", rest)
}

// TestSlidingWindow_SimulatedTwoProcessesBothCounted 模拟两个进程在同一起点提交请求：
// 通过替换 memberPrefix 并重置进程内计数来还原"两个进程各自从 counter=1 开始"的场景。
//
// 新实现下两个成员因前缀不同而必然不同，两个请求都被计入；
// 旧实现（成员仅 `<毫秒>:<计数>`）在两请求落在同一毫秒时会互相覆盖。
func TestSlidingWindow_SimulatedTwoProcessesBothCounted(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:two-procs"

	origPrefix := memberPrefix
	t.Cleanup(func() { memberPrefix = origPrefix })
	origCounter := atomic.LoadUint64(&memberCounter)
	t.Cleanup(func() { atomic.StoreUint64(&memberCounter, origCounter) })

	sw := NewRedisSlidingWindow(client, key, 10, time.Minute)

	// 进程 A
	memberPrefix = "process-aaaaaaaa"
	atomic.StoreUint64(&memberCounter, 0)
	require.True(t, sw.Allow())

	// 进程 B：前缀不同，且计数从 1 重新开始（模拟另一进程）
	memberPrefix = "process-bbbbbbbb"
	atomic.StoreUint64(&memberCounter, 0)
	require.True(t, sw.Allow())

	members, err := client.ZRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, members, 2, "both requests must occupy distinct members: %v", members)
	assert.NotEqual(t, members[0], members[1])

	// 两个成员的前缀分别对应两个"进程"
	assert.True(t, strings.HasPrefix(members[0], "process-aaaaaaaa:"), members[0])
	assert.True(t, strings.HasPrefix(members[1], "process-bbbbbbbb:"), members[1])
}

// TestSlidingWindow_MemberFormatDistinguishesProcesses 说明前缀的作用：
// 同一毫秒 + 同一计数的成员，在不同进程前缀下互不相同；
// 而旧格式（无前缀）会碰撞。
func TestSlidingWindow_MemberFormatDistinguishesProcesses(t *testing.T) {
	const (
		now     = int64(1700000000000)
		counter = uint64(1)
	)

	nowStr := strconv.FormatInt(now, 10)
	counterStr := strconv.FormatUint(counter, 10)

	// 新格式：不同进程 → 不同成员
	memberA := memberPrefix + ":" + nowStr + ":" + counterStr
	memberB := "0123456789abcdef:" + nowStr + ":" + counterStr
	assert.NotEqual(t, memberA, memberB,
		"members from different processes must differ even with the same millisecond and counter")

	// 旧格式：同毫秒同计数 → 完全相同（这正是限流可被突破的原因）
	oldA := nowStr + ":" + counterStr
	oldB := nowStr + ":" + counterStr
	assert.Equal(t, oldA, oldB,
		"the old prefix-less format collides across processes, undercounting ZCARD")
}

// TestSlidingWindow_CountsDistinctProcessMembers 用真实 Lua 脚本验证：
// 两个"进程"在同一毫秒并发请求时都被计入窗口，limit 被正确执行。
func TestSlidingWindow_CountsDistinctProcessMembers(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:cross-proc"
	const limit = 2
	now := time.Now().UnixMilli()
	window := int64(time.Minute)

	// 同一毫秒、相同 counter，但来自不同进程（前缀不同）
	memberA := "aaaaaaaaaaaaaaaa:" + strconv.FormatInt(now, 10) + ":1"
	memberB := "bbbbbbbbbbbbbbbb:" + strconv.FormatInt(now, 10) + ":1"

	for i, member := range []string{memberA, memberB} {
		got, err := slidingWindowScript.Run(ctx, client, []string{key},
			now, window, limit, member).Int()
		require.NoError(t, err)
		assert.Equal(t, 1, got, "request %d must be allowed", i+1)
	}

	// 两个不同成员都必须被计入
	card, err := client.ZCard(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), card,
		"ZADD must insert distinct members, not overwrite: got ZCARD=%d for 2 requests", card)

	// 达到 limit 后第 3 个请求被拒绝（limit 真正生效）
	memberC := "cccccccccccccccc:" + strconv.FormatInt(now, 10) + ":1"
	got, err := slidingWindowScript.Run(ctx, client, []string{key},
		now, window, limit, memberC).Int()
	require.NoError(t, err)
	assert.Equal(t, 0, got, "third request must be rejected once limit is reached")
}

// TestSlidingWindow_RealWindowsShareKey 用两个限流器实例（模拟两个进程共享 Redis）
// 验证 limit 被正确执行——它们使用同一个进程前缀，但计数不同，
// 因此成员仍唯一。
func TestSlidingWindow_RealWindowsShareKey(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	sw1 := NewRedisSlidingWindow(client, "shared:sw:proc", 3, time.Minute)
	sw2 := NewRedisSlidingWindow(client, "shared:sw:proc", 3, time.Minute)

	assert.True(t, sw1.Allow())
	assert.True(t, sw1.Allow())
	assert.True(t, sw1.Allow())
	// 共享 key，第 4 个请求（来自另一实例）必须被拒绝
	assert.False(t, sw2.Allow(), "limit must be enforced across instances sharing the key")

	card, err := client.ZCard(context.Background(), "shared:sw:proc").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(3), card)
}

// TestSlidingWindow_ConcurrentMembersAllCounted 并发场景下每个请求都必须被计入。
// 成员含进程内原子计数，因此不会互相覆盖。
func TestSlidingWindow_ConcurrentMembersAllCounted(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:concurrent"
	const n = 50
	const limit = 1000 // 足够大，保证全部放行，仅验证计数

	sw := NewRedisSlidingWindow(client, key, limit, time.Minute)

	var allowed int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sw.Allow() {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(n), atomic.LoadInt64(&allowed))
	card, err := client.ZCard(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(n), card,
		"every concurrent request must occupy a distinct member, got ZCARD=%d", card)
}
