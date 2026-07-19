package websocket

import (
	"context"
	"encoding/json"
	"fmt"
)

// Broadcaster 广播器，将消息推送到指定房间的所有连接。
// 通过 Server.To() 创建，支持流式 API。
//
// 单机模式：直接遍历房间内的本实例连接并写入。
// 集群模式：通过 Redis Pub/Sub 将消息发送到所有实例，
// 各实例收到后分发到本地在目标房间内的连接。
//
// 用法：
//
//	// 推送文本消息到多个房间
//	srv.To("room1", "room2").PushText("hello")
//
//	// 推送事件到房间
//	srv.To("room1").Emit("chat", map[string]string{"msg": "hi"})
//
//	// 推送 JSON 数据
//	srv.To("room1").WriteJSON(myData)
type Broadcaster struct {
	server  *Server
	targets []string // 目标房间列表
}

// write 发送消息到所有目标房间。
// 集群模式下通过 Redis Pub/Sub 广播，单机模式直接本地投递。
func (b *Broadcaster) write(messageType int, data []byte) error {
	// 集群模式：通过 Pub/Sub 跨实例广播
	if b.server.cluster != nil {
		msg := clusterMessage{
			Type:        clusterMessageTypeRoom,
			MessageType: messageType,
			Data:        data,
			Rooms:       b.targets,
		}
		return b.server.cluster.Publish(context.Background(), msg)
	}

	// 单机模式：直接遍历本地连接
	fds := b.collectLocalFDs()
	var errs []string
	sent := 0

	for _, fd := range fds {
		conn, ok := b.server.GetConn(fd)
		if !ok {
			continue
		}
		if err := conn.WriteMessage(messageType, data); err != nil {
			errs = append(errs, fmt.Sprintf("conn %d: %v", fd, err))
			continue
		}
		sent++
	}

	if len(errs) > 0 {
		return fmt.Errorf("websocket: broadcast errors: %v (sent %d/%d)", errs, sent, len(fds))
	}
	return nil
}

// collectLocalFDs 收集所有目标房间中的唯一连接 ID（仅本实例）。
func (b *Broadcaster) collectLocalFDs() []ConnID {
	fdSet := make(map[ConnID]struct{})
	for _, room := range b.targets {
		for _, fd := range b.server.room.GetClients(room) {
			fdSet[fd] = struct{}{}
		}
	}

	fds := make([]ConnID, 0, len(fdSet))
	for fd := range fdSet {
		fds = append(fds, fd)
	}
	return fds
}

// Push 推送文本消息到所有目标房间的连接。
func (b *Broadcaster) Push(data []byte) error {
	return b.write(TextMessage, data)
}

// PushText 推送字符串文本消息到所有目标房间的连接。
func (b *Broadcaster) PushText(data string) error {
	return b.write(TextMessage, []byte(data))
}

// PushBinary 推送二进制消息到所有目标房间的连接。
func (b *Broadcaster) PushBinary(data []byte) error {
	return b.write(BinaryMessage, data)
}

// WriteJSON 推送 JSON 消息到所有目标房间的连接。
func (b *Broadcaster) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.write(TextMessage, data)
}

// Emit 推送事件消息到所有目标房间的连接。
// 消息格式为 {"type":..., "data":...}。
func (b *Broadcaster) Emit(event string, data any) error {
	e, err := NewEvent(event, data)
	if err != nil {
		return err
	}
	return b.WriteJSON(e)
}
