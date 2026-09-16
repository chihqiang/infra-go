package websocket

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// redisPubSub bridges the go-redis PubSub and implements the PubSub interface.
type redisPubSub struct {
	client RedisClient
}

// NewRedisPubSub creates a PubSub implementation backed by go-redis.
func NewRedisPubSub(client RedisClient) PubSub {
	return &redisPubSub{client: client}
}

// Publish implements the PubSub interface.
func (p *redisPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	return p.client.Publish(ctx, channel, message).Err()
}

// Subscribe implements the PubSub interface.
// It returns the message channel and a cancel function; after cancel the message
// channel is closed and the subscription connection is released.
func (p *redisPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	// Use a cancellable child context to release the resources tied to the subscription.
	// Note: the context alone cannot wake up a blocked Receive, so cancel also closes
	// the subscription explicitly (see below).
	subCtx, subCancel := context.WithCancel(ctx)

	pubsub := p.client.Subscribe(subCtx, channel)

	// Wait for the subscription confirmation
	_, err := pubsub.Receive(subCtx)
	if err != nil {
		subCancel()
		_ = pubsub.Close()
		return nil, nil, fmt.Errorf("websocket: subscribe failed: %w", err)
	}

	out := make(chan []byte, 100)
	done := make(chan struct{})

	go func() {
		defer close(out)
		// Release the subscription connection no matter how we exit
		defer func() { _ = pubsub.Close() }()

		// Poll with Receive directly to avoid the goroutine inside Channel() being
		// incompatible in some scenarios
		for {
			msg, err := pubsub.Receive(subCtx)
			if err != nil {
				return
			}
			if m, ok := msg.(*redis.Message); ok {
				select {
				case out <- []byte(m.Payload):
				case <-done:
					return
				}
			}
		}
	}()

	// cancel is idempotent: it may be called twice by ClusterHandler.Stop and the caller
	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			close(done)
			subCancel()
			// The subscription must be closed explicitly to wake up a blocked Receive:
			// go-redis only derives the read timeout from ctx.Deadline(), so without a
			// deadline Receive blocks on the socket read and cancelling the context alone
			// cannot interrupt it (verified experimentally).
			// PubSub.Close is idempotent and returns pool.ErrClosed when called twice.
			_ = pubsub.Close()
		})
	}

	return out, cancel, nil
}
