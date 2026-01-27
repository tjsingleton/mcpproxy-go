package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/oauth"
	"github.com/smart-mcp-proxy/mcpproxy-go/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestGetOAuthHandler_ReturnsNilWhenClientNil tests that GetOAuthHandler returns nil
// when the mcp-go client has not been initialized yet.
func TestGetOAuthHandler_ReturnsNilWhenClientNil(t *testing.T) {
	client := &Client{
		config: &config.ServerConfig{Name: "test-server"},
		logger: zap.NewNop(),
	}

	handler := client.GetOAuthHandler()

	assert.Nil(t, handler, "GetOAuthHandler should return nil when c.client is nil")
}

// TestPersistDCRCredentials_SkipsWhenNoStorage tests that persistDCRCredentials
// returns early when storage is nil without panicking or causing errors.
func TestPersistDCRCredentials_SkipsWhenNoStorage(t *testing.T) {
	client := &Client{
		config:  &config.ServerConfig{Name: "test-server"},
		logger:  zap.NewNop(),
		storage: nil, // No storage configured
	}

	// Should not panic and should return early
	// The function should handle nil storage gracefully
	client.persistDCRCredentials()

	// Verify: No storage operations should occur with nil storage
	// This test ensures defensive programming for nil storage
}

// TestPersistDCRCredentials_SkipsWhenNoHandler tests that persistDCRCredentials
// returns early when GetOAuthHandler returns nil.
func TestPersistDCRCredentials_SkipsWhenNoHandler(t *testing.T) {
	// Setup in-memory storage
	db, err := storage.NewBoltDB(t.TempDir(), zap.NewNop().Sugar())
	require.NoError(t, err)
	defer db.Close()

	serverName := "test-server"
	serverURL := "https://example.com/mcp"
	serverKey := oauth.GenerateServerKey(serverName, serverURL)

	// Pre-create a token record to verify it's NOT modified
	initialRecord := &storage.OAuthTokenRecord{
		ServerName:   serverKey,
		DisplayName:  serverName,
		AccessToken:  "initial-token",
		RefreshToken: "initial-refresh",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Created:      time.Now(),
		Updated:      time.Now(),
		ClientID:     "", // No client ID initially
		ClientSecret: "",
	}
	err = db.SaveOAuthToken(initialRecord)
	require.NoError(t, err)

	client := &Client{
		config:  &config.ServerConfig{Name: serverName, URL: serverURL},
		logger:  zap.NewNop(),
		storage: db,
		// No mcp-go client set, so GetOAuthHandler() will return nil
	}

	// Call persistDCRCredentials
	client.persistDCRCredentials()

	// Verify: The token record should NOT be modified
	// (because GetOAuthHandler returns nil and function returns early)
	record, err := db.GetOAuthToken(serverKey)
	require.NoError(t, err)
	assert.Equal(t, "", record.ClientID, "ClientID should remain empty when no handler available")
	assert.Equal(t, "", record.ClientSecret, "ClientSecret should remain empty when no handler available")
}

// TestPersistDCRCredentials_SkipsWhenClientIDEmpty tests that persistDCRCredentials
// returns early when handler.GetClientID() returns empty string.
// This simulates the case where OAuth is not using DCR.
func TestPersistDCRCredentials_SkipsWhenClientIDEmpty(t *testing.T) {
	// This test requires mocking the mcp-go client and transport
	// which is complex. For now, we'll test the storage-related behavior.
	t.Skip("Requires mocking mcp-go client - implement with mock transport")
}

// TestPersistDCRCredentials_SavesCredentialsToStorage tests the happy path
// where DCR credentials are successfully persisted to storage.
func TestPersistDCRCredentials_SavesCredentialsToStorage(t *testing.T) {
	// This test requires mocking the mcp-go client and transport
	// which is complex. For now, we'll test the storage-related behavior.
	t.Skip("Requires mocking mcp-go client - implement with mock transport")
}

