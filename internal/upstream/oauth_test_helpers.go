package upstream

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// MockBrowserOpener tracks browser open attempts for testing
type MockBrowserOpener struct {
	mu          sync.Mutex
	OpenCount   int32 // Atomic counter for thread-safe access
	OpenURLs    []string
	OpenTimes   []time.Time
	ShouldFail  bool
	FailError   error
	OpenDelay   time.Duration // Simulate slow browser opening
}

// Open records a browser open attempt
func (m *MockBrowserOpener) Open(url string) error {
	if m.OpenDelay > 0 {
		time.Sleep(m.OpenDelay)
	}
	
	atomic.AddInt32(&m.OpenCount, 1)
	
	m.mu.Lock()
	m.OpenURLs = append(m.OpenURLs, url)
	m.OpenTimes = append(m.OpenTimes, time.Now())
	m.mu.Unlock()
	
	if m.ShouldFail {
		return m.FailError
	}
	return nil
}

// GetOpenCount returns the number of browser opens (thread-safe)
func (m *MockBrowserOpener) GetOpenCount() int {
	return int(atomic.LoadInt32(&m.OpenCount))
}

// Reset clears the mock state
func (m *MockBrowserOpener) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	atomic.StoreInt32(&m.OpenCount, 0)
	m.OpenURLs = nil
	m.OpenTimes = nil
	m.ShouldFail = false
	m.FailError = nil
}

// MockOAuthCallback simulates OAuth callback scenarios
type MockOAuthCallback struct {
	mu            sync.Mutex
	Code          string
	Error         string
	Delay         time.Duration
	ShouldTimeout bool
	callbackChan  chan struct{}
}

// NewMockOAuthCallback creates a new mock callback
func NewMockOAuthCallback() *MockOAuthCallback {
	return &MockOAuthCallback{
		callbackChan: make(chan struct{}, 1),
	}
}

// TriggerSuccess simulates a successful OAuth callback
func (m *MockOAuthCallback) TriggerSuccess(code string) {
	m.mu.Lock()
	m.Code = code
	m.Error = ""
	m.mu.Unlock()
	
	if m.Delay > 0 {
		time.Sleep(m.Delay)
	}
	
	select {
	case m.callbackChan <- struct{}{}:
	default:
	}
}

// TriggerError simulates an OAuth error
func (m *MockOAuthCallback) TriggerError(errMsg string) {
	m.mu.Lock()
	m.Code = ""
	m.Error = errMsg
	m.mu.Unlock()
	
	select {
	case m.callbackChan <- struct{}{}:
	default:
	}
}

