package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func TestCursorOAuthStartAuthGeneratesPKCEParams(t *testing.T) {
	svc := NewCursorOAuthService(nil)

	result, err := svc.StartAuth(context.Background())
	require.NoError(t, err)
	require.Contains(t, result.URL, "loginDeepControl")
	require.Contains(t, result.URL, "uuid="+result.UUID)
	require.Contains(t, result.URL, "challenge=")
	require.NotEqual(t, result.Verifier, "")
}

func TestCursorOAuthPollAuthPendingAndDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uuid") == "pending" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  "at-1",
			"refreshToken": "rt-1",
		})
	}))
	defer srv.Close()

	restore := cursor.SetAuthEndpointsForTest(srv.URL, srv.URL)
	t.Cleanup(restore)

	svc := NewCursorOAuthService(nil)
	svc.SetHTTPClientFactoryForTest(func(string) (*http.Client, error) { return srv.Client(), nil })

	pending, err := svc.PollAuth(context.Background(), "pending", "v", nil)
	require.NoError(t, err)
	require.False(t, pending.Done)

	done, err := svc.PollAuth(context.Background(), "ready", "v", nil)
	require.NoError(t, err)
	require.True(t, done.Done)
	require.Equal(t, "at-1", done.Credentials["access_token"])
	require.Equal(t, "rt-1", done.Credentials["refresh_token"])
	require.Equal(t, cursor.TokenKindDeepControl, done.Credentials["token_kind"])
}

func TestCursorOAuthPollAuthRejectsMissingParams(t *testing.T) {
	svc := NewCursorOAuthService(nil)
	_, err := svc.PollAuth(context.Background(), "", "v", nil)
	require.ErrorContains(t, err, "uuid and verifier")
}

func TestCursorTokenRefresherRoutesDeepControlToExchangeEndpoint(t *testing.T) {
	var exchangeCalls, oauthTokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cursor.EndpointUserAPIKey:
			exchangeCalls++
			_ = json.NewEncoder(w).Encode(map[string]string{
				"accessToken":  "at-exchanged",
				"refreshToken": "rt-rotated",
			})
		case "/oauth/token":
			oauthTokenCalls++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	restore := cursor.SetAuthEndpointsForTest(srv.URL, srv.URL)
	t.Cleanup(restore)
	cursor.SetOAuthTokenURLForTest(srv.URL + "/oauth/token")
	t.Cleanup(func() { cursor.SetOAuthTokenURLForTest("") })

	account := &Account{
		ID:       51,
		Platform: PlatformCursor,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "stale",
			"refresh_token": "rt",
			"token_kind":    cursor.TokenKindDeepControl,
		},
	}

	refresher := NewCursorTokenRefresher()
	refresher.httpClient = srv.Client()
	creds, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, exchangeCalls)
	require.Zero(t, oauthTokenCalls, "deep-control tokens must not hit /oauth/token")
	require.Equal(t, "at-exchanged", creds["access_token"])
	require.Equal(t, cursor.TokenKindDeepControl, creds["token_kind"], "token_kind must survive rotation")
}

func TestCursorTokenRefresherSessionFallsBackToExchange(t *testing.T) {
	var oauthTokenCalls, exchangeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			oauthTokenCalls++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		case cursor.EndpointUserAPIKey:
			exchangeCalls++
			_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "at-fallback"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	restore := cursor.SetAuthEndpointsForTest(srv.URL, srv.URL)
	t.Cleanup(restore)
	cursor.SetOAuthTokenURLForTest(srv.URL + "/oauth/token")
	t.Cleanup(func() { cursor.SetOAuthTokenURLForTest("") })

	account := &Account{
		ID:       52,
		Platform: PlatformCursor,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "stale",
			"refresh_token": "deep-control-token-without-kind",
		},
	}

	refresher := NewCursorTokenRefresher()
	refresher.httpClient = srv.Client()
	creds, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, oauthTokenCalls)
	require.Equal(t, 1, exchangeCalls)
	require.Equal(t, "at-fallback", creds["access_token"])
}
