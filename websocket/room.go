package websocket

// ConnID is the connection ID type.
type ConnID = uint64

// Room is the room management interface; it keeps the bidirectional mapping between
// room → fds and fd → rooms.
// Two implementations are provided:
//   - MemoryRoom: based on sync.RWMutex + map, for standalone deployments.
//   - RedisRoom: based on Redis SET, for multi-instance deployments.
type Room interface {
	// Add adds a connection to the given rooms.
	// A connection that is already in a room is not added twice.
	Add(fd ConnID, rooms ...string)

	// Delete removes a connection from the given rooms.
	// If rooms is empty, the connection is removed from all of its rooms.
	Delete(fd ConnID, rooms ...string)

	// GetClients returns all connection IDs in the room.
	GetClients(room string) []ConnID

	// GetRooms returns the names of all rooms the connection is in.
	GetRooms(fd ConnID) []string

	// Clear clears all rooms and connection mappings.
	Clear()
}
