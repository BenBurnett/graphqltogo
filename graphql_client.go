package graphqltogo

type GraphQLClient struct {
	*GraphQLHTTPClient
	*GraphQLWebSocketClient
}

func NewClient(endpoint string) *GraphQLClient {
	wsEndpoint := endpoint
	if wsEndpoint[:4] == "http" {
		wsEndpoint = "ws" + wsEndpoint[4:]
	}
	return &GraphQLClient{
		GraphQLHTTPClient:      NewHTTPClient(endpoint),
		GraphQLWebSocketClient: NewWebSocketClient(wsEndpoint),
	}
}

type GraphQLResponse struct {
	Data   interface{}              `json:"data"`
	Errors []map[string]interface{} `json:"errors"`
}
