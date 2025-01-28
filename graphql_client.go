package graphqltogo

type GraphQLClient struct {
	*GraphQLHTTPClient
	*GraphQLWebSocketClient
}

type ClientOption func(*GraphQLClient)

func WithWebSocket(wsEndpoint string) ClientOption {
	return func(client *GraphQLClient) {
		client.GraphQLWebSocketClient = NewWebSocketClient(wsEndpoint)
	}
}

func NewClient(endpoint string, opts ...ClientOption) *GraphQLClient {
	client := &GraphQLClient{
		GraphQLHTTPClient: NewHTTPClient(endpoint),
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

type GraphQLResponse struct {
	Data   interface{}              `json:"data"`
	Errors []map[string]interface{} `json:"errors"`
}
