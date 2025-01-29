package graphqltogo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

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

func WithWebSocket(wsEndpoint string) ClientOption {
	return func(client *GraphQLClient) {
		client.wsEndpoint = wsEndpoint
	}
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
