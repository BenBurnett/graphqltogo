package graphqltogo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

const testEndpoint = "http://example.com"

func TestNewClient(t *testing.T) {
	client := NewClient(testEndpoint)
	assert.NotNil(t, client)
	assert.Equal(t, testEndpoint, client.httpEndpoint)
}

func TestSetHeader(t *testing.T) {
	client := NewClient(testEndpoint)
	client.SetHeader("Authorization", "Bearer token")
	assert.Equal(t, "Bearer token", client.headers["Authorization"])
}

func setupHTTPTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": {"message": "hello"}}`))
	}))
}

func TestExecute(t *testing.T) {
	server := setupHTTPTestServer(t)
	defer server.Close()

	client := NewClient(server.URL)
	var result GraphQLResponse[map[string]string]
	err := client.execute("query { message }", nil, &result)
	assert.NoError(t, err)
	assert.Equal(t, "hello", result.Data["message"])
}
