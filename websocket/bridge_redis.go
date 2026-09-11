package websocket

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// redisPubSub 桥接 go-redis 的 PubSub，实现 PubSub 接口。
type redisPubSub struct {
	client RedisClient
}

// NewRedisPubSub 创建基于 go-redis 的 PubSub 实现。
func NewRedisPubSub(client RedisClient) PubSub {
	return &redisPubSub{client: client}
}

// Publish 实现 PubSub 接口。
func (p *redisPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	return p.client.Publish(ctx, channel, message).Err()
}

// Subscribe 实现 PubSub 接口。
// 返回消息通道和取消函数；取消后消息通道被关闭，订阅连接被释放。
func (p *redisPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	// 使用可取消的子 context：用于释放订阅关联的资源。
	// 注意：仅靠 context 无法唤醒阻塞中的 Receive，cancel 中还会显式 Close（见下）。
	subCtx, subCancel := context.WithCancel(ctx)

	pubsub := p.client.Subscribe(subCtx, channel)

	// 等待订阅确认
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
		// 无论以何种方式退出，都释放订阅连接
		defer func() { _ = pubsub.Close() }()

		// 使用 Receive 直接轮询，避免 Channel() 内部 goroutine 在某些场景下不兼容
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

	// cancel 保证幂等：可能被 ClusterHandler.Stop 与调用方重复调用
	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			close(done)
			subCancel()
			// 必须显式关闭订阅来唤醒阻塞中的 Receive：
			// go-redis 只从 ctx.Deadline() 推导读超时，Receive 在无 deadline 时
			// 阻塞在 socket 读上，仅取消 context 无法中断它（实测确认）。
			// PubSub.Close 是幂等的，重复调用返回 pool.ErrClosed。
			_ = pubsub.Close()
		})
	}

	return out, cancel, nil
}