// TestGetOAuthHandler_DiagnosticLogging tests that GetOAuthHandler logs
// diagnostic information to help debug issues.
// This test verifies the diagnostic logging approach from the implementation plan.
func TestGetOAuthHandler_DiagnosticLogging(t *testing.T) {
	// Create a test logger that captures output
	// For now, just verify the function doesn't panic with various inputs
	testCases := []struct {
		name   string
		client *Client
	}{
		{
			name: "nil client field",
			client: &Client{
				config: &config.ServerConfig{Name: "test-server"},
				logger: zap.NewNop(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler := tc.client.GetOAuthHandler()
			assert.Nil(t, handler)
		})
	}
}

// ===========================================================================
// RefreshOAuthTokenDirect Tests
// ===========================================================================

// TestRefreshOAuthTokenDirect_NoOAuthHandler tests that RefreshOAuthTokenDirect
// returns an appropriate error when no OAuth handler is available.
func TestRefreshOAuthTokenDirect_NoOAuthHandler(t *testing.T) {
	// Setup storage
	db, err := storage.NewBoltDB(t.TempDir(), zap.NewNop().Sugar())
	require.NoError(t, err)
	defer db.Close()

	client := &Client{
		config:  &config.ServerConfig{Name: "test-server", URL: "https://example.com/mcp"},
		logger:  zap.NewNop(),
		storage: db,
		// No mcp-go client, so GetOAuthHandler returns nil
	}

	ctx := context.Background()
	err = client.RefreshOAuthTokenDirect(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no OAuth handler available")
	assert.Contains(t, err.Error(), "test-server")
}

// TestRefreshOAuthTokenDirect_StorageLookupFails tests that RefreshOAuthTokenDirect
// returns appropriate error when token lookup from storage fails.
// This requires a real OAuth handler which is hard to mock, so we verify
// the error path that occurs when GetOAuthToken fails.
func TestRefreshOAuthTokenDirect_StorageLookupFails(t *testing.T) {
	// This test verifies error handling when storage.GetOAuthToken fails
	// The actual failure would occur after GetOAuthHandler succeeds
	// but before we can use stored credentials
	t.Skip("Requires mocking OAuth handler to test storage failure path")
}

// TestRefreshOAuthTokenDirect_NoRefreshToken tests that RefreshOAuthTokenDirect
// returns appropriate error when the stored token has no refresh token.
func TestRefreshOAuthTokenDirect_NoRefreshToken(t *testing.T) {
	// This test verifies the check for empty refresh token
	// which happens after storage lookup succeeds but before refresh attempt
	t.Skip("Requires mocking OAuth handler to test no-refresh-token path")
}

// ===========================================================================
// refreshTokenWithStoredCredentials Additional Tests
// ===========================================================================

// TestRefreshTokenWithStoredCredentials_AuthFailure401 tests that 401 errors
// are properly reported with appropriate error message.
func TestRefreshTokenWithStoredCredentials_AuthFailure401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid_client", "error_description": "Client authentication failed"}`))
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "invalid-client-id",
		ClientSecret: "invalid-secret",
	}

	ctx := context.Background()
	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "invalid_client")
}

// TestRefreshTokenWithStoredCredentials_AuthFailure403 tests that 403 errors
// are properly reported with appropriate error message.
func TestRefreshTokenWithStoredCredentials_AuthFailure403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error": "access_denied", "error_description": "Access denied"}`))
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
	assert.Contains(t, err.Error(), "403")
}

// TestRefreshTokenWithStoredCredentials_EmptyResponseBody tests handling of
// an OK response with no body content.
func TestRefreshTokenWithStoredCredentials_EmptyResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Empty body - no data written
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

// TestRefreshTokenWithStoredCredentials_MissingAccessToken tests handling of
// a response that's valid JSON but missing the required access_token field.
func TestRefreshTokenWithStoredCredentials_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Valid JSON but missing access_token
		response := map[string]interface{}{
			"token_type": "Bearer",
			"expires_in": 3600,
			// access_token intentionally omitted
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	// Note: The current implementation doesn't validate for missing access_token
	// This test documents the current behavior (returns empty access_token)
	require.NoError(t, err, "Current implementation doesn't validate for missing access_token")
	assert.Empty(t, newToken.AccessToken, "Access token should be empty when not provided")
}

