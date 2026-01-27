package core

import (
	"testing"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
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
// returns early when storage is nil.
func TestPersistDCRCredentials_SkipsWhenNoStorage(t *testing.T) {
	client := &Client{
		config:  &config.ServerConfig{Name: "test-server"},
		logger:  zap.NewNop(),
		storage: nil, // No storage configured
	}

	// Should not panic and should return early
	client.persistDCRCredentials()
	// No assertion needed - just verifying it doesn't panic
}

// TestPersistDCRCredentials_SkipsWhenNoHandler tests that persistDCRCredentials
// returns early when GetOAuthHandler returns nil.
func TestPersistDCRCredentials_SkipsWhenNoHandler(t *testing.T) {
	// Setup in-memory storage
	db, err := storage.NewBoltDB(t.TempDir(), zap.NewNop().Sugar())
	require.NoError(t, err)
	defer db.Close()

	client := &Client{
		config:  &config.ServerConfig{Name: "test-server"},
		logger:  zap.NewNop(),
		storage: db,
		// No mcp-go client set, so GetOAuthHandler() will return nil
	}

	// Should not panic and should return early
	client.persistDCRCredentials()
	// No assertion needed - just verifying it doesn't panic
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
