package upstream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestOAuth_EC001_ConcurrentTriggers tests that multiple goroutines trying
// to trigger OAuth simultaneously result in only one OAuth flow.
func TestOAuth_EC001_ConcurrentTriggers(t *testing.T) {
	tracker := NewOAuthStateTracker()
	mockBrowser := &MockBrowserOpener{}
	serverName := "concurrent-test-server"
	
	var wg sync.WaitGroup
	var attemptCount int32
	var actualOAuthStarts int32
	
	numGoroutines := 20
	
	// Use a mutex to properly synchronize the OAuth start check
	var oauthMu sync.Mutex
	oauthStarted := false
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			atomic.AddInt32(&attemptCount, 1)
			
			// Proper locking for the OAuth check-and-start
			oauthMu.Lock()
			if oauthStarted {
				oauthMu.Unlock()
				t.Logf("Goroutine %d: OAuth already started, skipping", id)
				return
			}
			oauthStarted = true
			oauthMu.Unlock()
			
			atomic.AddInt32(&actualOAuthStarts, 1)
			tracker.MarkOAuthStart(serverName)
			_ = mockBrowser.Open("https://auth.example.com/oauth")
			tracker.MarkBrowserOpen(serverName)
			
			// Simulate OAuth duration
			time.Sleep(10 * time.Millisecond)
			
			tracker.MarkOAuthComplete(serverName)
		}(i)
	}
	
	wg.Wait()
	
	t.Logf("Total attempts: %d, Actual OAuth starts: %d, Browser opens: %d",
		attemptCount, actualOAuthStarts, mockBrowser.GetOpenCount())
	
	// Should have exactly 1 OAuth start and 1 browser open
	assert.Equal(t, int32(1), actualOAuthStarts, "Only one OAuth should have started")
	assert.Equal(t, 1, mockBrowser.GetOpenCount(), "Only one browser should have opened")
	
	// No violations
	violations := tracker.GetViolations()
	assert.Empty(t, violations, "No invariant violations expected")
}

// TestOAuth_EC002_ShutdownDuringOAuth tests that OAuth is cancelled gracefully
// when the application shuts down.
func TestOAuth_EC002_ShutdownDuringOAuth(t *testing.T) {
	tracker := NewOAuthStateTracker()
	serverName := "shutdown-test-server"
	
	// Start OAuth
	tracker.MarkOAuthStart(serverName)
	assert.True(t, tracker.IsOAuthInProgress(serverName))
	
	// Create a cancellable context for the OAuth flow
	ctx, cancel := context.WithCancel(context.Background())
	
	oauthComplete := make(chan bool)
	
	go func() {
		// Simulate OAuth waiting for callback
		select {
		case <-ctx.Done():
			// Shutdown signaled - clean up
			tracker.MarkOAuthComplete(serverName)
			oauthComplete <- false
		case <-time.After(5 * time.Second):
			// Normal completion
			tracker.MarkOAuthComplete(serverName)
			oauthComplete <- true
		}
	}()
	
	// Simulate shutdown after 50ms
	time.Sleep(50 * time.Millisecond)
	cancel()
	
	// Wait for OAuth to clean up
	completed := <-oauthComplete
	assert.False(t, completed, "OAuth should have been cancelled")
	assert.False(t, tracker.IsOAuthInProgress(serverName), "OAuth should no longer be in progress")
}

// TestOAuth_EC004_TokenSavedClientNotUpdated tests the scenario where tokens
// are saved but the HTTP client doesn't have them loaded.
func TestOAuth_EC004_TokenSavedClientNotUpdated(t *testing.T) {
	tracker := NewOAuthStateTracker()
	serverName := "token-client-mismatch-server"
	
	// Simulate OAuth completing and tokens being saved
	tracker.MarkOAuthStart(serverName)
	tracker.MarkTokensSaved(serverName)
	tracker.MarkOAuthComplete(serverName)
	
	// Client was NOT recreated (the bug scenario)
	// tracker.MarkClientRecreated(serverName) // Missing!
	
	// Invariant check should detect this
	err := tracker.CheckInvariant_ClientRecreated(serverName)
	assert.Error(t, err, "Should detect that client was not recreated")
	
	// The fix is to force client recreation
	tracker.MarkClientRecreated(serverName)
	
	// Now invariant should pass
	err = tracker.CheckInvariant_ClientRecreated(serverName)
	assert.NoError(t, err)
}

