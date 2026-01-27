package main

import "os"

func getAPIToken(alternative string) string {
	token := os.Getenv("API_TOKEN")
	if token == "" {
		token = os.Getenv(alternative)
	}

	return token
}
