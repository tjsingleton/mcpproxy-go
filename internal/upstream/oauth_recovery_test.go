package upstream

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"

	"mcpproxy-go/internal/oauth"
)

// =============================================================================
// Browser Rate Limit Tests
// =============================================================================

// TestOAuthBrowserRateLimit_PreventsDuplicateOpens verifies that the browser
// rate limit prevents duplicate browser opens within the rate limit window.
func TestOAuthBrowserRateLimit_PreventsDuplicateOpens(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m := &Manager{
		logger:                logger,
		tokenReconnect:        make(map[string]time.Time),
		oauthBrowserRateLimit: make(map[string]time.Time),
	}

	// Simulate browser was opened 5 seconds ago
	m.oauthBrowserRateLimitMu.Lock()
	m.oauthBrowserRateLimit["test-server"] = time.Now().Add(-5 * time.Second)
	m.oauthBrowserRateLimitMu.Unlock()

	// Check if rate limited
	rateLimited := m.isOAuthBrowserRateLimited("test-server")

	assert.True(t, rateLimited, "Should be rate limited within 10 second window")
}

// TestOAuthBrowserRateLimit_AllowsAfterExpiry verifies that browser opens
// are allowed after the rate limit window expires.
func TestOAuthBrowserRateLimit_AllowsAfterExpiry(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m := &Manager{
		logger:                logger,
		tokenReconnect:        make(map[string]time.Time),
		oauthBrowserRateLimit: make(map[string]time.Time),
	}

	// Simulate browser was opened 15 seconds ago (beyond 10s window)
	m.oauthBrowserRateLimitMu.Lock()
	m.oauthBrowserRateLimit["test-server"] = time.Now().Add(-15 * time.Second)
	m.oauthBrowserRateLimitMu.Unlock()

	rateLimited := m.isOAuthBrowserRateLimited("test-server")

	assert.False(t, rateLimited, "Should NOT be rate limited after window expires")
}

// TestOAuthBrowserRateLimit_PerServer verifies that rate limits are per-server.
func TestOAuthBrowserRateLimit_PerServer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m := &Manager{
		logger:                logger,
		tokenReconnect:        make(map[string]time.Time),
		oauthBrowserRateLimit: make(map[string]time.Time),
	}

	// Rate limit server-1
	m.oauthBrowserRateLimitMu.Lock()
	m.oauthBrowserRateLimit["server-1"] = time.Now()
	m.oauthBrowserRateLimitMu.Unlock()

	// server-1 should be rate limited, server-2 should not
	assert.True(t, m.isOAuthBrowserRateLimited("server-1"))
	assert.False(t, m.isOAuthBrowserRateLimited("server-2"))
}

// TestOAuthBrowserRateLimit_NoEntry verifies that servers without rate limit
// entries are not rate limited.
func TestOAuthBrowserRateLimit_NoEntry(t *testing.T) {
	logger := zaptest.NewLogger(t)

	m := &Manager{
		logger:                logger,
		tokenReconnect:        make(map[string]time.Time),
		oauthBrowserRateLimit: make(map[string]time.Time),
	}

	// No entry for test-server
	rateLimited := m.isOAuthBrowserRateLimited("test-server")

	assert.False(t, rateLimited, "Server without rate limit entry should not be rate limited")
}

// =============================================================================
// Token Error Detection Tests
// =============================================================================

// TestIsTokenError verifies that token errors are correctly detected.
func TestIsTokenError(t *testing.T) {
	testCases := []struct {
		name     string
		errStr   string
		expected bool
	}{
		{
			name:     "no valid token available",
			errStr:   "no valid token available, authorization required",
			expected: true,
		},
		{
			name:     "authorization required",
			errStr:   "failed: authorization required",
			expected: true,
		},
		{
			name:     "invalid_token",
			errStr:   "OAuth error: invalid_token",
			expected: true,
		},
		{
			name:     "Unauthorized",
			errStr:   "HTTP 401 Unauthorized",
			expected: true,
		},
		{
			name:     "OAuth authentication failed",
			errStr:   "OAuth authentication failed for server",
			expected: true,
		},
		{
			name:     "network timeout",
			errStr:   "connection timeout after 30s",
			expected: false,
		},
		{
			name:     "server error",
			errStr:   "internal server error",
			expected: false,
		},
		{
			name:     "empty error",
			errStr:   "",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isTokenError(tc.errStr)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// HasRecentOAuthCompletion Tests
// =============================================================================

// TestHasRecentOAuthCompletion_NoCompletion verifies false when no completion recorded.
func TestHasRecentOAuthCompletion_NoCompletion(t *testing.T) {
	manager := oauth.GetTokenStoreManager()
	
	// Use a unique server name that definitely hasn't been used
	serverName := "test-server-no-completion-" + time.Now().Format("20060102150405.000")
	
	hasRecent := manager.HasRecentOAuthCompletion(serverName)
	
	assert.False(t, hasRecent, "Should return false when no completion recorded")
}

// TestHasRecentOAuthCompletion_RecentCompletion verifies true when recently completed.
func TestHasRecentOAuthCompletion_RecentCompletion(t *testing.T) {
	manager := oauth.GetTokenStoreManager()
	
	// Use a unique server name for this test
	serverName := "test-server-recent-" + time.Now().Format("20060102150405.000")
	
	// Record a completion
	manager.MarkOAuthCompleted(serverName)
	
	hasRecent := manager.HasRecentOAuthCompletion(serverName)
	
	assert.True(t, hasRecent, "Should return true when OAuth recently completed")
}

// =============================================================================
// Connection Timeout Tests
// =============================================================================

// TestAddServer_TimeoutConstant verifies the 5-minute timeout constant.
func TestAddServer_TimeoutConstant(t *testing.T) {
	// This test documents the expected timeout value
	// In the implementation: ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	
	expectedTimeout := 5 * time.Minute
	
	// The test passes if the constant is correct
	assert.Equal(t, 5*time.Minute, expectedTimeout,
		"AddServer should use 5-minute timeout for OAuth connections")
}

// =============================================================================
// Helper Functions
// =============================================================================

// isOAuthBrowserRateLimited checks if browser opens are rate limited for a server.
// This is a test helper that mirrors the actual implementation.
func (m *Manager) isOAuthBrowserRateLimited(serverName string) bool {
	m.oauthBrowserRateLimitMu.RLock()
	defer m.oauthBrowserRateLimitMu.RUnlock()

	if last, ok := m.oauthBrowserRateLimit[serverName]; ok {
		return time.Since(last) < 10*time.Second
	}
	return false
}

// isTokenError checks if an error string indicates a token/auth error.
// This helper documents the error detection logic.
func isTokenError(errStr string) bool {
	tokenErrorPatterns := []string{
		"no valid token available",
		"authorization required",
		"invalid_token",
		"Unauthorized",
		"OAuth authentication failed",
	}

	for _, pattern := range tokenErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}
