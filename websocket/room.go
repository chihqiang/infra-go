package websocket

// ConnID 连接 ID 类型。
type ConnID = uint64

// Room 房间管理接口，维护 room → fds 和 fd → rooms 的双向映射。
// 提供内存和 Redis 两种实现：
//   - MemoryRoom：基于 sync.RWMutex + map，适用于单机部署。
//   - RedisRoom：基于 Redis SET，适用于多实例部署。
//
// 提供内存和 Redis 两种实现。
type Room interface {
	// Add 将连接加入房间。
	// 如果连接已在房间中，不会重复添加。
	Add(fd ConnID, rooms ...string)

	// Delete 将连接从房间移除。
	// 如果 rooms 为空，则移除该连接所在的所有房间。
	Delete(fd ConnID, rooms ...string)

	// GetClients 获取房间内的所有连接 ID。
	GetClients(room string) []ConnID

	// GetRooms 获取连接所在的所有房间名称。
	GetRooms(fd ConnID) []string

	// Clear 清空所有房间和连接映射。
	Clear()
}
