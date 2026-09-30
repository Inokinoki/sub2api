package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

// deepCheck validates the deep-control credential end to end: refresh via
// exchange_user_api_key, then a minimal agent Run with the CLI profile.
func deepCheck() {
	raw, err := os.ReadFile("/tmp/cursor-oauth-credentials.json")
	if err != nil {
		fmt.Printf("DEEP_FATAL: read credentials: %v\n", err)
		return
	}
	var creds struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		fmt.Printf("DEEP_FATAL: parse: %v\n", err)
		return
	}
	fmt.Printf("deep-control tokens loaded (access %d chars, refresh %d chars)\n", len(creds.AccessToken), len(creds.RefreshToken))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := cursor.UnaryHTTPClient("")
	if err != nil {
		fmt.Printf("DEEP_FATAL: %v\n", err)
		return
	}

	// 1. Refresh via /oauth/token (verified live: deep-control refresh tokens
	// use the standard OAuth endpoint; exchange_user_api_key is for API keys).
	fmt.Println("=== refresh via /oauth/token ===")
	refreshed, err := cursor.RefreshSession(ctx, client, creds.RefreshToken)
	if err != nil {
		fmt.Printf("DEEP_REFRESH_FAILED: %v\n", err)
		return
	}
	fmt.Printf("DEEP_REFRESH_OK access(%d): %s… rotated: %v\n", len(refreshed.AccessToken), mask(refreshed.AccessToken), refreshed.RefreshToken != "")

	// 2. Minimal agent Run with the CLI profile (no machine ids).
	fmt.Println("=== minimal StreamChat (CLI profile, no machine ids) ===")
	clientVersion := os.Getenv("CURSOR_DEMO_CLIENT_VERSION")
	if clientVersion == "" {
		clientVersion = "cli-2026.07.23-e383d2b" // oh-my-pi's current CLI version string
	}
	chatClient := cursor.NewClient(cursor.Credentials{AccessToken: creds.AccessToken, ClientVersion: clientVersion})
	resp, err := chatClient.StreamChat(ctx, cursor.AgentRunRequest{
		Model:    "default",
		Messages: []cursor.ChatMessage{{Role: "user", Content: "Reply with exactly: ok"}},
	})
	if err != nil {
		fmt.Printf("DEEP_CHAT_FAILED: %v\n", err)
		return
	}
	defer resp.Body.Close()
	var text string
	_, connectErr := cursor.ConsumeAssistantStream(resp.Body, func(ev cursor.StreamEvent) error {
		if ev.Type == "text" {
			text += ev.Text
		}
		return nil
	})
	fmt.Printf("DEEP_CHAT_OK status=%d text=%q connectErr=%q\n", resp.StatusCode, text, connectErr)
	fmt.Println("DEEP_CHECK_DONE")
}
