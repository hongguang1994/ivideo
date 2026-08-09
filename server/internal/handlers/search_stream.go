package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// StreamSearch 按 jobId 推送渐进式搜索快照。GET /api/v1/search/resources/ws
func (h *Handler) StreamSearch(c *gin.Context) {
	jobID := strings.TrimSpace(c.Query("jobId"))
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 jobId"})
		return
	}
	current, stream, unsubscribe, err := h.discovery.Subscribe(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	connection, err := websocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		unsubscribe()
		return
	}
	defer connection.Close()
	defer unsubscribe()

	connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := connection.WriteJSON(gin.H{"type": "snapshot", "data": current}); err != nil || !current.Pending {
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
		case snapshot, ok := <-stream:
			if !ok {
				return
			}
			connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := connection.WriteJSON(gin.H{"type": "snapshot", "data": snapshot}); err != nil || !snapshot.Pending {
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}
