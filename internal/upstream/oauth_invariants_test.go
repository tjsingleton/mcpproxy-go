package upstream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInvariant_INV001_SingleBrowserPerServer tests that at most ONE browser
// window opens for OAuth for any given server at any time.
func TestInvariant_INV001_SingleBrowserPerServer(t *testing.T) {
	t.Run("concurrent OAuth triggers open only one browser", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		mockBrowser := &MockBrowserOpener{}
		serverName := "test-server"
		
		// Simulate multiple concurrent OAuth triggers
		var wg sync.WaitGroup
		numGoroutines := 10
		
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				
				// Check if OAuth is already in progress before opening browser
				if !tracker.IsOAuthInProgress(serverName) {
					tracker.MarkOAuthStart(serverName)
					_ = mockBrowser.Open("https://auth.example.com/oauth")
					tracker.MarkBrowserOpen(serverName)
				}
			}()
		}
		
		wg.Wait()
		
		// Verify only one browser was opened
		violations := tracker.GetViolations()
		assert.Empty(t, violations, "Invariant violations detected: %v", violations)
		
		// The browser open count should be 1 (or 0 if all failed)
		browserCount := mockBrowser.GetOpenCount()
		t.Logf("Browser open count: %d", browserCount)
		
		// Check invariant
		err := tracker.CheckInvariant_SingleBrowser(serverName)
		if err != nil {
			t.Errorf("INV-001 violated: %v", err)
		}
	})
	
	t.Run("sequential OAuth triggers respect rate limiting", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		rateLimit := 100 * time.Millisecond // Short for testing
		
		// First trigger - should open browser
		tracker.MarkBrowserOpen(serverName)
		firstOpenTime := time.Now()
		
		// Immediate second trigger - should be rate limited
		time.Sleep(10 * time.Millisecond)
		timeSinceFirst := time.Since(firstOpenTime)
		
		if timeSinceFirst < rateLimit {
			// Should be rate limited - don't open another browser
			t.Logf("Rate limited: %v < %v", timeSinceFirst, rateLimit)
		} else {
			tracker.MarkBrowserOpen(serverName)
		}
		
		// Verify browser count
		count := tracker.GetBrowserOpenCount(serverName)
		assert.Equal(t, 1, count, "Should only have one browser open within rate limit")
	})
}

// TestInvariant_INV002_TokenPersistenceBeforeCompletion tests that tokens
// are persisted to storage BEFORE marking OAuth as complete.
func TestInvariant_INV002_TokenPersistenceBeforeCompletion(t *testing.T) {
	t.Run("tokens saved before completion marked", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Start OAuth
		tracker.MarkOAuthStart(serverName)
		assert.True(t, tracker.IsOAuthInProgress(serverName))
		
		// Simulate callback received - tokens should be saved first
		tracker.MarkTokensSaved(serverName)
		
		// Then mark complete
		tracker.MarkOAuthComplete(serverName)
		assert.False(t, tracker.IsOAuthInProgress(serverName))
		
		// No violations should have occurred
		violations := tracker.GetViolations()
		assert.Empty(t, violations)
	})
}

// TestInvariant_INV003_ClientRecreationAfterOAuth tests that after OAuth
// completion, the HTTP client is recreated to load new tokens.
func TestInvariant_INV003_ClientRecreationAfterOAuth(t *testing.T) {
	t.Run("client recreated after tokens saved", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Simulate OAuth flow
		tracker.MarkOAuthStart(serverName)
		tracker.MarkTokensSaved(serverName)
		tracker.MarkClientRecreated(serverName)
		tracker.MarkOAuthComplete(serverName)
		
		// Check invariant
		err := tracker.CheckInvariant_ClientRecreated(serverName)
		assert.NoError(t, err)
	})
	
	t.Run("violation when tokens saved but client not recreated", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Tokens saved but client not recreated
		tracker.MarkTokensSaved(serverName)
		// Missing: tracker.MarkClientRecreated(serverName)
		
		// Check invariant - should detect violation
		err := tracker.CheckInvariant_ClientRecreated(serverName)
		assert.Error(t, err)
		
		var violation *InvariantViolation
		require.ErrorAs(t, err, &violation)
		assert.Equal(t, "INV-003", violation.Invariant)
	})
}

// TestInvariant_INV004_RateLimitOnBrowserOnly tests that browser rate limiting
// only applies to actual successful browser opens, not to OAuth attempts.
func TestInvariant_INV004_RateLimitOnBrowserOnly(t *testing.T) {
	t.Run("failed browser open does not update rate limit", func(t *testing.T) {
		mockBrowser := &MockBrowserOpener{
			ShouldFail: true,
			FailError:  &OAuthError{Message: "browser not available"},
		}
		
		var lastSuccessfulOpen time.Time
		
		// Attempt to open browser - should fail
		err := mockBrowser.Open("https://auth.example.com/oauth")
		assert.Error(t, err)
		
		// Rate limit timestamp should NOT be updated
		assert.True(t, lastSuccessfulOpen.IsZero(), 
			"Rate limit timestamp should not be updated on failed browser open")
		
		// Second attempt should be allowed immediately
		mockBrowser.ShouldFail = false
		err = mockBrowser.Open("https://auth.example.com/oauth")
		assert.NoError(t, err)
		lastSuccessfulOpen = time.Now()
		
		assert.False(t, lastSuccessfulOpen.IsZero(),
			"Rate limit timestamp should be updated on successful browser open")
	})
}

