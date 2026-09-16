package redisx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Error definitions.
var (
	// ErrLockNotAcquired means the lock was not acquired (another client holds it).
	ErrLockNotAcquired = errors.New("redisx: lock not acquired")
	// ErrLockOwnershipMismatch means the release failed (the lock does not belong to the
	// current holder).
	ErrLockOwnershipMismatch = errors.New("redisx: lock ownership mismatch")
	// ErrInvalidLockTTL means the lock TTL is invalid (<= 0 or below minLockTTL).
	// With a TTL <= 0, SET NX writes a key that never expires (a crash of the holder
	// means a permanent deadlock) and, with automatic renewal enabled,
	// time.NewTicker(ttl/3) panics inside the goroutine and terminates the process; with
	// a TTL below 1ms the PEXPIRE argument of the renew script is truncated to 0 and
	// deletes the lock outright.
	ErrInvalidLockTTL = errors.New("redisx: invalid lock ttl")
)

// Client wraps redis.Client and provides convenient Redis operations.
type Client struct {
	client    *redis.Client
	keyPrefix string
}

// New creates a Redis client from the configuration.
// Zero-value fields are filled with their defaults automatically (defined through the
// default tag).
func New(cfg Config) (*Client, error) {
	c := fillDefault(cfg)

	var client *redis.Client
	if c.MasterName != "" && len(c.SentinelAddrs) > 0 {
		// Sentinel mode
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      c.MasterName,
			SentinelAddrs:   c.SentinelAddrs,
			Username:        c.Username,
			Password:        c.Password,
			DB:              c.DB,
			PoolSize:        c.PoolSize,
			MinIdleConns:    c.MinIdleConns,
			MaxRetries:      c.MaxRetries,
			DialTimeout:     c.DialTimeout,
			ReadTimeout:     c.ReadTimeout,
			WriteTimeout:    c.WriteTimeout,
			PoolTimeout:     c.PoolTimeout,
			ConnMaxIdleTime: c.ConnMaxIdleTime,
		})
	} else {
		// Standalone mode
		client = redis.NewClient(&redis.Options{
			Addr:            c.Addr,
			Username:        c.Username,
			Password:        c.Password,
			DB:              c.DB,
			PoolSize:        c.PoolSize,
			MinIdleConns:    c.MinIdleConns,
			MaxRetries:      c.MaxRetries,
			DialTimeout:     c.DialTimeout,
			ReadTimeout:     c.ReadTimeout,
			WriteTimeout:    c.WriteTimeout,
			PoolTimeout:     c.PoolTimeout,
			ConnMaxIdleTime: c.ConnMaxIdleTime,
		})
	}

	return &Client{
		client:    client,
		keyPrefix: c.KeyPrefix,
	}, nil
}

// MustNew creates a Redis client from the configuration and panics on error.
func MustNew(cfg Config) *Client {
	c, err := New(cfg)
	if err != nil {
		panic(fmt.Errorf("redisx: failed to create client: %w", err))
	}
	return c
}

// Client returns the underlying redis.Client for cases that need direct access.
func (c *Client) Client() *redis.Client {
	return c.client
}

// Ping tests whether the Redis connection is healthy.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redisx: ping failed: %w", err)
	}
	return nil
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("redisx: failed to close client: %w", err)
	}
	return nil
}

// wrapKey adds the key prefix.
func (c *Client) wrapKey(key string) string {
	if c.keyPrefix == "" {
		return key
	}
	return c.keyPrefix + ":" + key
}

// wrapKeys adds the key prefix to several keys.
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

// --- Basic operations ---

// Get returns the string value.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// Set stores a string value with an expiration.
func (c *Client) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	if err := c.client.Set(ctx, c.wrapKey(key), value, expiration).Err(); err != nil {
		return wrapErr(err)
	}
	return nil
}

// SetNX sets the value only when the key does not exist, with an expiration, and
// reports whether it was set.
// It is commonly used for distributed locks and for writing the placeholders that
// prevent cache penetration.
func (c *Client) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	ok, err := c.client.SetNX(ctx, c.wrapKey(key), value, expiration).Result()
	if err != nil {
		return false, wrapErr(err)
	}
	return ok, nil
}

