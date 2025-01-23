package graphqltogo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
)

type GraphQLClient struct {
	endpoint   string
	wsEndpoint string
	conn       *websocket.Conn
	subCount   int
	subID      int
	mu         sync.Mutex
}

func NewClient(endpoint string) *GraphQLClient {
	wsEndpoint := endpoint
	if wsEndpoint[:4] == "http" {
		wsEndpoint = "ws" + wsEndpoint[4:]
	}
	return &GraphQLClient{endpoint: endpoint, wsEndpoint: wsEndpoint}
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

func (client *GraphQLClient) openWebSocket(onMessage func(data map[string]interface{}), onError func(err error)) error {
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
	client.conn = conn

	initMessage := map[string]interface{}{
		"type": "connection_init",
	}
	if err := client.conn.WriteJSON(initMessage); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	go func() {
		defer cancel()
		for {
			select {
			case <-interrupt:
				fmt.Println("Received interrupt signal, closing connection...")
				client.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			case <-ctx.Done():
				fmt.Println("Context cancelled, closing connection...")
				client.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			default:
				_, message, err := client.conn.ReadMessage()
				if err != nil {
					client.mu.Lock()
					if client.conn == nil {
						client.mu.Unlock()
						return
					}
					client.mu.Unlock()
					fmt.Println("Read error:", err)
					return
				}

				var result map[string]interface{}
				if err := json.Unmarshal(message, &result); err != nil {
					return
				}

				switch result["type"] {
				case "data":
					// Handle data message
					if payload, ok := result["payload"].(map[string]interface{}); ok {
						onMessage(payload["data"].(map[string]interface{}))
					}
				case "error":
					// Handle error message
					if payload, ok := result["payload"].(map[string]interface{}); ok {
						onError(fmt.Errorf("subscription error: %v", payload))
					}
				case "complete":
					fmt.Println("Subscription complete")
					return
				}
			}
		}
	}()

	return nil
}

func (client *GraphQLClient) closeWebSocket() {
	client.conn.Close()
	client.conn = nil
	fmt.Println("WebSocket connection closed")
}

func (client *GraphQLClient) Subscribe(operation string, variables map[string]interface{}, onMessage func(data map[string]interface{}), onError func(err error)) (int, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn == nil {
		if err := client.openWebSocket(onMessage, onError); err != nil {
			return 0, err
		}
	}

	client.subID++
	subID := client.subID

	startMessage := map[string]interface{}{
		"id":   fmt.Sprintf("%d", subID),
		"type": "start",
		"payload": map[string]interface{}{
			"query":     operation,
			"variables": variables,
		},
	}

	if err := client.conn.WriteJSON(startMessage); err != nil {
		client.closeWebSocket()
		return 0, fmt.Errorf("failed to send start message: %w", err)
	}

	client.subCount++
	return subID, nil
}

func (client *GraphQLClient) Unsubscribe(subID int) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn == nil {
		return fmt.Errorf("no active WebSocket connection")
	}

	stopMessage := map[string]interface{}{
		"id":   fmt.Sprintf("%d", subID),
		"type": "stop",
	}

	if err := client.conn.WriteJSON(stopMessage); err != nil {
		return fmt.Errorf("failed to send stop message: %w", err)
	}

	client.subCount--
	if client.subCount == 0 {
		client.closeWebSocket()
	}

	return nil
}

func (client *GraphQLClient) Close() {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.conn != nil {
		client.closeWebSocket()
	}
}
