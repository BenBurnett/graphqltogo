package main

import (
	"fmt"
	"sync"

	"github.com/BenBurnett/graphqltogo"
)

func authenticate(client *graphqltogo.GraphQLClient, username, password string) error {
	const tokenAuthMutation = `
		mutation tokenAuth($username: String!, $password: String!) {
			tokenAuth(username: $username, password: $password) {
				token
			}
		}`

	type tokenAuthResponse struct {
		TokenAuth struct {
			Token string
		}
	}

	result, err := graphqltogo.Execute[tokenAuthResponse](client, tokenAuthMutation, map[string]interface{}{
		"username": username,
		"password": password,
	})
	if err != nil {
		return err
	}

	client.SetHeader("Authorization", "Bearer "+result.Data.TokenAuth.Token)
	return nil
}

func analyticsSummary(client *graphqltogo.GraphQLClient) error {
	const analyticsSummary = `
		query analyticsSummary {
			analyticsDeviceCount
			analyticsTaskCount
			analyticsPluginCount
			analyticsFileSummary {
				count
			}
		}`

	type analyticsSummaryResponse struct {
		AnalyticsDeviceCount int
		AnalyticsTaskCount   int
		AnalyticsPluginCount int
		AnalyticsFileSummary struct {
			Count int
		}
	}

	result, err := graphqltogo.Execute[analyticsSummaryResponse](client, analyticsSummary, nil)
	if err != nil {
		return err
	}

	fmt.Printf("Devices: %d, Tasks: %d, Plugins: %d, Files: %d\n",
		result.Data.AnalyticsDeviceCount,
		result.Data.AnalyticsTaskCount,
		result.Data.AnalyticsPluginCount,
		result.Data.AnalyticsFileSummary.Count)

	return nil
}

func newActivity(client *graphqltogo.GraphQLClient, wg *sync.WaitGroup) error {
	const newActivity = `
		subscription newActivity {
			newActivity {
				activity {
					title
				}
			}
		}`

	type newActivityResponse struct {
		NewActivity struct {
			Activity struct {
				Title string
			}
		}
	}

	subChan, _, err := graphqltogo.Subscribe[newActivityResponse](client, newActivity, nil)
	if err != nil {
		return fmt.Errorf("error from subscription: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for msg := range subChan {
			fmt.Printf(" -- Title: %s\n", msg.Data.NewActivity.Activity.Title)
		}
	}()

	return nil
}

func main() {
	host := "localhost:4000"
	client := graphqltogo.NewClient("http://"+host+"/graphql", graphqltogo.WithWebSocket("ws://"+host+"/graphql"))
	defer client.Close()

	client.SetAuthErrorHandler(func() {
		fmt.Println("Handling WebSocket authentication error, re-authenticating...")
		if err := authenticate(client, "admin", "admin"); err != nil {
			fmt.Println("Re-authentication Error:", err)
		}
	})

	if err := authenticate(client, "admin", "admin"); err != nil {
		fmt.Println("Authentication Error:", err)
		return
	}

	if err := analyticsSummary(client); err != nil {
		fmt.Println("Analytics Summary Error:", err)
		return
	}

	var wg sync.WaitGroup
	if err := newActivity(client, &wg); err != nil {
		fmt.Println("New Activity Subscription Error:", err)
		return
	}

	wg.Wait()
}