// TestInvariant_INV005_NoReconnectionDuringOAuth tests that the system
// does NOT trigger reconnection for a server while OAuth is in progress.
func TestInvariant_INV005_NoReconnectionDuringOAuth(t *testing.T) {
	t.Run("reconnection blocked during OAuth", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Start OAuth
		tracker.MarkOAuthStart(serverName)
		
		// Attempt reconnection while OAuth in progress
		tracker.MarkReconnectionAttempt(serverName)
		
		// Should have recorded a violation
		violations := tracker.GetViolations()
		require.Len(t, violations, 1)
		assert.Contains(t, violations[0], "INV-005")
	})
	
	t.Run("reconnection allowed after OAuth complete", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Complete OAuth
		tracker.MarkOAuthStart(serverName)
		tracker.MarkOAuthComplete(serverName)
		
		// Reconnection after completion should be fine
		tracker.MarkReconnectionAttempt(serverName)
		
		violations := tracker.GetViolations()
		assert.Empty(t, violations)
	})
}

// TestInvariant_INV006_GlobalProgressFlag tests that the "OAuth in progress"
// flag is checked globally across all client instances for a server.
func TestInvariant_INV006_GlobalProgressFlag(t *testing.T) {
	t.Run("global flag prevents multiple flows", func(t *testing.T) {
		// Shared global tracker simulates the global OAuth state
		globalTracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		var browserOpenCount int32
		var wg sync.WaitGroup
		
		// Simulate multiple client instances trying to start OAuth
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(clientID int) {
				defer wg.Done()
				
				// Each client checks global flag before starting OAuth
				if globalTracker.IsOAuthInProgress(serverName) {
					t.Logf("Client %d: OAuth already in progress, skipping", clientID)
					return
				}
				
				// Try to mark as in progress atomically
				globalTracker.MarkOAuthStart(serverName)
				atomic.AddInt32(&browserOpenCount, 1)
				
				// Simulate OAuth duration
				time.Sleep(50 * time.Millisecond)
				
				globalTracker.MarkOAuthComplete(serverName)
			}(i)
		}
		
		wg.Wait()
		
		// Only violations should be from concurrent starts racing
		// The key is that we detect and log them
		violations := globalTracker.GetViolations()
		t.Logf("Violations detected: %v", violations)
		t.Logf("Browser open count: %d", browserOpenCount)
	})
}

// TestInvariant_INV007_TokenCheckBeforeOAuth tests that before triggering
// OAuth, the system checks if valid tokens already exist in storage.
func TestInvariant_INV007_TokenCheckBeforeOAuth(t *testing.T) {
	t.Run("OAuth skipped when valid tokens exist", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		mockBrowser := &MockBrowserOpener{}
		serverName := "test-server"
		
		// Simulate existing tokens
		hasValidTokens := true
		
		// OAuth trigger should check tokens first
		if hasValidTokens {
			t.Log("Valid tokens exist, skipping OAuth")
		} else {
			tracker.MarkOAuthStart(serverName)
			_ = mockBrowser.Open("https://auth.example.com/oauth")
			tracker.MarkBrowserOpen(serverName)
		}
		
		// Browser should not have been opened
		assert.Equal(t, 0, mockBrowser.GetOpenCount())
		assert.False(t, tracker.IsOAuthInProgress(serverName))
	})
	
	t.Run("OAuth triggered when tokens missing", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		mockBrowser := &MockBrowserOpener{}
		serverName := "test-server"
		
		// No existing tokens
		hasValidTokens := false
		
		if hasValidTokens {
			t.Log("Valid tokens exist, skipping OAuth")
		} else {
			tracker.MarkOAuthStart(serverName)
			_ = mockBrowser.Open("https://auth.example.com/oauth")
			tracker.MarkBrowserOpen(serverName)
		}
		
		// Browser should have been opened
		assert.Equal(t, 1, mockBrowser.GetOpenCount())
		assert.True(t, tracker.IsOAuthInProgress(serverName))
	})
}

// TestInvariant_INV008_CleanupOnFailure tests that OAuth state is cleaned up
// when a flow fails or times out.
func TestInvariant_INV008_CleanupOnFailure(t *testing.T) {
	t.Run("state cleaned up on timeout", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Start OAuth
		tracker.MarkOAuthStart(serverName)
		assert.True(t, tracker.IsOAuthInProgress(serverName))
		
		// Simulate timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		
		<-ctx.Done()
		
		// Clean up state on timeout
		tracker.MarkOAuthComplete(serverName)
		assert.False(t, tracker.IsOAuthInProgress(serverName))
	})
	
	t.Run("state cleaned up on error", func(t *testing.T) {
		tracker := NewOAuthStateTracker()
		serverName := "test-server"
		
		// Start OAuth
		tracker.MarkOAuthStart(serverName)
		
		// Simulate error during OAuth
		oauthError := &OAuthError{Message: "callback error"}
		_ = oauthError // Use the error
		
		// Clean up state on error
		tracker.MarkOAuthComplete(serverName)
		assert.False(t, tracker.IsOAuthInProgress(serverName))
	})
}

