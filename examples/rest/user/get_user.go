package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	// Get user ID from command line
	if len(os.Args) <= 1 {
		log.Println("Usage: get_user <user_id> [api_key]")
		return
	}
	userID := os.Args[1]

	// Create HTTP client
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest(http.MethodGet, "http://localhost:8080/v1/users/"+userID, nil)
	if err != nil {
		log.Printf("Error creating request: %v\n", err)
		return
	}

	// Add API key if provided
	if len(os.Args) > 2 {
		apiKey := os.Args[2]
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error making request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// Check if user exists
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("User %s: NOT FOUND\n", userID)
		return
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response: %v\n", err)
		return
	}

	// Pretty print the full response
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err != nil {
		log.Printf("Error formatting JSON: %v\n", err)
		return
	}

	log.Println(prettyJSON.String())
}
