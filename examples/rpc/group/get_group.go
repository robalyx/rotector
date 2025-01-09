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
	// Get group ID from command line
	if len(os.Args) <= 1 {
		log.Println("Usage: get_group <group_id> [api_key]")
		return
	}
	groupID := os.Args[1]

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
	resp, err := client.GetGroup(ctx, &proto.GetGroupRequest{
		GroupId: groupID,
	})
	if err != nil {
		log.Printf("Error getting group: %v\n", err)
		return
	}

	// Check if group exists
	if resp.GetStatus() == proto.GroupStatus_GROUP_STATUS_UNFLAGGED {
		log.Printf("Group %s: NOT FOUND\n", groupID)
		return
	}

	// Pretty print the full response
	jsonBytes, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		log.Printf("Error marshalling group: %v\n", err)
		return
	}
	log.Println(string(jsonBytes))
}
