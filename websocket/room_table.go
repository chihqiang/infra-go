package websocket

import "sync"

// MemoryRoom 内存房间实现。
// 使用两个 map 维护 room → fds 和 fd → rooms 的双向映射。
// 适用于单机部署，通过 sync.RWMutex 保证并发安全。
//
// 使用 sync.RWMutex + map 维护双向映射，保证并发安全。
type MemoryRoom struct {
	mu    sync.RWMutex
	rooms map[string]map[ConnID]struct{} // room -> set of fds
	fds   map[ConnID]map[string]struct{} // fd -> set of rooms
}

// NewMemoryRoom 创建一个内存房间。
func NewMemoryRoom() *MemoryRoom {
	return &MemoryRoom{
		rooms: make(map[string]map[ConnID]struct{}),
		fds:   make(map[ConnID]map[string]struct{}),
	}
}

// Add 将连接加入房间。
func (r *MemoryRoom) Add(fd ConnID, rooms ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 确保 fd 在 fds 映射中存在
	fdRooms, ok := r.fds[fd]
	if !ok {
		fdRooms = make(map[string]struct{})
		r.fds[fd] = fdRooms
	}

	for _, room := range rooms {
		// 加入 room → fds 映射
		roomFds, ok := r.rooms[room]
		if !ok {
			roomFds = make(map[ConnID]struct{})
			r.rooms[room] = roomFds
		}
		roomFds[fd] = struct{}{}

		// 加入 fd → rooms 映射
		fdRooms[room] = struct{}{}
	}
}

// Delete 将连接从房间移除。
// 如果 rooms 为空，则移除该连接所在的所有房间。
func (r *MemoryRoom) Delete(fd ConnID, rooms ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	fdRooms, ok := r.fds[fd]
	if !ok {
		return
	}

	// 如果未指定 rooms，移除该连接所在的所有房间
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

// GetClients 获取房间内的所有连接 ID。
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

// GetRooms 获取连接所在的所有房间名称。
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

// Clear 清空所有房间和连接映射。
func (r *MemoryRoom) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rooms = make(map[string]map[ConnID]struct{})
	r.fds = make(map[ConnID]map[string]struct{})
}
