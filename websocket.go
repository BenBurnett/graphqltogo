package graphqltogo

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const maxRetries = 5
const retryInterval = 2 * time.Second

func (client *GraphQLClient) openWebSocket() error {
	client.mu.Lock()
	if client.wsConn != nil {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock() // Unlock before dialing

	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-ws")
	if client.authHeader != "" {
		header.Set("Authorization", client.authHeader)
	}

	var conn *websocket.Conn
	var resp *http.Response
	var err error

	for i := 0; i < maxRetries; i++ {
		fmt.Println("Connecting to WebSocket endpoint:", client.wsEndpoint)
		conn, resp, err = websocket.DefaultDialer.Dial(client.wsEndpoint, header)
		if err == nil {
			break
		}

		if resp != nil {
			fmt.Println("Handshake failed with status:", resp.Status)
			body, _ := io.ReadAll(resp.Body)
			fmt.Println("Response body:", string(body))
		} else {
			fmt.Println("Dial error:", err)
		}

		fmt.Printf("Retrying in %v...\n", retryInterval)
		time.Sleep(retryInterval)
	}

	if err != nil {
		return fmt.Errorf("failed to dial WebSocket after %d attempts: %w", maxRetries, err)
	}

	client.mu.Lock()
	client.wsConn = conn
	client.mu.Unlock()

	initMessage := map[string]interface{}{
		"type": "connection_init",
	}
	if err := client.wsConn.WriteJSON(initMessage); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}

	go client.listen()

	return nil
}

func (client *GraphQLClient) listen() {
	client.wg.Add(1)
	defer client.wg.Done()

	for {
		client.mu.Lock()
		conn := client.wsConn
		client.mu.Unlock()

		if conn == nil {
			fmt.Println("WebSocket connection is nil, stopping listen goroutine")
			return
		}

		var result map[string]interface{}
		if err := conn.ReadJSON(&result); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Println("WebSocket closed:", err)
				client.reconnect()
			} else {
				fmt.Println("WebSocket read error:", err)
				client.reconnect()
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
				delete(client.subQueries, subID)
				delete(client.subVars, subID)
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

func (client *GraphQLClient) reconnect() {
	client.closeWebSocket()
	for {
		fmt.Println("Attempting to reconnect...")
		if err := client.openWebSocket(); err == nil {
			client.resubscribeAll()
			return
		}
		fmt.Printf("Retrying in %v...\n", retryInterval)
		time.Sleep(retryInterval)
	}
}

func (client *GraphQLClient) resubscribeAll() {
	client.mu.Lock()
	defer client.mu.Unlock()
	for subID, subChan := range client.subs {
		query := client.subQueries[subID]
		variables := client.subVars[subID]
		startMessage := map[string]interface{}{
			"id":   subID,
			"type": "start",
			"payload": map[string]interface{}{
				"query":     query,
				"variables": variables,
			},
		}
		if err := client.wsConn.WriteJSON(startMessage); err != nil {
			fmt.Printf("Failed to resubscribe to %s: %v\n", subID, err)
			close(subChan)
			delete(client.subs, subID)
			delete(client.subQueries, subID)
			delete(client.subVars, subID)
		}
	}
}

func (client *GraphQLClient) closeWebSocket() {
	client.mu.Lock()
	if client.wsConn != nil {
		client.closing = true
		closeMessage := map[string]interface{}{
			"type": "connection_terminate",
		}
		if err := client.wsConn.WriteJSON(closeMessage); err != nil {
			fmt.Println("Failed to send close message:", err)
		}
		if err := client.wsConn.Close(); err != nil {
			fmt.Println("Failed to close WebSocket connection:", err)
		}
		client.wsConn = nil
		client.mu.Unlock() // Release the lock before waiting
		client.wg.Wait()
		client.mu.Lock()
		client.closing = false
		fmt.Println("WebSocket connection closed")
	}
	client.mu.Unlock()
}
