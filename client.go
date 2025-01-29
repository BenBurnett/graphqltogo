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

type GraphQLResponse[T interface{}] struct {
	Data   T                        `json:"data,omitempty"`
	Errors []map[string]interface{} `json:"errors,omitempty"`
}

type Subscription struct {
	Channel   chan interface{}
	Query     string
	Variables map[string]interface{}
	NewTarget func() interface{}
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

func Execute[T interface{}](client *GraphQLClient, operation string, variables map[string]interface{}) (*GraphQLResponse[T], error) {
	var result GraphQLResponse[T]
	err := client.execute(operation, variables, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

func (client *GraphQLClient) execute(operation string, variables map[string]interface{}, result interface{}) error {
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

	err = json.Unmarshal(body, &result)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	return nil
}

func (client *GraphQLClient) generateUniqueID() string {
	return strconv.FormatInt(atomic.AddInt64(&client.counter, 1), 10)
}

func Subscribe[T interface{}](client *GraphQLClient, operation string, variables map[string]interface{}) (<-chan *GraphQLResponse[T], func() error, error) {
	subChan, subId, err := client.subscribe(operation, variables, func() interface{} {
		return new(GraphQLResponse[T])
	})
	if err != nil {
		return nil, nil, err
	}
	typedChan := make(chan *GraphQLResponse[T])
	client.wg.Add(1)
	go func() {
		defer client.wg.Done()
		defer close(typedChan)
		for msg := range subChan {
			typedChan <- msg.(*GraphQLResponse[T])
		}
	}()
	return typedChan, subId, nil
}

func (client *GraphQLClient) subscribe(operation string, variables map[string]interface{}, newTarget func() interface{}) (<-chan interface{}, func() error, error) {
	client.mu.Lock()
	if client.wsConn == nil {
		client.mu.Unlock()
		if err := client.openWebSocket(); err != nil {
			return nil, nil, err
		}
		client.mu.Lock()
	}

	subID := client.generateUniqueID()
	subChan := make(chan interface{})
	client.subs[subID] = Subscription{
		Channel:   subChan,
		Query:     operation,
		Variables: variables,
		NewTarget: newTarget,
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
		return nil, nil, fmt.Errorf("failed to send start message: %w", err)
	}

	return subChan, func() error {
		return client.unsubscribe(subID)
	}, nil
}

func (client *GraphQLClient) unsubscribe(subID string) error {
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
