package graphqltogo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func setupWebSocketTestServer() *httptest.Server {
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if messageType == websocket.TextMessage {
				if strings.Contains(string(message), "connection_init") {
					conn.WriteMessage(websocket.TextMessage, []byte(`{"type": "connection_ack"}`))
				} else if strings.Contains(string(message), "connection_terminate") {
					return
				}
			}
		}
	}))
}

func TestWebSocketConnection(t *testing.T) {
	server := setupWebSocketTestServer()
	defer server.Close()

	wsEndpoint := "ws" + server.URL[4:]
	client := NewClient("http://example.com", WithWebSocket(wsEndpoint))
	client.SetHeader("Authorization", "Bearer token")

	err := client.openWebSocket()
	assert.NoError(t, err)

	time.Sleep(1 * time.Second) // Give some time for the WebSocket connection to establish
}
