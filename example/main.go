package main

import (
	"fmt"

	"github.com/BenBurnett/graphqltogo"
)

type HelloResponse struct {
	Hello string `json:"hello"`
}

const HelloQuery = `
	query {
		hello
	}
`

const ErrorQuery = `
	query {
		errorQuery
	}
`

func main() {
	client := graphqltogo.NewClient("http://localhost:4000/graphql")

	// Query Hello
	var helloResult HelloResponse
	err := client.Query(HelloQuery, nil, &helloResult)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Hello Response:", helloResult.Hello)

	// Query Error
	err = client.Query(ErrorQuery, nil, &struct{}{})
	if err != nil {
		fmt.Println("Error from errorQuery:", err)
		return
	}
}
