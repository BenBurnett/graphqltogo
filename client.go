package graphqltogo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type GraphQLClient struct {
	endpoint   string
	wsEndpoint string
	conn       *websocket.Conn
	counter    int64
	mu         sync.Mutex
	subs       map[string]chan interface{}
}

func NewClient(endpoint string) *GraphQLClient {
	wsEndpoint := endpoint
	if wsEndpoint[:4] == "http" {
		wsEndpoint = "ws" + wsEndpoint[4:]
	}
	return &GraphQLClient{
		endpoint:   endpoint,
		wsEndpoint: wsEndpoint,
		subs:       make(map[string]chan interface{}),
	}
}

func (client *GraphQLClient) generateUniqueID() string {
	return strconv.FormatInt(atomic.AddInt64(&client.counter, 1), 10)
}

type GraphQLResponse struct {
	Data   interface{}              `json:"data"`
	Errors []map[string]interface{} `json:"errors"`
}

func (client *GraphQLClient) Execute(operation string, variables map[string]interface{}, target interface{}) error {
	requestBody, err := json.Marshal(map[string]interface{}{
		"query":     operation,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", client.endpoint, bytes.NewBuffer(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	clientHTTP := &http.Client{}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var result GraphQLResponse
	result.Data = target
	err = json.Unmarshal(body, &result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %v", result.Errors[0]["message"])
	}

	return nil
}

func (client *GraphQLClient) openWebSocket() error {
	client.mu.Lock()
	if client.conn != nil {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock()

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

func (client *GraphQLClient) listen() {
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

func (client *GraphQLClient) closeWebSocket() {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn != nil {
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
		fmt.Println("WebSocket connection closed")
	}
}

func (client *GraphQLClient) Subscribe(operation string, variables map[string]interface{}) (chan interface{}, string, error) {
	client.mu.Lock()
	if client.conn == nil {
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

func (client *GraphQLClient) Unsubscribe(subID string) error {
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

func (client *GraphQLClient) Close() {
	client.mu.Lock()
	if client.conn != nil {
		client.mu.Unlock()
		client.closeWebSocket()
	} else {
		client.mu.Unlock()
	}
}
