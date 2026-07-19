package redisx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// --- 默认常量 ---

const (
	// defaultRenewTimeout 续期操作的 Redis 调用超时时间。
	defaultRenewTimeout = 5 * time.Second
	// defaultTokenLen 锁 token 的随机字节数。
	defaultTokenLen = 16
)

// Lock 分布式锁。
// 基于 Redis 的 SET NX EX 实现，支持自动续期和安全释放。
type Lock struct {
	client    *Client
	key       string
	token     string
	ttl       time.Duration
	done      chan struct{}
	doneOnce  sync.Once // 保护 done 只 close 一次
	autoRenew bool
}

// LockOption 锁选项。
type LockOption func(*lockConfig)

// lockConfig 锁配置。
type lockConfig struct {
	ttl       time.Duration
	autoRenew bool
}

// WithTTL 设置锁的过期时间。
func WithTTL(ttl time.Duration) LockOption {
	return func(c *lockConfig) {
		c.ttl = ttl
	}
}

// WithAutoRenew 启用自动续期。
// 启用后，锁会在后台自动续期，防止业务逻辑执行时间超过锁过期时间。
// 调用 Unlock 时会自动停止续期。
func WithAutoRenew() LockOption {
	return func(c *lockConfig) {
		c.autoRenew = true
	}
}

// LockAcquirer 锁获取器。
type LockAcquirer struct {
	client *Client
	key    string
	ttl    time.Duration
	opts   []LockOption
}

// Locker 创建一个锁获取器。
// key 为锁的名称，ttl 为锁的默认过期时间。
func (c *Client) Locker(key string, ttl time.Duration, opts ...LockOption) *LockAcquirer {
	return &LockAcquirer{
		client: c,
		key:    key,
		ttl:    ttl,
		opts:   opts,
	}
}

// TryLock 尝试获取锁，如果锁已被持有则立即返回错误。
func (la *LockAcquirer) TryLock(ctx context.Context) (*Lock, error) {
	// 应用选项
	lc := &lockConfig{ttl: la.ttl}
	for _, opt := range la.opts {
		opt(lc)
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("redisx: failed to generate lock token: %w", err)
	}

	wrappedKey := la.client.wrapKey(la.key)

	// 使用 SET NX EX 原子操作获取锁
	ok, err := la.client.client.SetNX(ctx, wrappedKey, token, lc.ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("redisx: failed to acquire lock: %w", err)
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}

	lock := &Lock{
		client:    la.client,
		key:       la.key,
		token:     token,
		ttl:       lc.ttl,
		done:      make(chan struct{}),
		autoRenew: lc.autoRenew,
	}

	// 启动自动续期
	if lock.autoRenew {
		go lock.renewLoop()
	}

	return lock, nil
}

// Lock 阻塞式获取锁，会不断重试直到成功或 context 取消。
// retryInterval 为重试间隔。
func (la *LockAcquirer) Lock(ctx context.Context, retryInterval time.Duration) (*Lock, error) {
	for {
		lock, err := la.TryLock(ctx)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLockNotAcquired) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryInterval):
			// 继续重试
		}
	}
}

// Unlock 释放锁。
// 使用 Lua 脚本确保只有锁的持有者才能释放锁。
func (l *Lock) Unlock(ctx context.Context) error {
	// 停止自动续期（sync.Once 保护，避免重复 close panic）
	l.doneOnce.Do(func() {
		if l.autoRenew {
			close(l.done)
		}
	})

	// Lua 脚本：只有 token 匹配时才删除
	const unlockScript = `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	wrappedKey := l.client.wrapKey(l.key)
	result, err := l.client.client.Eval(ctx, unlockScript, []string{wrappedKey}, l.token).Result()
	if err != nil {
		return fmt.Errorf("redisx: failed to release lock: %w", err)
	}

	n, ok := result.(int64)
	if !ok || n == 0 {
		return ErrLockOwnershipMismatch
	}

	return nil
}

// renewLoop 后台自动续期循环。
func (l *Lock) renewLoop() {
	ticker := time.NewTicker(l.ttl / 3)
	defer ticker.Stop()

	const renewScript = `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	wrappedKey := l.client.wrapKey(l.key)
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), defaultRenewTimeout)
			_, err := l.client.client.Eval(ctx, renewScript, []string{wrappedKey}, l.token, l.ttl.Milliseconds()).Result()
			cancel()
			if err != nil {
				// 续期失败，可能是 Redis 故障或锁已丢失
				return
			}
		}
	}
}

// generateToken 生成随机 token，用于标识锁的持有者。
func generateToken() (string, error) {
	b := make([]byte, defaultTokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// --- 便捷方法 ---

// SetNXWithLock 使用分布式锁执行函数。
// 获取锁后执行 fn，执行完成后自动释放锁。
// 如果获取锁失败，返回 ErrLockNotAcquired。
func (c *Client) SetNXWithLock(ctx context.Context, key string, ttl time.Duration, fn func(ctx context.Context) error) error {
	lock, err := c.Locker(key, ttl).TryLock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// 用独立的 context 释放锁，避免业务 ctx 取消后锁无法释放
		_ = lock.Unlock(context.Background())
	}()

	return fn(ctx)
}

// IsLockNotAcquired 判断错误是否为获取锁失败。
func IsLockNotAcquired(err error) bool {
	return errors.Is(err, ErrLockNotAcquired)
}
