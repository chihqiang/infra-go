package websocket

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// RedisClient is the Redis client interface; it is compatible with *redis.Client,
// *redis.ClusterClient and *redis.Ring.
type RedisClient = redis.UniversalClient

// RedisRoom is a Redis SET based room implementation.
// It keeps the mapping in both directions with two kinds of SET:
//   - {prefix}rooms:{room} → SET(fd1, fd2, ...)  connections in the room
//   - {prefix}fds:{fd}     → SET(room1, room2, ...) rooms the connection is in
//
// It fits multi-instance deployments, where processes share Redis to broadcast to rooms.
//
// Redis SETs keep the bidirectional mapping between room → fds and fd → rooms.
type RedisRoom struct {
	client RedisClient
	prefix string
}

// NewRedisRoom creates a Redis room.
// client is the Redis client and prefix is the key prefix.
func NewRedisRoom(client RedisClient, prefix string) *RedisRoom {
	return &RedisRoom{
		client: client,
		prefix: prefix,
	}
}

// Add adds a connection to the given rooms.
func (r *RedisRoom) Add(fd ConnID, rooms ...string) {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	pipe := r.client.Pipeline()
	// Add the fd to the SET of every room
	for _, room := range rooms {
		pipe.SAdd(ctx, r.roomKey(room), fdStr)
	}
	// Add the room names to the SET of the fd
	pipe.SAdd(ctx, r.fdKey(fdStr), rooms)
	_, _ = pipe.Exec(ctx)
}

// Delete removes a connection from the given rooms.
// If rooms is empty, the connection is removed from all of its rooms.
func (r *RedisRoom) Delete(fd ConnID, rooms ...string) {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	if len(rooms) == 0 {
		// Fetch all rooms the connection is in first
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

// GetClients returns all connection IDs in the room.
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

// GetRooms returns the names of all rooms the connection is in.
func (r *RedisRoom) GetRooms(fd ConnID) []string {
	ctx := context.Background()
	fdStr := strconv.FormatUint(fd, 10)

	rooms, err := r.client.SMembers(ctx, r.fdKey(fdStr)).Result()
	if err != nil {
		return nil
	}
	return rooms
}

// Clear clears all rooms and connection mappings.
// It only deletes the keys related to room and connection mappings (the rooms: and
// fds: prefixes) and never deletes unrelated business keys under the same prefix.
//
// ⚠️ These keys are **shared by all instances**: clearing them affects the other nodes
// in the cluster (their connections vanish from the rooms and later broadcasts silently
// stop working).
// This method is therefore meant for operations/test scenarios only; Server.Close does
// not call it, as the latter only removes the connections of this instance (see
// Server.Close).
func (r *RedisRoom) Clear() {
	ctx := context.Background()

	for _, pattern := range []string{r.prefix + "rooms:*", r.prefix + "fds:*"} {
		var cursor uint64
		for {
			keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
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
}

// roomKey builds the room key.
func (r *RedisRoom) roomKey(room string) string {
	return fmt.Sprintf("%srooms:%s", r.prefix, room)
}

// fdKey builds the connection key.
func (r *RedisRoom) fdKey(fd string) string {
	return fmt.Sprintf("%sfds:%s", r.prefix, fd)
}
