package graphqltogo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GraphQLHTTPClient struct {
	endpoint string
}

func NewHTTPClient(endpoint string) *GraphQLHTTPClient {
	return &GraphQLHTTPClient{
		endpoint: endpoint,
	}
}

func (client *GraphQLHTTPClient) Execute(operation string, variables map[string]interface{}, target interface{}) error {
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
