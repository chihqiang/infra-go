package websocket

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// RedisClient Redis 客户端接口，兼容 *redis.Client、*redis.ClusterClient 和 *redis.Ring。
type RedisClient = redis.UniversalClient

// RedisRoom 基于 Redis SET 的房间实现。
// 使用两个方向的 SET 维护映射：
//   - {prefix}rooms:{room} → SET(fd1, fd2, ...)  房间内的连接集合
//   - {prefix}fds:{fd}     → SET(room1, room2, ...) 连接所在的房间集合
//
// 适用于多实例部署，不同进程通过共享 Redis 实现房间广播。
//
// 使用 Redis SET 维护 room → fds 和 fd → rooms 的双向映射。
type RedisRoom struct {
	client RedisClient
	prefix string
}

// NewRedisRoom 创建一个 Redis 房间。
// client 为 Redis 客户端，prefix 为键前缀。
func NewRedisRoom(client RedisClient, prefix string) *RedisRoom {
	return &RedisRoom{
		client: client,
		prefix: prefix,
	}
}

// Add 将连接加入房间。
func (r *RedisRoom) Add(fd ConnID, rooms ...string) {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	pipe := r.client.Pipeline()
	// 将 fd 加入每个房间的 SET
	for _, room := range rooms {
		pipe.SAdd(ctx, r.roomKey(room), fdStr)
	}
	// 将房间名加入 fd 的 SET
	pipe.SAdd(ctx, r.fdKey(fdStr), rooms)
	_, _ = pipe.Exec(ctx)
}

// Delete 将连接从房间移除。
// 如果 rooms 为空，则移除该连接所在的所有房间。
func (r *RedisRoom) Delete(fd ConnID, rooms ...string) {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	if len(rooms) == 0 {
		// 先获取该连接所在的所有房间
		allRooms, err := r.client.SMembers(ctx, r.fdKey(fdStr)).Result()
		if err != nil || len(allRooms) == 0 {
			return
		}
		rooms = allRooms
	}

	pipe := r.client.Pipeline()
	for _, room := range rooms {
		pipe.SRem(ctx, r.roomKey(room), fdStr)
	}
	pipe.SRem(ctx, r.fdKey(fdStr), rooms)
	_, _ = pipe.Exec(ctx)
}

// GetClients 获取房间内的所有连接 ID。
func (r *RedisRoom) GetClients(room string) []ConnID {
	ctx := context.Background()

	members, err := r.client.SMembers(ctx, r.roomKey(room)).Result()
	if err != nil {
		return nil
	}

	fds := make([]ConnID, 0, len(members))
	for _, m := range members {
		fd, err := strconv.ParseUint(m, 10, 64)
		if err != nil {
			continue
		}
		fds = append(fds, fd)
	}
	return fds
}

// GetRooms 获取连接所在的所有房间名称。
func (r *RedisRoom) GetRooms(fd ConnID) []string {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	rooms, err := r.client.SMembers(ctx, r.fdKey(fdStr)).Result()
	if err != nil {
		return nil
	}
	return rooms
}

// Clear 清空所有房间和连接映射。
// 通过 SCAN 命令删除所有匹配前缀的键。
func (r *RedisRoom) Clear() {
	ctx := context.Background()

	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, r.prefix+"*", 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			r.client.Del(ctx, keys...)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// roomKey 构建房间键。
func (r *RedisRoom) roomKey(room string) string {
	return fmt.Sprintf("%srooms:%s", r.prefix, room)
}

// fdKey 构建连接键。
func (r *RedisRoom) fdKey(fd string) string {
	return fmt.Sprintf("%sfds:%s", r.prefix, fd)
}