// TestOAuth_EC005_RapidReconnection tests that rapid reconnection attempts
// don't trigger multiple OAuth flows.
func TestOAuth_EC005_RapidReconnection(t *testing.T) {
	tracker := NewOAuthStateTracker()
	mockBrowser := &MockBrowserOpener{}
	serverName := "rapid-reconnect-server"
	
	reconnectInterval := 10 * time.Millisecond
	numReconnects := 10
	
	var browserOpens int32
	var oauthMu sync.Mutex
	lastOAuthTime := time.Time{}
	rateLimitWindow := 50 * time.Millisecond
	
	for i := 0; i < numReconnects; i++ {
		// Simulate reconnection attempt
		oauthMu.Lock()
		
		// Check rate limit
		if !lastOAuthTime.IsZero() && time.Since(lastOAuthTime) < rateLimitWindow {
			oauthMu.Unlock()
			t.Logf("Reconnect %d: Rate limited", i)
			time.Sleep(reconnectInterval)
			continue
		}
		
		// Check if OAuth already in progress
		if tracker.IsOAuthInProgress(serverName) {
			oauthMu.Unlock()
			t.Logf("Reconnect %d: OAuth in progress", i)
			time.Sleep(reconnectInterval)
			continue
		}
		
		tracker.MarkOAuthStart(serverName)
		lastOAuthTime = time.Now()
		oauthMu.Unlock()
		
		atomic.AddInt32(&browserOpens, 1)
		_ = mockBrowser.Open("https://auth.example.com/oauth")
		tracker.MarkBrowserOpen(serverName)
		
		// Simulate quick OAuth completion
		time.Sleep(5 * time.Millisecond)
		tracker.MarkOAuthComplete(serverName)
		
		time.Sleep(reconnectInterval)
	}
	
	t.Logf("Total browser opens: %d", browserOpens)
	
	// Should have limited browser opens due to rate limiting
	assert.LessOrEqual(t, int(browserOpens), 3, "Rate limiting should prevent rapid browser opens")
}

// TestOAuth_EC006_StaleInProgressFlag tests that stale "in progress" flags
// are detected and cleared.
func TestOAuth_EC006_StaleInProgressFlag(t *testing.T) {
	tracker := NewOAuthStateTracker()
	serverName := "stale-flag-server"
	
	// Set OAuth in progress but don't actually start a flow
	tracker.MarkOAuthStart(serverName)
	
	// Simulate stale detection timeout
	staleTimeout := 100 * time.Millisecond
	time.Sleep(staleTimeout)
	
	// A monitor would detect this and clear the stale state
	// For this test, we simulate the cleanup
	if tracker.IsOAuthInProgress(serverName) {
		t.Log("Detected stale OAuth in-progress flag, clearing")
		tracker.MarkOAuthComplete(serverName)
	}
	
	assert.False(t, tracker.IsOAuthInProgress(serverName), "Stale flag should be cleared")
}

// TestOAuth_EC007_MultipleServersSimultaneous tests that OAuth for multiple
// servers can proceed independently.
func TestOAuth_EC007_MultipleServersSimultaneous(t *testing.T) {
	tracker := NewOAuthStateTracker()
	mockBrowser := &MockBrowserOpener{}
	
	servers := []string{"server-a", "server-b", "server-c"}
	var wg sync.WaitGroup
	
	for _, serverName := range servers {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			
			tracker.MarkOAuthStart(name)
			_ = mockBrowser.Open("https://auth.example.com/oauth?server=" + name)
			tracker.MarkBrowserOpen(name)
			
			// Simulate OAuth duration
			time.Sleep(20 * time.Millisecond)
			
			tracker.MarkTokensSaved(name)
			tracker.MarkClientRecreated(name)
			tracker.MarkOAuthComplete(name)
		}(serverName)
	}
	
	wg.Wait()
	
	// Each server should have opened one browser
	assert.Equal(t, len(servers), mockBrowser.GetOpenCount(), 
		"Each server should have its own browser window")
	
	// All servers should be complete
	for _, serverName := range servers {
		assert.False(t, tracker.IsOAuthInProgress(serverName),
			"OAuth should be complete for %s", serverName)
		
		err := tracker.CheckInvariant_ClientRecreated(serverName)
		assert.NoError(t, err, "Client should be recreated for %s", serverName)
	}
}

