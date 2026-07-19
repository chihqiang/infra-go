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
// 返回消息通道和取消函数，channel 在订阅就绪后关闭。
func (p *redisPubSub) Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error) {
	pubsub := p.client.Subscribe(ctx, channel)

	// 等待订阅确认
	_, err := pubsub.Receive(ctx)
	if err != nil {
		_ = pubsub.Close()
		return nil, nil, fmt.Errorf("websocket: subscribe failed: %w", err)
	}

	out := make(chan []byte, 100)
	done := make(chan struct{})

	go func() {
		defer close(out)
		// 使用 Receive 直接轮询，避免 Channel() 内部 goroutine 在某些场景下不兼容
		for {
			msg, err := pubsub.Receive(ctx)
			if err != nil {
				_ = pubsub.Close()
				return
			}
			if m, ok := msg.(*redis.Message); ok {
				select {
				case out <- []byte(m.Payload):
				case <-done:
					_ = pubsub.Close()
					return
				}
			}
		}
	}()

	cancel := func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}

	return out, cancel, nil
}

// --- 测试用 MockPubSub ---

// mockPubSub 内存实现的 PubSub，用于集群测试。
type mockPubSub struct {
	mu       sync.Mutex
	subs     map[string][]chan []byte
}

func newMockPubSub() *mockPubSub {
	return &mockPubSub{
		subs: make(map[string][]chan []byte),
	}
}

func (m *mockPubSub) Publish(_ context.Context, channel string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.subs[channel] {
		select {
		case ch <- message:
		default:
		}
	}
	return nil
}

func (m *mockPubSub) Subscribe(_ context.Context, channel string) (<-chan []byte, func(), error) {
	m.mu.Lock()
	ch := make(chan []byte, 100)
	m.subs[channel] = append(m.subs[channel], ch)
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// 用 nil 标记已取消，避免发送到已关闭的 channel
		for i, sub := range m.subs[channel] {
			if sub == ch {
				m.subs[channel][i] = nil
				break
			}
		}
		close(ch)
	}

	return ch, cancel, nil
}
