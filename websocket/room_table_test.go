package websocket

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- MemoryRoom tests ---

func TestMemoryRoom_AddAndGet(t *testing.T) {
	room := NewMemoryRoom()

	// Add connections to rooms
	room.Add(1, "room1", "room2")
	room.Add(2, "room1")
	room.Add(3, "room2")

	// Verify the connections in the rooms
	clients1 := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{1, 2}, clients1)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1, 3}, clients2)

	// Verify the rooms the connection is in
	rooms := room.GetRooms(1)
	assert.ElementsMatch(t, []string{"room1", "room2"}, rooms)

	rooms2 := room.GetRooms(2)
	assert.ElementsMatch(t, []string{"room1"}, rooms2)
}

func TestMemoryRoom_Delete(t *testing.T) {
	room := NewMemoryRoom()

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	// Remove connection 1 from room1
	room.Delete(1, "room1")

	clients := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{2}, clients)

	// Connection 1 is still in room2
	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	// Remove connection 1 from all rooms
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
	room.Add(1, "room1") // duplicate add

	clients := room.GetClients("room1")
	assert.Len(t, clients, 1)
}
