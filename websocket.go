package graphqltogo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
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

	conn, err := client.dialWebSocket(header)
	if err != nil {
		return err
	}

	client.mu.Lock()
	client.wsConn = conn
	client.mu.Unlock()

	if err := client.sendInitMessage(); err != nil {
		return err
	}

	go client.listen()

	return nil
}

func (client *GraphQLClient) dialWebSocket(header http.Header) (*websocket.Conn, error) {
	var conn *websocket.Conn
	var resp *http.Response
	var err error

	for i := 0; i < maxRetries; i++ {
		fmt.Println("Connecting to WebSocket endpoint:", client.wsEndpoint)
		conn, resp, err = websocket.DefaultDialer.Dial(client.wsEndpoint, header)
		if err == nil {
			break
		}

		client.logDialError(resp, err)
		fmt.Printf("Retrying in %v...\n", retryInterval)
		time.Sleep(retryInterval)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to dial WebSocket after %d attempts: %w", maxRetries, err)
	}

	return conn, nil
}

func (client *GraphQLClient) logDialError(resp *http.Response, err error) {
	if resp != nil {
		fmt.Println("Handshake failed with status:", resp.Status)
		body, _ := io.ReadAll(resp.Body)
		fmt.Println("Response body:", string(body))
	} else {
		fmt.Println("Dial error:", err)
	}
}

func (client *GraphQLClient) sendInitMessage() error {
	initMessage := map[string]interface{}{
		"type": "connection_init",
		"payload": map[string]interface{}{
			"Authorization": client.authHeader,
		},
	}
	if err := client.wsConn.WriteJSON(initMessage); err != nil {
		return fmt.Errorf("failed to send init message: %w", err)
	}
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
			client.handleReadError(err)
			return
		}

		client.handleMessage(result)
	}
}

func (client *GraphQLClient) handleReadError(err error) {
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
}

func (client *GraphQLClient) handleMessage(result WebSocketMessage) {
	switch result.Type {
	case "next", "error":
		client.handleDataMessage(result)
	case "complete":
		client.handleCompleteMessage(result.ID)
	case "connection_ack":
		fmt.Println("WebSocket connection established")
	case "ping":
		client.sendPong()
	case "pong":
		// No action needed
	default:
		fmt.Println("Unknown message type:", result.Type)
	}
}

func (client *GraphQLClient) handleDataMessage(result WebSocketMessage) {
	subID := result.ID
	payload := result.Payload
	client.mu.Lock()
	sub, ok := client.subs[subID]
	client.mu.Unlock()
	if !ok {
		fmt.Println("Subscription not found for ID:", subID)
		return
	}

	target := sub.NewTarget()
	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("Error serializing payload:", err)
		return
	}

	err = json.Unmarshal(jsonData, target)
	if err != nil {
		fmt.Println("Error deserializing payload:", err)
		return
	}

	sub.Channel <- target
}

func (client *GraphQLClient) handleCompleteMessage(subID string) {
	fmt.Println("Subscription completed")
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
}

func (client *GraphQLClient) sendPong() {
	client.mu.Lock()
	conn := client.wsConn
	client.mu.Unlock()

	pongMessage := WebSocketMessage{
		Type: "pong",
	}
	if err := conn.WriteJSON(pongMessage); err != nil {
		fmt.Println("Failed to send pong message:", err)
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
	defer client.wg.Wait()
	defer client.mu.Unlock()
	if client.wsConn != nil {
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
		fmt.Println("WebSocket connection closed")
	}
}

func (client *GraphQLClient) generateUniqueID() string {
	return strconv.FormatInt(atomic.AddInt64(&client.counter, 1), 10)
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

	if err := client.sendSubscribeMessage(subID, operation, variables); err != nil {
		client.cleanupSubscription(subID)
		return nil, nil, err
	}

	return subChan, func() error {
		return client.unsubscribe(subID)
	}, nil
}

func (client *GraphQLClient) sendSubscribeMessage(subID, operation string, variables map[string]interface{}) error {
	startMessage := WebSocketMessage{
		ID:   subID,
		Type: "subscribe",
		Payload: map[string]interface{}{
			"query":     operation,
			"variables": variables,
		},
	}

	if err := client.wsConn.WriteJSON(startMessage); err != nil {
		return fmt.Errorf("failed to send start message: %w", err)
	}
	return nil
}

func (client *GraphQLClient) cleanupSubscription(subID string) {
	client.mu.Lock()
	delete(client.subs, subID)
	shouldClose := len(client.subs) == 0
	client.mu.Unlock()

	if shouldClose {
		client.closeWebSocket()
	}
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
