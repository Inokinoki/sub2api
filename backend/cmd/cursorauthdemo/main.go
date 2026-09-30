// Command cursorauthdemo drives the Cursor deep-control login flow end to
// end: prints the browser login URL, polls until Cursor releases tokens,
// saves them locally, and verifies the access token with AvailableModels.
// Local experiment tool — not registered in wire.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "check-refresh" {
		checkRefresh()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "check-deep" {
		deepCheck()
		return
	}
	ctx := context.Background()
	proxyURL := os.Getenv("CURSOR_PROXY_URL")

	params, err := cursor.GenerateAuthParams()
	if err != nil {
		fatal("generate auth params: %v", err)
	}

	fmt.Println("=== 打开下面的链接，在浏览器里登录 Cursor ===")
	fmt.Println("LOGIN_URL=" + params.LoginURL)
	fmt.Println("UUID=" + params.UUID)
	_ = os.WriteFile("/tmp/cursor-login-url.txt", []byte(params.LoginURL+"\n"), 0o600)

	pollClient, err := cursor.UnaryHTTPClient(proxyURL)
	if err != nil {
		fatal("build poll client: %v", err)
	}

	var session *cursor.AuthSession
	for attempt := 1; attempt <= 200; attempt++ {
		time.Sleep(3 * time.Second)
		s, err := cursor.PollAuthSession(ctx, pollClient, params.UUID, params.Verifier)
		if err != nil {
			if errors.Is(err, cursor.ErrAuthPending) {
				fmt.Printf("attempt %3d: pending\n", attempt)
				continue
			}
			fmt.Printf("attempt %3d: error: %v\n", attempt, err)
			continue
		}
		session = s
		fmt.Printf("attempt %3d: LOGIN_COMPLETE\n", attempt)
		break
	}
	if session == nil {
		fatal("%s", "timed out waiting for login")
	}

	creds := map[string]any{
		"access_token":  session.AccessToken,
		"refresh_token": session.RefreshToken,
		"token_kind":    cursor.TokenKindDeepControl,
	}
	if expiry := cursor.AccessTokenExpiry(session.AccessToken); expiry != nil {
		creds["expires_at"] = expiry.UTC().Format(time.RFC3339)
	}
	raw, _ := json.MarshalIndent(creds, "", "  ")
	_ = os.WriteFile("/tmp/cursor-oauth-credentials.json", raw, 0o600)
	fmt.Printf("access_token  (%d chars): %s...\n", len(session.AccessToken), mask(session.AccessToken))
	fmt.Printf("refresh_token (%d chars): %s...\n", len(session.RefreshToken), mask(session.RefreshToken))
	fmt.Printf("expires_at: %v\n", creds["expires_at"])
	fmt.Println("credentials saved to /tmp/cursor-oauth-credentials.json")

	// Verify the token actually works for API calls (CLI profile: no machine ids).
	fmt.Println("=== verifying with AvailableModels (CLI profile, no machine ids) ===")
	modelsClient := cursor.NewClient(cursor.Credentials{AccessToken: session.AccessToken})
	modelsClient.ProxyURL = proxyURL
	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	models, err := modelsClient.AvailableModels(verifyCtx)
	if err != nil {
		fmt.Printf("AVAILABLE_MODELS_FAILED: %v\n", err)
		fmt.Println("AUTH_DONE (token issued, model list call failed — see error above)")
		return
	}
	names := make([]string, 0, 5)
	for _, m := range models {
		if len(names) < 5 {
			names = append(names, m.Name)
		}
	}
	fmt.Printf("AVAILABLE_MODELS_OK count=%d sample=%v\n", len(models), names)
	fmt.Println("AUTH_DONE")
}

func mask(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}

func fatal(format string, args ...any) {
	fmt.Printf("FATAL: "+format+"\n", args...)
	os.Exit(1)
}
