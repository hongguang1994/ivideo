package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"ivideo/server/internal/logging"
)

var websocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		requestHost := strings.Split(r.Host, ":")[0]
		return strings.EqualFold(parsed.Hostname(), requestHost)
	},
}

// StreamLogs 先发送近期日志，再持续推送新增日志。GET /api/v1/logs/ws
func (h *Handler) StreamLogs(c *gin.Context) {
	connection, err := websocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer connection.Close()

	history, stream, unsubscribe := logging.DefaultHub().Subscribe()
	defer unsubscribe()
	connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := connection.WriteJSON(gin.H{"type": "history", "entries": history}); err != nil {
		return
	}

	disconnected := make(chan struct{})
	go func() {
		defer close(disconnected)
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-disconnected:
			return
		case entry, ok := <-stream:
			if !ok {
				return
			}
			connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteJSON(gin.H{"type": "entry", "entry": entry}); err != nil {
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}
