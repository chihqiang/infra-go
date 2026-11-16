// Package ws provides a WebSocket server built on gorilla/websocket:
// connection lifecycle management, event dispatch, rooms and cluster broadcast.
//
// Typical usage:
//
//	handler := ws.NewEventHandler()
//	handler.Handle("chat", func(conn *ws.Conn, data json.RawMessage) {
//	    _ = conn.Emit("message", string(data))
//	})
//
//	server := ws.MustNew(ws.Config{}, handler)
//	http.Handle("/ws", server)
//	_ = http.ListenAndServe(":8080", nil)
//
// Room backends are selected through Config.RoomType ("memory" or "redis"),
// and cluster broadcast is enabled automatically in Redis mode.
package ws
