package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/robalyx/rotector/internal/rpc/proto"
	"github.com/twitchtv/twirp"
)

func main() {
	// Get user ID from command line
	if len(os.Args) <= 1 {
		log.Println("Usage: get_user <user_id> [api_key]")
		return
	}
	userID := os.Args[1]

	// Create context
	ctx := context.Background()

	// Add API key if provided
	if len(os.Args) > 2 {
		apiKey := os.Args[2]
		header := make(http.Header)
		header.Set("Authorization", "Bearer "+apiKey)

		var err error
		ctx, err = twirp.WithHTTPRequestHeaders(ctx, header)
		if err != nil {
			log.Printf("Error setting headers: %v\n", err)
			return
		}
	}

	// Create Twirp client
	client := proto.NewRotectorServiceProtobufClient("http://localhost:8080", &http.Client{})

	// Make request
	resp, err := client.GetUser(ctx, &proto.GetUserRequest{
		UserId: userID,
	})
	if err != nil {
		log.Printf("Error getting user: %v\n", err)
		return
	}

	// Check if user exists
	if resp.GetStatus() == proto.UserStatus_USER_STATUS_UNFLAGGED {
		log.Printf("User %s: NOT FOUND\n", userID)
		return
	}

	// Pretty print the full response
	jsonBytes, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		log.Printf("Error marshalling user: %v\n", err)
		return
	}
	log.Println(string(jsonBytes))
}
