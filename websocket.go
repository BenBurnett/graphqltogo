package graphqltogo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const maxRetries = 5
const retryInterval = 2 * time.Second

type WebSocketMessage struct {
	Type    string                 `json:"type"`
	ID      string                 `json:"id,omitempty"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

func (client *GraphQLClient) openWebSocket() error {
	client.mu.Lock()
	if client.wsConn != nil {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock() // Unlock before dialing

	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-transport-ws")

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
		"payload": map[string]interface{}{
			"Authorization": client.authHeader,
		},
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

		var result WebSocketMessage
		if err := conn.ReadJSON(&result); err != nil {
			client.mu.Lock()
			client.wsConn = nil
			client.mu.Unlock()

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Println("WebSocket closed:", err)
				return
			}

			if websocket.IsCloseError(err, 4401, 4403) {
				fmt.Println("Authentication error:", err)
				if client.authErrorHandler != nil {
					client.authErrorHandler()
				}
			} else {
				fmt.Println("WebSocket read error:", err)
			}
			client.reconnect()
			return
		}

		switch result.Type {
		case "next":
			subID := result.ID
			payload := result.Payload
			client.mu.Lock()
			sub, ok := client.subs[subID]
			client.mu.Unlock()
			if !ok {
				fmt.Println("Subscription not found for ID:", subID)
				break
			}

			target := sub.NewTarget()
			jsonData, err := json.Marshal(payload)
			if err != nil {
				fmt.Println("Error serializing payload:", err)
				break
			}

			err = json.Unmarshal(jsonData, target)
			if err != nil {
				fmt.Println("Error deserializing payload:", err)
				break
			}

			sub.Channel <- target

		case "error":
			subID := result.ID
			payload := result.Payload
			client.mu.Lock()
			if sub, ok := client.subs[subID]; ok {
				sub.Channel <- fmt.Errorf("subscription error: %v", payload)
			}
			client.mu.Unlock()

		case "complete":
			fmt.Println("Subscription completed")
			subID := result.ID
			client.mu.Lock()
			if sub, ok := client.subs[subID]; ok {
				close(sub.Channel)
				delete(client.subs, subID)
			}
			shouldClose := len(client.subs) == 0
			client.mu.Unlock()

			if shouldClose {
				client.closeWebSocket()
			}

		case "connection_ack":
			fmt.Println("WebSocket connection established")

		case "ping":
			pongMessage := WebSocketMessage{
				Type: "pong",
			}
			if err := conn.WriteJSON(pongMessage); err != nil {
				fmt.Println("Failed to send pong message:", err)
			}

		case "pong":
			// Do nothing, just keep-alive

		default:
			fmt.Println("Unknown message type:", result.Type)
		}
	}
}

func (client *GraphQLClient) reconnect() {
	client.mu.Lock()
	if client.wsConn != nil {
		client.mu.Unlock()
		return
	}
	client.mu.Unlock()

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
	for subID, sub := range client.subs {
		startMessage := map[string]interface{}{
			"id":   subID,
			"type": "subscribe",
			"payload": map[string]interface{}{
				"query":     sub.Query,
				"variables": sub.Variables,
			},
		}
		if err := client.wsConn.WriteJSON(startMessage); err != nil {
			fmt.Printf("Failed to resubscribe to %s: %v\n", subID, err)
			close(sub.Channel)
			delete(client.subs, subID)
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
