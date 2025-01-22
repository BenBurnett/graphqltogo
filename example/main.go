package main

import (
	"fmt"
	"time"

	"github.com/BenBurnett/graphqltogo"
)

type HelloResponse struct {
	Hello string `json:"hello"`
}

type EchoResponse struct {
	Echo string `json:"echo"`
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

const EchoMutation = `
	mutation($message: String!) {
		echo(message: $message)
	}
`

const SubscriptionQuery = `
	subscription {
		messageSent
	}
`

func main() {
	client := graphqltogo.NewClient("http://localhost:4000/graphql")

	// Query Hello
	var helloResult HelloResponse
	err := client.Execute(HelloQuery, nil, &helloResult)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Hello Response:", helloResult.Hello)

	// Query Error
	err = client.Execute(ErrorQuery, nil, &struct{}{})
	if err != nil {
		fmt.Println("Error from errorQuery:", err)
	}

	// Mutation Echo
	var echoResult EchoResponse
	err = client.Execute(EchoMutation, map[string]interface{}{"message": "Hello, mutation!"}, &echoResult)
	if err != nil {
		fmt.Println("Error from echo mutation:", err)
		return
	}
	fmt.Println("Echo Response:", echoResult.Echo)

	// Subscription
	err = client.Subscribe(SubscriptionQuery, nil, func(data map[string]interface{}) {
		fmt.Println("Subscription message received:", data["messageSent"])
	}, func(err error) {
		fmt.Println("Subscription error:", err)
	})
	if err != nil {
		fmt.Println("Error from subscription:", err)
		return
	}

	// Mutation Echo
	err = client.Execute(EchoMutation, map[string]interface{}{"message": "Hello, mutation again!"}, &echoResult)
	if err != nil {
		fmt.Println("Error from echo mutation:", err)
		return
	}
	fmt.Println("Echo Response:", echoResult.Echo)

	// Keep the main function running to receive subscription messages
	time.Sleep(5 * time.Second)
}
