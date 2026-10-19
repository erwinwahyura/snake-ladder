package room

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/erwinwahyura/snake-ladder/internal/stats"
)

// client is one WebSocket connection. Its seat and detached fields are
// owned by the room goroutine.
type client struct {
	conn      *websocket.Conn
	send      chan []byte
	seat      int
	detached  bool
	lastReact time.Time
}

const (
	sendBuffer   = 32
	writeTimeout = 5 * time.Second
	pingInterval = 25 * time.Second
	readLimit    = 1024
)

// Handler upgrades /ws?room=CODE&name=NAME&token=TOKEN&pid=PLAYERID.
// An empty room creates a new one. The token reclaims a seat after a
// reconnect; pid is the browser's stable ID for the leaderboard.
func (h *Hub) Handler(originPatterns []string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{OriginPatterns: originPatterns})
		if err != nil {
			return // Accept already wrote the HTTP error
		}
		conn.SetReadLimit(readLimit)
		ctx := req.Context()
		q := req.URL.Query()

		var r *Room
		if code := q.Get("room"); code == "" {
			r, err = h.Create()
			if err != nil {
				closeWithError(ctx, conn, "server_full")
				return
			}
			slog.Info("room created", "room", r.code)
		} else if r = h.Get(code); r == nil {
			closeWithError(ctx, conn, "room_not_found")
			return
		}

		c := &client{conn: conn, send: make(chan []byte, sendBuffer), seat: -1}
		go c.writeLoop(ctx)
		if !r.post(joinEv{c: c, name: q.Get("name"), token: q.Get("token"), pid: h.playerID(q.Get("pid"))}) {
			conn.CloseNow()
			return
		}
		c.readLoop(ctx, r)
		r.post(leaveEv{c: c})
	}
}

// playerID checks the client's signed ID. An unknown or forged ID just
// means the player's games aren't recorded; they can still play.
func (h *Hub) playerID(token string) string {
	if h.cfg.Verify != nil {
		id, _ := h.cfg.Verify(token)
		return id
	}
	if stats.ValidID(token) {
		return token
	}
	return ""
}

func (c *client) readLoop(ctx context.Context, r *Room) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var m inMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if !r.post(msgEv{c: c, msg: m}) {
			return
		}
	}
}

// writeLoop drains c.send until the room closes it, then closes the socket.
func (c *client) writeLoop(ctx context.Context) {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	defer c.conn.CloseNow()
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func closeWithError(ctx context.Context, conn *websocket.Conn, code string) {
	b, _ := json.Marshal(errorMsg{T: "error", Code: code})
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, b)
	conn.Close(websocket.StatusPolicyViolation, code)
}
