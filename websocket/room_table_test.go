package websocket

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- MemoryRoom 测试 ---

func TestMemoryRoom_AddAndGet(t *testing.T) {
	room := NewMemoryRoom()

	// 添加连接到房间
	room.Add(1, "room1", "room2")
	room.Add(2, "room1")
	room.Add(3, "room2")

	// 验证房间内的连接
	clients1 := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{1, 2}, clients1)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1, 3}, clients2)

	// 验证连接所在的房间
	rooms := room.GetRooms(1)
	assert.ElementsMatch(t, []string{"room1", "room2"}, rooms)

	rooms2 := room.GetRooms(2)
	assert.ElementsMatch(t, []string{"room1"}, rooms2)
}

func TestMemoryRoom_Delete(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	// 从 room1 移除连接 1
	room.Delete(1, "room1")

	clients := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{2}, clients)

	// 连接 1 仍在 room2
	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	// 移除连接 1 的所有房间
	room.Delete(1)
	rooms := room.GetRooms(1)
	assert.Empty(t, rooms)
}

func TestMemoryRoom_Clear(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1")
	room.Add(2, "room2")

	room.Clear()

	assert.Empty(t, room.GetClients("room1"))
	assert.Empty(t, room.GetClients("room2"))
	assert.Empty(t, room.GetRooms(1))
}

func TestMemoryRoom_DuplicateAdd(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1")
	room.Add(1, "room1") // 重复添加

	clients := room.GetClients("room1")
	assert.Len(t, clients, 1)
}
