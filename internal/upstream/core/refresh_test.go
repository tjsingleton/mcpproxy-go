package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshTokenWithStoredCredentials_Success(t *testing.T) {
	// Setup mock OAuth server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		err := r.ParseForm()
		require.NoError(t, err)

		// Verify request parameters
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
		assert.Equal(t, "test-refresh-token", r.FormValue("refresh_token"))
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))
		assert.Equal(t, "test-client-secret", r.FormValue("client_secret"))

		// Return successful token response
		response := oauthTokenResponse{
			AccessToken:  "new-access-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "new-refresh-token",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client with minimal setup for testing
	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	// Execute
	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	// Verify
	require.NoError(t, err)
	assert.Equal(t, "new-access-token", newToken.AccessToken)
	assert.Equal(t, "new-refresh-token", newToken.RefreshToken)
	assert.Equal(t, "Bearer", newToken.TokenType)
	assert.WithinDuration(t, time.Now().Add(3600*time.Second), newToken.ExpiresAt, 5*time.Second)
}

func TestRefreshTokenWithStoredCredentials_NoClientSecret(t *testing.T) {
	// Setup mock OAuth server - public client (no secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		require.NoError(t, err)

		// Verify client_secret is NOT sent for public clients
		assert.Equal(t, "", r.FormValue("client_secret"))
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))

		response := oauthTokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
		ClientSecret: "", // Public client - no secret
	}

	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.NoError(t, err)
	assert.Equal(t, "new-access-token", newToken.AccessToken)
}

func TestRefreshTokenWithStoredCredentials_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant", "error_description": "Refresh token expired"}`))
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "expired-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "invalid_grant")
}

func TestRefreshTokenWithStoredCredentials_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse token response")
}

func TestRefreshTokenWithStoredCredentials_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow server
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.Error(t, err)
}
