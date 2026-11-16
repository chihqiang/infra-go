package ws

import (
	"net/http"

	"github.com/chihqiang/infra-go/core/logger"
)

// Option is a server configuration option.
type Option func(*options)

// options holds the internal server options.
type options struct {
	logger       logger.ILogger
	redisClient  RedisClient
	pubsub       PubSub
	checkOrigin  func(r *http.Request) bool
	subprotocols []string
}

// WithLogger sets the logger; logger.GetGlobal() is used by default.
func WithLogger(l logger.ILogger) Option {
	return func(o *options) { o.logger = l }
}

// WithRedisClient sets the Redis client (used by Redis rooms).
// When it is not set and RoomType is "redis", one is created automatically from the
// Redis settings in Config.
func WithRedisClient(client RedisClient) Option {
	return func(o *options) { o.redisClient = client }
}

// WithCheckOrigin sets the Origin check function. The default is the same-origin
// check of gorilla/websocket; pass a custom function to allow other origins (for
// example a whitelist for a separate front-end domain).
func WithCheckOrigin(fn func(r *http.Request) bool) Option {
	return func(o *options) { o.checkOrigin = fn }
}

// WithSubprotocols sets the list of subprotocols to negotiate.
func WithSubprotocols(protocols ...string) Option {
	return func(o *options) { o.subprotocols = protocols }
}

// WithPubSub sets a custom PubSub implementation (used for cluster broadcast).
// In Redis room mode a go-redis based implementation is created automatically by default.
// Tests can inject a mock implementation through this option.
func WithPubSub(ps PubSub) Option {
	return func(o *options) { o.pubsub = ps }
}
