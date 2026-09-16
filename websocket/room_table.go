package websocket

import "sync"

// MemoryRoom is the in-memory room implementation.
// Two maps keep the bidirectional mapping between room → fds and fd → rooms.
// It fits standalone deployments and is made concurrency-safe by sync.RWMutex.
//
// sync.RWMutex + maps keep the bidirectional mapping and guarantee concurrency safety.
type MemoryRoom struct {
	mu    sync.RWMutex
	rooms map[string]map[ConnID]struct{} // room -> set of fds
	fds   map[ConnID]map[string]struct{} // fd -> set of rooms
}

// NewMemoryRoom creates an in-memory room.
func NewMemoryRoom() *MemoryRoom {
	return &MemoryRoom{
		rooms: make(map[string]map[ConnID]struct{}),
		fds:   make(map[ConnID]map[string]struct{}),
	}
}

// Add adds a connection to the given rooms.
func (r *MemoryRoom) Add(fd ConnID, rooms ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Make sure the fd exists in the fds map
	fdRooms, ok := r.fds[fd]
	if !ok {
		fdRooms = make(map[string]struct{})
		r.fds[fd] = fdRooms
	}

	for _, room := range rooms {
		// Add to the room → fds mapping
		roomFds, ok := r.rooms[room]
		if !ok {
			roomFds = make(map[ConnID]struct{})
			r.rooms[room] = roomFds
		}
		roomFds[fd] = struct{}{}

		// Add to the fd → rooms mapping
		fdRooms[room] = struct{}{}
	}
}

// Delete removes a connection from the given rooms.
// If rooms is empty, the connection is removed from all of its rooms.
func (r *MemoryRoom) Delete(fd ConnID, rooms ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fdRooms, ok := r.fds[fd]
	if !ok {
		return
	}

	// When rooms is not given, remove every room the connection is in
	if len(rooms) == 0 {
		for room := range fdRooms {
			if roomFds, ok := r.rooms[room]; ok {
				delete(roomFds, fd)
				if len(roomFds) == 0 {
					delete(r.rooms, room)
				}
			}
		}
		delete(r.fds, fd)
		return
	}

	for _, room := range rooms {
		delete(fdRooms, room)

		if roomFds, ok := r.rooms[room]; ok {
			delete(roomFds, fd)
			if len(roomFds) == 0 {
				delete(r.rooms, room)
			}
		}
	}

	if len(fdRooms) == 0 {
		delete(r.fds, fd)
	}
}

// GetClients returns all connection IDs in the room.
func (r *MemoryRoom) GetClients(room string) []ConnID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	roomFds, ok := r.rooms[room]
	if !ok {
		return nil
	}

	fds := make([]ConnID, 0, len(roomFds))
	for fd := range roomFds {
		fds = append(fds, fd)
	}
	return fds
}

// GetRooms returns the names of all rooms the connection is in.
func (r *MemoryRoom) GetRooms(fd ConnID) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fdRooms, ok := r.fds[fd]
	if !ok {
		return nil
	}

	rooms := make([]string, 0, len(fdRooms))
	for room := range fdRooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// Clear clears all rooms and connection mappings.
func (r *MemoryRoom) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rooms = make(map[string]map[ConnID]struct{})
	r.fds = make(map[ConnID]map[string]struct{})
}
