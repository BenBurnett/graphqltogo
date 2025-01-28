package graphqltogo

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type GraphQLWebSocketClient struct {
	wsEndpoint string
	conn       *websocket.Conn
	counter    int64
	mu         sync.Mutex
	subs       map[string]chan interface{}
	wg         sync.WaitGroup
	closing    bool
}

func NewWebSocketClient(wsEndpoint string) *GraphQLWebSocketClient {
	return &GraphQLWebSocketClient{
		wsEndpoint: wsEndpoint,
		subs:       make(map[string]chan interface{}),
	}
}

func (client *GraphQLWebSocketClient) generateUniqueID() string {
	return strconv.FormatInt(atomic.AddInt64(&client.counter, 1), 10)
}

func (client *GraphQLWebSocketClient) openWebSocket() error {
	client.mu.Lock()
	if client.conn != nil {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock() // Unlock before dialing

	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-ws")

	fmt.Println("Connecting to WebSocket endpoint:", client.wsEndpoint)
	conn, resp, err := websocket.DefaultDialer.Dial(client.wsEndpoint, header)
	if err != nil {
		if resp != nil {
			fmt.Println("Handshake failed with status:", resp.Status)
			body, _ := io.ReadAll(resp.Body)
			fmt.Println("Response body:", string(body))
		} else {
			fmt.Println("Dial error:", err)
		}
		return fmt.Errorf("failed to dial WebSocket: %w", err)
	}

	client.mu.Lock()
	client.conn = conn
	client.mu.Unlock()

	initMessage := map[string]interface{}{
		"type": "connection_init",
	}
	if err := client.conn.WriteJSON(initMessage); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}

	go client.listen()

	return nil
}

func (client *GraphQLWebSocketClient) listen() {
	client.wg.Add(1)
	defer client.wg.Done()

	for {
		client.mu.Lock()
		conn := client.conn
		client.mu.Unlock()

		if conn == nil {
			fmt.Println("WebSocket connection is nil, stopping listen goroutine")
			return
		}

		var result map[string]interface{}
		if err := conn.ReadJSON(&result); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Println("WebSocket closed:", err)
			} else {
				fmt.Println("WebSocket read error:", err)
			}
			return
		}

		messageType := result["type"].(string)

		switch messageType {
		case "data":
			subID := result["id"].(string)
			payload := result["payload"]
			client.mu.Lock()
			if subChan, ok := client.subs[subID]; ok {
				subChan <- payload
			}
			client.mu.Unlock()

		case "error":
			subID := result["id"].(string)
			payload := result["payload"]
			client.mu.Lock()
			if subChan, ok := client.subs[subID]; ok {
				subChan <- fmt.Errorf("subscription error: %v", payload)
			}
			client.mu.Unlock()

		case "complete":
			fmt.Println("Subscription completed")
			subID := result["id"].(string)
			client.mu.Lock()
			if subChan, ok := client.subs[subID]; ok {
				close(subChan)
				delete(client.subs, subID)
			}
			shouldClose := len(client.subs) == 0
			client.mu.Unlock()

			if shouldClose {
				client.closeWebSocket()
			}

		case "connection_ack":
			fmt.Println("WebSocket connection established")

		case "connection_error":
			fmt.Println("WebSocket connection error:", result["payload"])

		case "connection_keep_alive":
			if err := conn.WriteJSON(map[string]interface{}{"type": "connection_ack"}); err != nil {
				fmt.Println("Failed to send connection_ack message:", err)
				return
			}

		default:
			fmt.Println("Unknown message type:", messageType)
		}
	}
}

func (client *GraphQLWebSocketClient) closeWebSocket() {
	client.mu.Lock()
	if client.conn != nil {
		client.closing = true
		closeMessage := map[string]interface{}{
			"type": "connection_terminate",
		}
		if err := client.conn.WriteJSON(closeMessage); err != nil {
			fmt.Println("Failed to send close message:", err)
		}
		if err := client.conn.Close(); err != nil {
			fmt.Println("Failed to close WebSocket connection:", err)
		}
		client.conn = nil
		client.mu.Unlock() // Release the lock before waiting
		client.wg.Wait()
		client.mu.Lock()
		client.closing = false
		fmt.Println("WebSocket connection closed")
	}
	client.mu.Unlock()
}

func (client *GraphQLWebSocketClient) Subscribe(operation string, variables map[string]interface{}) (chan interface{}, string, error) {
	client.mu.Lock()
	if client.conn == nil || client.closing {
		client.mu.Unlock()
		if err := client.openWebSocket(); err != nil {
			return nil, "", err
		}
		client.mu.Lock()
	}

	subID := client.generateUniqueID()
	subChan := make(chan interface{})
	client.subs[subID] = subChan

	client.mu.Unlock()

	startMessage := map[string]interface{}{
		"id":   subID,
		"type": "start",
		"payload": map[string]interface{}{
			"query":     operation,
			"variables": variables,
		},
	}

	if err := client.conn.WriteJSON(startMessage); err != nil {
		client.mu.Lock()
		delete(client.subs, subID)
		shouldClose := len(client.subs) == 0
		client.mu.Unlock()

		if shouldClose {
			client.closeWebSocket()
		}
		return nil, "", fmt.Errorf("failed to send start message: %w", err)
	}

	return subChan, subID, nil
}

func (client *GraphQLWebSocketClient) Unsubscribe(subID string) error {
	client.mu.Lock()
	conn := client.conn
	client.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no active WebSocket connection")
	}

	stopMessage := map[string]interface{}{
		"id":   subID,
		"type": "stop",
	}

	fmt.Println("Unsubscribing from subscription:", subID)
	if err := conn.WriteJSON(stopMessage); err != nil {
		return fmt.Errorf("failed to send stop message: %w", err)
	}

	return nil
}

func (client *GraphQLWebSocketClient) Close() {
	client.mu.Lock()
	if client.conn != nil {
		client.mu.Unlock()
		client.closeWebSocket()
	} else {
		client.mu.Unlock()
	}
}
