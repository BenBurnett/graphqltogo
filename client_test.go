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

func TestNewClient(t *testing.T) {
	client := NewClient("http://example.com")
	assert.NotNil(t, client)
	assert.Equal(t, "http://example.com", client.httpEndpoint)
}

func TestSetAuthHeader(t *testing.T) {
	client := NewClient("http://example.com")
	client.SetAuthHeader("Bearer token")
	assert.Equal(t, "Bearer token", client.authHeader)
}

func TestExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": {"message": "hello"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	var result GraphQLResponse[map[string]string]
	err := client.execute("query { message }", nil, &result)
	assert.NoError(t, err)
	assert.Equal(t, "hello", result.Data["message"])
}

func TestWebSocketConnection(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		assert.NoError(t, err)
		defer conn.Close()

		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			assert.Equal(t, websocket.TextMessage, messageType)
			if strings.Contains(string(message), "connection_init") {
				err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type": "connection_ack"}`))
				if err != nil {
					return
				}
			}
			if strings.Contains(string(message), "connection_terminate") {
				return
			}
		}
	}))
	defer server.Close()

	wsEndpoint := "ws" + server.URL[4:]
	client := NewClient("http://example.com", WithWebSocket(wsEndpoint))
	client.SetAuthHeader("Bearer token")
	err := client.openWebSocket()
	assert.NoError(t, err)
	time.Sleep(1 * time.Second) // Give some time for the WebSocket connection to establish
}
