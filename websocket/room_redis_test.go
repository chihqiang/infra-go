package websocket

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

// --- RedisRoom 测试 ---

func TestRedisRoom_AddAndGet(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	clients1 := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{1, 2}, clients1)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	rooms := room.GetRooms(1)
	assert.ElementsMatch(t, []string{"room1", "room2"}, rooms)
}

func TestRedisRoom_Delete(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1", "room2")
	room.Add(2, "room1")

	room.Delete(1, "room1")

	clients := room.GetClients("room1")
	assert.ElementsMatch(t, []ConnID{2}, clients)

	clients2 := room.GetClients("room2")
	assert.ElementsMatch(t, []ConnID{1}, clients2)

	room.Delete(1)
	assert.Empty(t, room.GetRooms(1))
}

func TestRedisRoom_Clear(t *testing.T) {
	_, client := newMiniRedis(t)
	defer client.Close()

	room := NewRedisRoom(client, "ws:room:")

	room.Add(1, "room1")
	room.Add(2, "room2")

	room.Clear()

	assert.Empty(t, room.GetClients("room1"))
	assert.Empty(t, room.GetClients("room2"))
}
