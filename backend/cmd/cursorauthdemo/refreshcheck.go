package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	_ "modernc.org/sqlite"
)

// checkRefresh reads the local Cursor install's refresh token and exercises
// both refresh endpoints. Tokens are never printed.
func checkRefresh() {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Application Support/Cursor/User/globalStorage/state.vscdb"
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		fmt.Printf("REFRESH_SKIP: open state.vscdb: %v\n", err)
		return
	}
	defer db.Close()

	var refreshToken string
	if err := db.QueryRow(`SELECT value FROM ItemTable WHERE key = 'cursorAuth/refreshToken'`).Scan(&refreshToken); err != nil {
		fmt.Printf("REFRESH_SKIP: read refreshToken: %v\n", err)
		return
	}
	if refreshToken == "" {
		fmt.Println("REFRESH_SKIP: empty refresh token")
		return
	}
	fmt.Printf("refresh token loaded (%d chars, masked)\n", len(refreshToken))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := cursor.UnaryHTTPClient("")
	if err != nil {
		fmt.Printf("REFRESH_FATAL: %v\n", err)
		return
	}

	// Path A: /oauth/token (what pasted-session accounts use today).
	fmt.Println("=== endpoint A: POST /oauth/token (session path) ===")
	if result, err := cursor.RefreshSession(ctx, client, refreshToken); err != nil {
		fmt.Printf("A_FAILED: %v\n", err)
	} else {
		fmt.Printf("A_OK access(%d chars): %s…  refresh-rotated: %v\n", len(result.AccessToken), mask(result.AccessToken), result.RefreshToken != "")
	}

	// Path B: /auth/exchange_user_api_key (deep-control path).
	fmt.Println("=== endpoint B: POST /auth/exchange_user_api_key (deep-control path) ===")
	if result, err := cursor.RefreshViaUserAPIKey(ctx, client, refreshToken); err != nil {
		fmt.Printf("B_FAILED: %v\n", err)
	} else {
		fmt.Printf("B_OK access(%d chars): %s…  refresh-rotated: %v\n", len(result.AccessToken), mask(result.AccessToken), result.RefreshToken != "")
	}
	fmt.Println("REFRESH_CHECK_DONE")
}
