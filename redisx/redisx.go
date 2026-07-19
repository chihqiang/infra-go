package redisx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// 错误定义。
var (
	// ErrLockNotAcquired 获取锁失败（锁已被其他客户端持有）。
	ErrLockNotAcquired = errors.New("redisx: lock not acquired")
	// ErrLockOwnershipMismatch 释放锁失败（锁不属于当前持有者）。
	ErrLockOwnershipMismatch = errors.New("redisx: lock ownership mismatch")
)

// Client 封装了 redis.Client，提供便捷的 Redis 操作。
type Client struct {
	client    *redis.Client
	keyPrefix string
}

// New 根据配置创建 Redis 客户端。
// 零值字段会自动填充默认值（通过 default 标签定义）。
func New(cfg Config) (*Client, error) {
	c := fillDefault(cfg)

	var client *redis.Client
	if c.MasterName != "" && len(c.SentinelAddrs) > 0 {
		// 哨兵模式
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       c.MasterName,
			SentinelAddrs:    c.SentinelAddrs,
			Username:         c.Username,
			Password:         c.Password,
			DB:               c.DB,
			PoolSize:         c.PoolSize,
			MinIdleConns:     c.MinIdleConns,
			MaxRetries:       c.MaxRetries,
			DialTimeout:      c.DialTimeout,
			ReadTimeout:      c.ReadTimeout,
			WriteTimeout:     c.WriteTimeout,
			PoolTimeout:      c.PoolTimeout,
			ConnMaxIdleTime:  c.ConnMaxIdleTime,
		})
	} else {
		// 单机模式
		client = redis.NewClient(&redis.Options{
			Addr:            c.Addr,
			Username:        c.Username,
			Password:        c.Password,
			DB:               c.DB,
			PoolSize:         c.PoolSize,
			MinIdleConns:     c.MinIdleConns,
			MaxRetries:       c.MaxRetries,
			DialTimeout:      c.DialTimeout,
			ReadTimeout:      c.ReadTimeout,
			WriteTimeout:     c.WriteTimeout,
			PoolTimeout:      c.PoolTimeout,
			ConnMaxIdleTime:  c.ConnMaxIdleTime,
		})
	}

	return &Client{
		client:    client,
		keyPrefix: c.KeyPrefix,
	}, nil
}

// MustNew 根据配置创建 Redis 客户端，出错时 panic。
func MustNew(cfg Config) *Client {
	c, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("redisx: failed to create client: %w", err))
	}
	return c
}

// Client 返回底层的 redis.Client，用于需要直接操作的场景。
func (c *Client) Client() *redis.Client {
	return c.client
}

// Ping 测试 Redis 连接是否正常。
func (c *Client) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redisx: ping failed: %w", err)
	}
	return nil
}

// Close 关闭 Redis 连接。
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("redisx: failed to close client: %w", err)
	}
	return nil
}

// wrapKey 为键添加前缀。
func (c *Client) wrapKey(key string) string {
	if c.keyPrefix == "" {
		return key
	}
	return c.keyPrefix + ":" + key
}

// wrapKeys 为多个键添加前缀。
func (c *Client) wrapKeys(keys ...string) []string {
	if c.keyPrefix == "" {
		return keys
	}
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = c.wrapKey(key)
	}
	return result
}

// --- 基础操作 ---

// Get 获取字符串值。
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// Set 设置字符串值，带过期时间。
func (c *Client) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	if err := c.client.Set(ctx, c.wrapKey(key), value, expiration).Err(); err != nil {
		return wrapErr(err)
	}
	return nil
}

// Del 删除一个或多个键。
func (c *Client) Del(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := c.client.Del(ctx, c.wrapKeys(keys...)...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// Exists 检查键是否存在，返回存在的键数量。
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	n, err := c.client.Exists(ctx, c.wrapKeys(keys...)...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// Expire 为键设置过期时间。
func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	ok, err := c.client.Expire(ctx, c.wrapKey(key), expiration).Result()
	if err != nil {
		return false, wrapErr(err)
	}
	return ok, nil
}

// TTL 获取键的剩余过期时间。
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	d, err := c.client.TTL(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return d, nil
}

// Incr 将键的值递增 1。
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	n, err := c.client.Incr(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// IncrBy 将键的值递增指定数量。
func (c *Client) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	n, err := c.client.IncrBy(ctx, c.wrapKey(key), value).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- Hash 操作 ---

// HGet 获取哈希表中指定字段的值。
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	val, err := c.client.HGet(ctx, c.wrapKey(key), field).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// HSet 设置哈希表字段的值。
func (c *Client) HSet(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.HSet(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// HGetAll 获取哈希表中所有字段和值。
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	m, err := c.client.HGetAll(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return m, nil
}

// HDel 删除哈希表中的一个或多个字段。
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	n, err := c.client.HDel(ctx, c.wrapKey(key), fields...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- List 操作 ---

// LPush 将值插入列表头部。
func (c *Client) LPush(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.LPush(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// RPush 将值插入列表尾部。
func (c *Client) RPush(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.RPush(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// LPop 移除并返回列表头部元素。
func (c *Client) LPop(ctx context.Context, key string) (string, error) {
	val, err := c.client.LPop(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// RPop 移除并返回列表尾部元素。
func (c *Client) RPop(ctx context.Context, key string) (string, error) {
	val, err := c.client.RPop(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// LRange 获取列表指定范围内的元素。
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	vals, err := c.client.LRange(ctx, c.wrapKey(key), start, stop).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return vals, nil
}

// --- Set 操作 ---

// SAdd 向集合添加一个或多个成员。
func (c *Client) SAdd(ctx context.Context, key string, members ...any) (int64, error) {
	n, err := c.client.SAdd(ctx, c.wrapKey(key), members...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// SMembers 获取集合所有成员。
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	members, err := c.client.SMembers(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return members, nil
}

// SIsMember 判断成员是否在集合中。
func (c *Client) SIsMember(ctx context.Context, key string, member any) (bool, error) {
	ok, err := c.client.SIsMember(ctx, c.wrapKey(key), member).Result()
	if err != nil {
		return false, wrapErr(err)
	}
	return ok, nil
}

// SRem 从集合移除一个或多个成员。
func (c *Client) SRem(ctx context.Context, key string, members ...any) (int64, error) {
	n, err := c.client.SRem(ctx, c.wrapKey(key), members...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- Scan 操作 ---

// Scan 迭代键，返回匹配 pattern 的键。
// 使用游标迭代，每次返回一批键和下一次的游标。
func (c *Client) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	var keys []string
	var newCursor uint64
	var err error

	if c.keyPrefix == "" {
		keys, newCursor, err = c.client.Scan(ctx, cursor, match, count).Result()
	} else {
		// 有前缀时，自动加上前缀匹配
		wrappedMatch := c.keyPrefix + ":" + match
		if match == "" {
			wrappedMatch = c.keyPrefix + ":*"
		}
		rawKeys, c2, e := c.client.Scan(ctx, cursor, wrappedMatch, count).Result()
		err = e
		newCursor = c2
		// 移除前缀
		prefix := c.keyPrefix + ":"
		for _, k := range rawKeys {
			keys = append(keys, strings.TrimPrefix(k, prefix))
		}
	}
	if err != nil {
		return nil, 0, wrapErr(err)
	}
	return keys, newCursor, nil
}

// --- 辅助函数 ---

// wrapErr 将 redis 错误转换为更友好的错误信息。
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, redis.Nil) {
		return ErrNil
	}
	return err
}

// ErrNil 表示键不存在。
var ErrNil = errors.New("redisx: key not found")
