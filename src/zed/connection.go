package zed

import (
	"sync"
	"time"

	"zedex/zed/pb"

	"github.com/gorilla/websocket"
)

// connection represents a single client websocket connection.
//
// Zed's collab protocol addresses peers by *connection id* (a PeerId), not by
// user id: the same user may have several connections open at once, and
// project/buffer messages are addressed to a specific connection. zedex
// therefore keeps a registry of connections and maps peer ids back to them.
type connection struct {
	mu sync.Mutex

	// id is the connection id, unique for the lifetime of the server.
	id uint32
	// userId is the (canonical) user id this connection belongs to.
	userId uint64
	// peerId is the id clients use to address this connection.
	peerId *pb.PeerId
	// socket is the underlying websocket.
	socket *websocket.Conn
}

// peerKey is the map key used to look up a connection by its peer id.
type peerKey struct {
	ownerId uint32
	id      uint32
}

func keyForPeerId(peerId *pb.PeerId) peerKey {
	if peerId == nil {
		return peerKey{}
	}
	return peerKey{ownerId: peerId.GetOwnerId(), id: peerId.GetId()}
}

func (c *connection) write(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socket.WriteMessage(msgType, data)
}

func (c *connection) writeCompressed(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socket.WriteMessage(websocket.BinaryMessage, data)
}

func (c *connection) writeControl(msgType int, data []byte, deadline time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socket.WriteMessage(msgType, data)
}