// Del removes one or more keys.
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

// Exists checks whether keys exist and returns the number of existing keys.
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

// Expire sets the expiration of a key.
func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	ok, err := c.client.Expire(ctx, c.wrapKey(key), expiration).Result()
	if err != nil {
		return false, wrapErr(err)
	}
	return ok, nil
}

// TTL returns the remaining time to live of a key.
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	d, err := c.client.TTL(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return d, nil
}

// Incr increments the value of a key by 1.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	n, err := c.client.Incr(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// IncrBy increments the value of a key by the given amount.
func (c *Client) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	n, err := c.client.IncrBy(ctx, c.wrapKey(key), value).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- Hash operations ---

// HGet returns the value of the given field of a hash.
func (c *Client) HGet(ctx context.Context, key, field string) (string, error) {
	val, err := c.client.HGet(ctx, c.wrapKey(key), field).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// HSet sets the value of a hash field.
func (c *Client) HSet(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.HSet(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// HGetAll returns all fields and values of a hash.
func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	m, err := c.client.HGetAll(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return m, nil
}

// HDel removes one or more fields from a hash.
func (c *Client) HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	n, err := c.client.HDel(ctx, c.wrapKey(key), fields...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- List operations ---

// LPush inserts values at the head of a list.
func (c *Client) LPush(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.LPush(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// RPush inserts values at the tail of a list.
func (c *Client) RPush(ctx context.Context, key string, values ...any) (int64, error) {
	n, err := c.client.RPush(ctx, c.wrapKey(key), values...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// LPop removes and returns the head element of a list.
func (c *Client) LPop(ctx context.Context, key string) (string, error) {
	val, err := c.client.LPop(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// RPop removes and returns the tail element of a list.
func (c *Client) RPop(ctx context.Context, key string) (string, error) {
	val, err := c.client.RPop(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return "", wrapErr(err)
	}
	return val, nil
}

// LRange returns the elements of a list within the given range.
func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	vals, err := c.client.LRange(ctx, c.wrapKey(key), start, stop).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return vals, nil
}

// --- Set operations ---

// SAdd adds one or more members to a set.
func (c *Client) SAdd(ctx context.Context, key string, members ...any) (int64, error) {
	n, err := c.client.SAdd(ctx, c.wrapKey(key), members...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// SMembers returns all members of a set.
func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	members, err := c.client.SMembers(ctx, c.wrapKey(key)).Result()
	if err != nil {
		return nil, wrapErr(err)
	}
	return members, nil
}

// SIsMember reports whether member is in the set.
func (c *Client) SIsMember(ctx context.Context, key string, member any) (bool, error) {
	ok, err := c.client.SIsMember(ctx, c.wrapKey(key), member).Result()
	if err != nil {
		return false, wrapErr(err)
	}
	return ok, nil
}

// SRem removes one or more members from a set.
func (c *Client) SRem(ctx context.Context, key string, members ...any) (int64, error) {
	n, err := c.client.SRem(ctx, c.wrapKey(key), members...).Result()
	if err != nil {
		return 0, wrapErr(err)
	}
	return n, nil
}

// --- Scan operations ---

// Scan iterates over keys and returns the keys matching pattern.
// It iterates with a cursor, returning a batch of keys and the next cursor each time.
func (c *Client) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	var keys []string
	var newCursor uint64
	var err error

	if c.keyPrefix == "" {
		keys, newCursor, err = c.client.Scan(ctx, cursor, match, count).Result()
	} else {
		// With a prefix, add it to the match pattern automatically
		wrappedMatch := c.keyPrefix + ":" + match
		if match == "" {
			wrappedMatch = c.keyPrefix + ":*"
		}
		rawKeys, c2, e := c.client.Scan(ctx, cursor, wrappedMatch, count).Result()
		err = e
		newCursor = c2
		// Strip the prefix
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

// --- Helper functions ---

// wrapErr converts a redis error into a friendlier error message.
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, redis.Nil) {
		return ErrNil
	}
	return err
}

// ErrNil means the key does not exist.
var ErrNil = errors.New("redisx: key not found")