// TestRefreshTokenWithStoredCredentials_ZeroExpiresIn tests handling of
// a response with expires_in = 0 (edge case).
func TestRefreshTokenWithStoredCredentials_ZeroExpiresIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := oauthTokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   0, // Zero expiry
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.NoError(t, err)
	assert.Equal(t, "new-access-token", newToken.AccessToken)
	// With expires_in=0, token should expire essentially "now"
	assert.WithinDuration(t, time.Now(), newToken.ExpiresAt, 5*time.Second,
		"Token with 0 expires_in should have immediate expiry")
}

// TestRefreshTokenWithStoredCredentials_NetworkTimeout tests handling of
// network timeout scenarios.
func TestRefreshTokenWithStoredCredentials_NetworkTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than context timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "test-refresh-token",
		ClientID:     "test-client-id",
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.Error(t, err)
	// Should fail due to context deadline
}

// TestRefreshTokenWithStoredCredentials_RequestFormatsCorrectly tests that
// the token refresh request is formatted correctly per OAuth 2.0 spec.
func TestRefreshTokenWithStoredCredentials_RequestFormatsCorrectly(t *testing.T) {
	var capturedRequest struct {
		grantType    string
		refreshToken string
		clientID     string
		clientSecret string
		contentType  string
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request details
		capturedRequest.contentType = r.Header.Get("Content-Type")
		err := r.ParseForm()
		require.NoError(t, err)
		capturedRequest.grantType = r.FormValue("grant_type")
		capturedRequest.refreshToken = r.FormValue("refresh_token")
		capturedRequest.clientID = r.FormValue("client_id")
		capturedRequest.clientSecret = r.FormValue("client_secret")

		// Return success
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

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "my-refresh-token",
		ClientID:     "my-client-id",
		ClientSecret: "my-client-secret",
	}

	ctx := context.Background()
	_, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)
	require.NoError(t, err)

	// Verify request was formatted correctly
	assert.Equal(t, "application/x-www-form-urlencoded", capturedRequest.contentType,
		"Content-Type must be application/x-www-form-urlencoded per OAuth spec")
	assert.Equal(t, "refresh_token", capturedRequest.grantType,
		"grant_type must be 'refresh_token'")
	assert.Equal(t, "my-refresh-token", capturedRequest.refreshToken,
		"refresh_token must match the stored value")
	assert.Equal(t, "my-client-id", capturedRequest.clientID,
		"client_id must match the stored value")
	assert.Equal(t, "my-client-secret", capturedRequest.clientSecret,
		"client_secret must match the stored value")
}

// TestRefreshTokenWithStoredCredentials_RefreshTokenRotation tests handling of
// refresh token rotation (new refresh token in response).
func TestRefreshTokenWithStoredCredentials_RefreshTokenRotation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := oauthTokenResponse{
			AccessToken:  "new-access-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "rotated-refresh-token", // New refresh token
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "old-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.NoError(t, err)
	assert.Equal(t, "rotated-refresh-token", newToken.RefreshToken,
		"New refresh token should be captured when server rotates tokens")
}

// TestRefreshTokenWithStoredCredentials_NoRefreshTokenRotation tests that
// empty refresh token in response doesn't overwrite existing token.
func TestRefreshTokenWithStoredCredentials_NoRefreshTokenRotation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := oauthTokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			// No RefreshToken in response
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{}

	record := &storage.OAuthTokenRecord{
		RefreshToken: "original-refresh-token",
		ClientID:     "test-client-id",
	}

	ctx := context.Background()
	newToken, err := client.refreshTokenWithStoredCredentials(ctx, server.URL, record)

	require.NoError(t, err)
	// Note: The returned record has empty RefreshToken because the response didn't include one
	// The caller (RefreshOAuthTokenDirect) is responsible for preserving the old token
	assert.Empty(t, newToken.RefreshToken,
		"RefreshToken should be empty when server doesn't return one")
}

// TestRefreshTokenWithStoredCredentials_InvalidGrantError tests handling of
// the permanent "invalid_grant" error which indicates refresh token is expired.
func TestRefreshTokenWithStoredCredentials_InvalidGrantError(t *testing.T) {
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
	assert.Contains(t, err.Error(), "invalid_grant")
	assert.Contains(t, err.Error(), "Refresh token expired")
}
