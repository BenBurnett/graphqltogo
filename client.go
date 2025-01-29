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

type Subscription struct {
	Channel   chan interface{}
	Query     string
	Variables map[string]interface{}
}

type GraphQLClient struct {
	httpEndpoint     string
	wsEndpoint       string
	authHeader       string
	httpClient       *http.Client
	wsConn           *websocket.Conn
	counter          int64
	mu               sync.Mutex
	subs             map[string]Subscription
	wg               sync.WaitGroup
	closing          bool
	authErrorHandler func()
}

type ClientOption func(*GraphQLClient)

func WithWebSocket(wsEndpoint string) ClientOption {
	return func(client *GraphQLClient) {
		client.wsEndpoint = wsEndpoint
	}
}

func NewClient(httpEndpoint string, opts ...ClientOption) *GraphQLClient {
	client := &GraphQLClient{
		httpEndpoint: httpEndpoint,
		httpClient:   &http.Client{},
		subs:         make(map[string]Subscription),
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func (client *GraphQLClient) SetAuthHeader(authHeader string) {
	client.authHeader = authHeader
}

func (client *GraphQLClient) SetAuthErrorHandler(handler func()) {
	client.authErrorHandler = handler
}

func (client *GraphQLClient) Execute(operation string, variables map[string]interface{}, target interface{}) error {
	requestBody, err := json.Marshal(map[string]interface{}{
		"query":     operation,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", client.httpEndpoint, bytes.NewBuffer(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if client.authHeader != "" {
		req.Header.Set("Authorization", client.authHeader)
	}

	resp, err := client.httpClient.Do(req)
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

func (client *GraphQLClient) generateUniqueID() string {
	return strconv.FormatInt(atomic.AddInt64(&client.counter, 1), 10)
}

func (client *GraphQLClient) Subscribe(operation string, variables map[string]interface{}) (chan interface{}, string, error) {
	client.mu.Lock()
	if client.wsConn == nil || client.closing {
		client.mu.Unlock()
		if err := client.openWebSocket(); err != nil {
			return nil, "", err
		}
		client.mu.Lock()
	}

	subID := client.generateUniqueID()
	subChan := make(chan interface{})
	client.subs[subID] = Subscription{
		Channel:   subChan,
		Query:     operation,
		Variables: variables,
	}

	client.mu.Unlock()

	startMessage := WebSocketMessage{
		ID:   subID,
		Type: "subscribe",
		Payload: map[string]interface{}{
			"query":     operation,
			"variables": variables,
		},
	}

	if err := client.wsConn.WriteJSON(startMessage); err != nil {
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
	conn := client.wsConn
	client.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no active WebSocket connection")
	}

	stopMessage := WebSocketMessage{
		ID:   subID,
		Type: "complete",
	}

	fmt.Println("Unsubscribing from subscription:", subID)
	if err := conn.WriteJSON(stopMessage); err != nil {
		return fmt.Errorf("failed to send stop message: %w", err)
	}

	client.mu.Lock()
	delete(client.subs, subID)
	client.mu.Unlock()

	return nil
}

func (client *GraphQLClient) Close() {
	client.mu.Lock()
	if client.wsConn != nil {
		client.mu.Unlock()
		client.closeWebSocket()
	} else {
		client.mu.Unlock()
	}
}

type GraphQLResponse struct {
	Data   interface{}              `json:"data"`
	Errors []map[string]interface{} `json:"errors"`
}
