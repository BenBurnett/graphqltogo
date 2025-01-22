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
	"syscall"

	"github.com/gorilla/websocket"
)

type GraphQLClient struct {
	endpoint string
}

func NewClient(endpoint string) *GraphQLClient {
	return &GraphQLClient{endpoint: endpoint}
}

type GraphQLResponse struct {
	Data   map[string]interface{}   `json:"data"`
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
	err = json.Unmarshal(body, &result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %v", result.Errors[0]["message"])
	}

	data, err := json.Marshal(result.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %w", err)
	}

	return json.Unmarshal(data, target)
}

func (client *GraphQLClient) Subscribe(operation string, variables map[string]interface{}, onMessage func(data map[string]interface{}), onError func(err error)) error {
	header := http.Header{}
	header.Set("Sec-WebSocket-Protocol", "graphql-ws")

	wsEndpoint := client.endpoint
	if wsEndpoint[:4] == "http" {
		wsEndpoint = "ws" + wsEndpoint[4:]
	}

	fmt.Println("Connecting to WebSocket endpoint:", wsEndpoint)
	conn, resp, err := websocket.DefaultDialer.Dial(wsEndpoint, header)
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

	initMessage := map[string]interface{}{
		"type": "connection_init",
	}
	startMessage := map[string]interface{}{
		"id":   "1",
		"type": "start",
		"payload": map[string]interface{}{
			"query":     operation,
			"variables": variables,
		},
	}

	if err := conn.WriteJSON(initMessage); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send init message: %w", err)
	}

	if err := conn.WriteJSON(startMessage); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send start message: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	go func() {
		defer conn.Close()
		defer cancel()
		for {
			select {
			case <-interrupt:
				fmt.Println("Received interrupt signal, closing connection...")
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			case <-ctx.Done():
				fmt.Println("Context cancelled, closing connection...")
				conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					onError(fmt.Errorf("failed to read message: %w", err))
					return
				}

				var result map[string]interface{}
				if err := json.Unmarshal(message, &result); err != nil {
					onError(fmt.Errorf("failed to unmarshal message: %w", err))
					return
				}

				switch result["type"] {
				case "data":
					payload := result["payload"].(map[string]interface{})
					onMessage(payload["data"].(map[string]interface{}))
				case "error":
					onError(fmt.Errorf("GraphQL error: %v", result["payload"]))
				case "complete":
					fmt.Println("Subscription complete")
					return
				}
			}
		}
	}()

	return nil
}
