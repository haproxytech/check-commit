package main

import "os"

func getAPIToken(alterative string) string {
	token := os.Getenv("API_TOKEN")
	if token == "" {
		token = os.Getenv(alterative)
	}

	return token
}