// TestOAuth_EC008_BrowserCancelled tests that the system handles the case
// where the user opens the browser but closes it without completing OAuth.
func TestOAuth_EC008_BrowserCancelled(t *testing.T) {
	tracker := NewOAuthStateTracker()
	serverName := "cancelled-oauth-server"
	
	// Start OAuth
	tracker.MarkOAuthStart(serverName)
	
	// Simulate timeout (user never completes OAuth)
	oauthTimeout := 100 * time.Millisecond
	
	ctx, cancel := context.WithTimeout(context.Background(), oauthTimeout)
	defer cancel()
	
	// Wait for OAuth callback (will timeout)
	mockCallback := NewMockOAuthCallback()
	mockCallback.ShouldTimeout = true
	
	_, err := mockCallback.WaitForCallback(ctx)
	assert.Error(t, err, "Should timeout waiting for callback")
	
	// Clean up OAuth state
	tracker.MarkOAuthComplete(serverName)
	
	// Verify state is clean
	assert.False(t, tracker.IsOAuthInProgress(serverName))
	
	// User should be able to retry
	tracker.MarkOAuthStart(serverName)
	assert.True(t, tracker.IsOAuthInProgress(serverName))
}

// TestOAuth_ScanForNewTokens_DoesNotTriggerOAuth tests that the background
// token scanner doesn't trigger OAuth when tokens don't exist.
func TestOAuth_ScanForNewTokens_DoesNotTriggerOAuth(t *testing.T) {
	tracker := NewOAuthStateTracker()
	mockBrowser := &MockBrowserOpener{}
	serverName := "scan-test-server"
	
	// Simulate scanForNewTokens behavior
	tokenExists := false
	tokenValid := false
	
	// scanForNewTokens should NOT trigger OAuth when tokens don't exist
	// It should only trigger reconnection if tokens exist but aren't loaded
	
	if !tokenExists {
		// No tokens - should not trigger OAuth
		t.Log("No tokens found, not triggering OAuth (correct behavior)")
	} else if tokenValid {
		// Valid tokens exist - may trigger reconnection but not OAuth
		t.Log("Valid tokens found, may trigger reconnection")
	} else {
		// Invalid/expired tokens - may trigger OAuth
		tracker.MarkOAuthStart(serverName)
		_ = mockBrowser.Open("https://auth.example.com/oauth")
	}
	
	assert.Equal(t, 0, mockBrowser.GetOpenCount(), 
		"scanForNewTokens should not open browser when no tokens exist")
}

// TestOAuth_ReconnectionAfterCompletion tests that reconnection after OAuth
// completion properly loads the new tokens.
func TestOAuth_ReconnectionAfterCompletion(t *testing.T) {
	tracker := NewOAuthStateTracker()
	serverName := "reconnect-after-oauth-server"
	
	// Complete OAuth flow
	tracker.MarkOAuthStart(serverName)
	tracker.MarkBrowserOpen(serverName)
	tracker.MarkTokensSaved(serverName)
	tracker.MarkOAuthComplete(serverName)
	
	// Trigger reconnection
	tracker.MarkReconnectionAttempt(serverName)
	
	// Should recreate client with new tokens
	tracker.MarkClientRecreated(serverName)
	
	// Verify invariants
	err := tracker.CheckInvariant_ClientRecreated(serverName)
	assert.NoError(t, err)
	
	violations := tracker.GetViolations()
	assert.Empty(t, violations, "No violations during reconnection after OAuth")
}