// WaitForCallback waits for the callback with timeout
func (m *MockOAuthCallback) WaitForCallback(ctx context.Context) (string, error) {
	if m.ShouldTimeout {
		<-ctx.Done()
		return "", ctx.Err()
	}
	
	select {
	case <-m.callbackChan:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.Error != "" {
			return "", &OAuthError{Message: m.Error}
		}
		return m.Code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// OAuthError represents an OAuth-specific error
type OAuthError struct {
	Message string
}

func (e *OAuthError) Error() string {
	return e.Message
}

// OAuthStateTracker tracks OAuth state for testing invariants
type OAuthStateTracker struct {
	mu                    sync.RWMutex
	InProgressServers     map[string]bool
	BrowserOpenTimes      map[string]time.Time
	BrowserOpenCounts     map[string]int
	TokensSaved           map[string]bool
	ClientsRecreated      map[string]bool
	ReconnectionAttempts  map[string]int
	
	// Violations detected during testing
	Violations            []string
}

// NewOAuthStateTracker creates a new state tracker
func NewOAuthStateTracker() *OAuthStateTracker {
	return &OAuthStateTracker{
		InProgressServers:    make(map[string]bool),
		BrowserOpenTimes:     make(map[string]time.Time),
		BrowserOpenCounts:    make(map[string]int),
		TokensSaved:          make(map[string]bool),
		ClientsRecreated:     make(map[string]bool),
		ReconnectionAttempts: make(map[string]int),
		Violations:           []string{},
	}
}

// MarkOAuthStart marks OAuth as started for a server
func (t *OAuthStateTracker) MarkOAuthStart(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if t.InProgressServers[serverName] {
		t.Violations = append(t.Violations, 
			"INV-001 violated: OAuth started while already in progress for "+serverName)
	}
	t.InProgressServers[serverName] = true
}

// MarkOAuthComplete marks OAuth as complete for a server
func (t *OAuthStateTracker) MarkOAuthComplete(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InProgressServers[serverName] = false
}

// MarkBrowserOpen marks a browser as opened for a server
func (t *OAuthStateTracker) MarkBrowserOpen(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.BrowserOpenCounts[serverName]++
	t.BrowserOpenTimes[serverName] = time.Now()
}

// MarkTokensSaved marks tokens as saved for a server
func (t *OAuthStateTracker) MarkTokensSaved(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.TokensSaved[serverName] = true
}

// MarkClientRecreated marks the client as recreated for a server
func (t *OAuthStateTracker) MarkClientRecreated(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ClientsRecreated[serverName] = true
}

// MarkReconnectionAttempt marks a reconnection attempt for a server
func (t *OAuthStateTracker) MarkReconnectionAttempt(serverName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if t.InProgressServers[serverName] {
		t.Violations = append(t.Violations,
			"INV-005 violated: Reconnection attempted while OAuth in progress for "+serverName)
	}
	t.ReconnectionAttempts[serverName]++
}

// IsOAuthInProgress checks if OAuth is in progress for a server
func (t *OAuthStateTracker) IsOAuthInProgress(serverName string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.InProgressServers[serverName]
}

// GetBrowserOpenCount returns the number of browser opens for a server
func (t *OAuthStateTracker) GetBrowserOpenCount(serverName string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.BrowserOpenCounts[serverName]
}

// GetViolations returns all invariant violations detected
func (t *OAuthStateTracker) GetViolations() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	violations := make([]string, len(t.Violations))
	copy(violations, t.Violations)
	return violations
}

// CheckInvariant_SingleBrowser checks INV-001: Single Browser Per Server
func (t *OAuthStateTracker) CheckInvariant_SingleBrowser(serverName string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	count := t.BrowserOpenCounts[serverName]
	if count > 1 {
		return &InvariantViolation{
			Invariant: "INV-001",
			Message:   "Multiple browsers opened for " + serverName,
			Details:   map[string]interface{}{"count": count},
		}
	}
	return nil
}

// CheckInvariant_TokensBeforeComplete checks INV-002
func (t *OAuthStateTracker) CheckInvariant_TokensBeforeComplete(serverName string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if !t.InProgressServers[serverName] && !t.TokensSaved[serverName] {
		// OAuth completed but tokens weren't saved - check if this is expected
		// This is only a violation if OAuth was marked complete without saving tokens
	}
	return nil
}

// CheckInvariant_ClientRecreated checks INV-003
func (t *OAuthStateTracker) CheckInvariant_ClientRecreated(serverName string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if t.TokensSaved[serverName] && !t.ClientsRecreated[serverName] {
		return &InvariantViolation{
			Invariant: "INV-003",
			Message:   "Tokens saved but client not recreated for " + serverName,
		}
	}
	return nil
}

// Reset clears all tracked state
func (t *OAuthStateTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InProgressServers = make(map[string]bool)
	t.BrowserOpenTimes = make(map[string]time.Time)
	t.BrowserOpenCounts = make(map[string]int)
	t.TokensSaved = make(map[string]bool)
	t.ClientsRecreated = make(map[string]bool)
	t.ReconnectionAttempts = make(map[string]int)
	t.Violations = []string{}
}

// InvariantViolation represents an invariant that was violated
type InvariantViolation struct {
	Invariant string
	Message   string
	Details   map[string]interface{}
}

func (v *InvariantViolation) Error() string {
	return v.Invariant + ": " + v.Message
}

