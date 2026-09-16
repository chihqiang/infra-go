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

// This file verifies that the ZSET members of the Redis sliding window are unique
// **across processes**.
//
// Background: the sliding window counts the requests in the window with ZCARD, while ZADD
// only updates the score of an existing member and does not increase the cardinality. The
// members of the old implementation were `<millisecond>:<in-process atomic counter>`, with no
// node identifier at all, so when two processes each produced counter=1 in the same
// millisecond the members were identical -> ZADD overwrote them -> ZCARD underestimated the
// real number of requests -> more requests were let through than limit allows.

// TestSlidingWindow_MemberPrefixIsProcessUnique verifies that the member prefix already
// carries a process-unique identifier.
func TestSlidingWindow_MemberPrefixIsProcessUnique(t *testing.T) {
	require.NotEmpty(t, memberPrefix, "memberPrefix must be initialised")

	// The prefix must be long enough to keep the collision probability low
	// (crypto/rand 64 bit -> 16 hex characters)
	assert.GreaterOrEqual(t, len(memberPrefix), 16)
}

// TestSlidingWindow_RealMemberCarriesProcessPrefix is a regression test: the members that a
// real call writes into the ZSET must carry the process-unique prefix.
//
// This is the only deterministic regression assertion in this group: the members written by
// the old implementation looked like `<millisecond>:<counter>` (two segments), while this case
// requires the first segment to equal memberPrefix, so it necessarily fails on the old code.
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

	// The suffix is still <ms>:<in-process counter>
	assert.Regexp(t, `^\d+:\d+$`, rest, "suffix must remain <ms>:<counter>, got %q", rest)
}

// TestSlidingWindow_SimulatedTwoProcessesBothCounted simulates two processes submitting
// requests from the same starting point: it replaces memberPrefix and resets the in-process
// counter to recreate the situation where each process starts again from counter=1.
//
// With the new implementation the two members necessarily differ because their prefixes
// differ, so both requests are counted; with the old implementation (members were only
// `<millisecond>:<counter>`) they overwrote each other when both requests landed in the same
// millisecond.
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

	// Process A
	memberPrefix = "process-aaaaaaaa"
	atomic.StoreUint64(&memberCounter, 0)
	require.True(t, sw.Allow())

	// Process B: different prefix, and the counter restarts at 1 (simulating another process)
	memberPrefix = "process-bbbbbbbb"
	atomic.StoreUint64(&memberCounter, 0)
	require.True(t, sw.Allow())

	members, err := client.ZRange(ctx, key, 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, members, 2, "both requests must occupy distinct members: %v", members)
	assert.NotEqual(t, members[0], members[1])

	// The prefixes of the two members correspond to the two "processes"
	assert.True(t, strings.HasPrefix(members[0], "process-aaaaaaaa:"), members[0])
	assert.True(t, strings.HasPrefix(members[1], "process-bbbbbbbb:"), members[1])
}

// TestSlidingWindow_MemberFormatDistinguishesProcesses illustrates what the prefix does: with
// the same millisecond and the same counter the members differ when the process prefixes
// differ, whereas the old prefix-less format collides.
func TestSlidingWindow_MemberFormatDistinguishesProcesses(t *testing.T) {
	const (
		now     = int64(1700000000000)
		counter = uint64(1)
	)

	nowStr := strconv.FormatInt(now, 10)
	counterStr := strconv.FormatUint(counter, 10)

	// New format: different processes -> different members
	memberA := memberPrefix + ":" + nowStr + ":" + counterStr
	memberB := "0123456789abcdef:" + nowStr + ":" + counterStr
	assert.NotEqual(t, memberA, memberB,
		"members from different processes must differ even with the same millisecond and counter")

	// Old format: same millisecond and same counter -> completely identical (which is exactly
	// why the limit could be bypassed)
	oldA := nowStr + ":" + counterStr
	oldB := nowStr + ":" + counterStr
	assert.Equal(t, oldA, oldB,
		"the old prefix-less format collides across processes, undercounting ZCARD")
}

// TestSlidingWindow_CountsDistinctProcessMembers uses the real Lua script to verify that two
// "processes" requesting concurrently in the same millisecond are both counted in the window
// and the limit is enforced correctly.
func TestSlidingWindow_CountsDistinctProcessMembers(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:cross-proc"
	const limit = 2
	now := time.Now().UnixMilli()
	window := int64(time.Minute)

	// Same millisecond and same counter, but from different processes (different prefixes)
	memberA := "aaaaaaaaaaaaaaaa:" + strconv.FormatInt(now, 10) + ":1"
	memberB := "bbbbbbbbbbbbbbbb:" + strconv.FormatInt(now, 10) + ":1"

	for i, member := range []string{memberA, memberB} {
		got, err := slidingWindowScript.Run(ctx, client, []string{key},
			now, window, limit, member).Int()
		require.NoError(t, err)
		assert.Equal(t, 1, got, "request %d must be allowed", i+1)
	}

	// Both distinct members must be counted
	card, err := client.ZCard(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), card,
		"ZADD must insert distinct members, not overwrite: got ZCARD=%d for 2 requests", card)

	// Once limit is reached, the 3rd request is rejected (the limit really takes effect)
	memberC := "cccccccccccccccc:" + strconv.FormatInt(now, 10) + ":1"
	got, err := slidingWindowScript.Run(ctx, client, []string{key},
		now, window, limit, memberC).Int()
	require.NoError(t, err)
	assert.Equal(t, 0, got, "third request must be rejected once limit is reached")
}

// TestSlidingWindow_RealWindowsShareKey uses two limiter instances (simulating two processes
// sharing Redis) to verify that limit is enforced: they use the same process prefix but
// different counters, so their members stay unique.
func TestSlidingWindow_RealWindowsShareKey(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	sw1 := NewRedisSlidingWindow(client, "shared:sw:proc", 3, time.Minute)
	sw2 := NewRedisSlidingWindow(client, "shared:sw:proc", 3, time.Minute)

	assert.True(t, sw1.Allow())
	assert.True(t, sw1.Allow())
	assert.True(t, sw1.Allow())
	// The key is shared, so the 4th request (from the other instance) must be rejected
	assert.False(t, sw2.Allow(), "limit must be enforced across instances sharing the key")

	card, err := client.ZCard(context.Background(), "shared:sw:proc").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(3), card)
}

// TestSlidingWindow_ConcurrentMembersAllCounted checks that every request is counted under
// concurrency. Members carry the in-process atomic counter, so they never overwrite each
// other.
func TestSlidingWindow_ConcurrentMembersAllCounted(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	ctx := context.Background()
	const key = "rl:concurrent"
	const n = 50
	const limit = 1000 // large enough to let everything through; only the counting is verified

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
