# Feature Specification: OAuth Reliability

**Feature Branch**: `007-oauth-reliability`  
**Created**: 2025-12-09  
**Status**: Draft  
**Input**: User description: "OAuth keeps breaking - fix one bug, another pops up. We need comprehensive specs and tests."

## Problem Statement

OAuth authentication in mcpproxy has been plagued by a series of cascading bugs:

1. **Browser Spam**: Multiple browser windows open when OAuth is triggered
2. **Token Loading Failure**: OAuth completes, tokens are saved, but HTTP client doesn't have them
3. **Reconnection Loops**: System continuously tries to reconnect, triggering OAuth repeatedly
4. **Rate Limiting Bypass**: Rate limits don't prevent browser spam due to client recreation
5. **State Machine Inconsistency**: OAuth "in progress" flags are not properly coordinated

The root cause is that OAuth behavior spans multiple components (core client, managed client, manager, token store) with complex state interactions that were never fully specified or tested.

## OAuth State Machine

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           OAuth State Machine                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐                                                             │
│  │   IDLE      │◄──────────────────────────────────────────────┐            │
│  │ (no tokens) │                                               │            │
│  └──────┬──────┘                                               │            │
│         │ Connect()                                            │            │
│         ▼                                                      │            │
│  ┌─────────────────┐                                           │            │
│  │   CONNECTING    │                                           │            │
│  │ (check tokens)  │                                           │            │
│  └────────┬────────┘                                           │            │
│           │                                                    │            │
│     ┌─────┴─────┐                                              │            │
│     │           │                                              │            │
│     ▼           ▼                                              │            │
│ [tokens      [no tokens /                                      │            │
│  exist]       401 error]                                       │            │
│     │           │                                              │            │
│     │           ▼                                              │            │
│     │    ┌─────────────────┐                                   │            │
│     │    │ OAUTH_REQUIRED  │                                   │            │
│     │    │ (check rate limit)│                                 │            │
│     │    └────────┬────────┘                                   │            │
│     │             │                                            │            │
│     │       ┌─────┴─────┐                                      │            │
│     │       │           │                                      │            │
│     │       ▼           ▼                                      │            │
│     │  [rate limited] [can open browser]                       │            │
│     │       │           │                                      │            │
│     │       │           ▼                                      │            │
│     │       │    ┌─────────────────┐                           │            │
│     │       │    │ OAUTH_IN_PROGRESS│◄─────────┐               │            │
│     │       │    │ (browser opened) │          │               │            │
│     │       │    └────────┬────────┘           │               │            │
│     │       │             │                    │               │            │
│     │       │       ┌─────┴─────┐              │               │            │
│     │       │       │           │              │               │            │
│     │       │       ▼           ▼              │               │            │
│     │       │  [callback   [timeout/          │               │            │
│     │       │   received]   error]            │               │            │
│     │       │       │           │              │               │            │
│     │       │       ▼           │              │               │            │
│     │       │ ┌─────────────┐   │              │               │            │
│     │       │ │TOKEN_EXCHANGE│  │              │               │            │
│     │       │ └──────┬──────┘   │              │               │            │
│     │       │        │          │              │               │            │
│     │       │  ┌─────┴─────┐    │              │               │            │
│     │       │  │           │    │              │               │            │
│     │       │  ▼           ▼    │              │               │            │
│     │       │[success]  [error] │              │               │            │
│     │       │  │           │    │              │               │            │
│     │       │  ▼           ▼    ▼              │               │            │
│     │       │ ┌───────────────────┐            │               │            │
│     │       │ │   TOKENS_SAVED    │            │               │            │
│     │       │ │ (persist to DB)   │            │               │            │
│     │       └►└────────┬──────────┘            │               │            │
│     │                  │                       │               │            │
│     │                  ▼                       │               │            │
│     │         ┌─────────────────┐              │               │            │
│     │         │  RECONNECTING   │──────────────┘               │            │
│     │         │ (with tokens)   │  [OAuth still needed]        │            │
│     │         └────────┬────────┘                              │            │
│     │                  │                                       │            │
│     └──────────────────┤                                       │            │
│                        ▼                                       │            │
│                 ┌─────────────┐                                │            │
│                 │  CONNECTED  │                                │            │
│                 │  (ready)    │                                │            │
│                 └──────┬──────┘                                │            │
│                        │                                       │            │
│                        ▼                                       │            │
│                  [token expires/                               │            │
│                   call fails]                                  │            │
│                        │                                       │            │
│                        └───────────────────────────────────────┘            │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Invariants (MUST ALWAYS BE TRUE)

These invariants define the core contract that must never be violated:

### INV-001: Single Browser Per Server
At most ONE browser window may be open for OAuth for any given server at any time.

### INV-002: Token Persistence Before Completion
Tokens MUST be persisted to storage BEFORE marking OAuth as complete.

### INV-003: Client Recreation After OAuth
After OAuth completion, the HTTP client MUST be recreated to load new tokens.

### INV-004: Rate Limit on Browser Opens Only
Browser rate limiting MUST only apply to actual successful browser opens, not to OAuth attempts.

### INV-005: No Reconnection During OAuth
The system MUST NOT trigger reconnection for a server while OAuth is in progress.

### INV-006: OAuth Progress is Global
The "OAuth in progress" flag MUST be checked globally (across all client instances for a server).

### INV-007: Token Check Before OAuth
Before triggering OAuth, the system MUST check if valid tokens already exist in storage.

### INV-008: Cleanup on Failure
OAuth state MUST be cleaned up when a flow fails or times out.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - First-Time OAuth Authentication (Priority: P0)

A user connects to an OAuth-protected server for the first time. The system should open exactly one browser window, wait for authentication, and connect successfully.

**Acceptance Scenarios**:

1. **Given** a server requires OAuth and has no stored tokens
   **When** the system attempts to connect
   **Then** exactly ONE browser window opens with the authorization URL
   **And** the system waits for the callback
   **And** after user authenticates, tokens are stored
   **And** the connection succeeds

2. **Given** a server requires OAuth and has no stored tokens
   **When** the user doesn't complete OAuth within 2 minutes
   **Then** the OAuth flow times out
   **And** the OAuth in-progress flag is cleared
   **And** an error is logged with the timeout reason

3. **Given** a server requires OAuth and has no stored tokens
   **When** OAuth is triggered but browser fails to open
   **Then** the authorization URL is displayed in logs
   **And** the rate limit is NOT updated
   **And** subsequent attempts can try to open the browser

---

### User Story 2 - Reconnection With Existing Tokens (Priority: P0)

A user has previously authenticated. The system should use existing tokens without opening a browser.

**Acceptance Scenarios**:

1. **Given** a server has valid stored tokens (not expired)
   **When** the system attempts to connect
   **Then** NO browser window opens
   **And** the HTTP client is created with the stored tokens
   **And** the connection succeeds

2. **Given** a server has expired tokens with a refresh token
   **When** the system attempts to connect
   **Then** the token is refreshed automatically
   **And** NO browser window opens
   **And** the connection succeeds

3. **Given** a server has expired tokens without a refresh token
   **When** the system attempts to connect
   **Then** OAuth is triggered
   **And** exactly ONE browser window opens

---

### User Story 3 - OAuth During Active Session (Priority: P0)

A user is connected but their token expires mid-session. The system should handle re-authentication gracefully.

**Acceptance Scenarios**:

1. **Given** a connected server with an expiring token
   **When** a tool call fails with 401/invalid_token
   **Then** the system attempts token refresh first
   **And** if refresh fails, triggers OAuth
   **And** at most ONE browser window opens

2. **Given** OAuth was just completed successfully
   **When** the system triggers reconnection
   **Then** the new tokens are loaded into the HTTP client
   **And** the connection succeeds without opening a browser

---

### User Story 4 - No Browser Spam (Priority: P0)

The system must NEVER open multiple browser windows regardless of internal state.

**Acceptance Scenarios**:

1. **Given** OAuth is already in progress for a server
   **When** any component attempts to trigger OAuth
   **Then** NO additional browser window opens
   **And** the component waits for or uses the existing OAuth flow

2. **Given** a browser was opened for OAuth within 5 minutes
   **When** another OAuth attempt is triggered (not manual)
   **Then** NO browser window opens
   **And** the authorization URL is logged for manual use

3. **Given** multiple reconnection attempts happen in rapid succession
   **When** each attempts to trigger OAuth
   **Then** at most ONE browser window opens total

4. **Given** the manager's scanForNewTokens() runs every 5 seconds
   **When** tokens don't exist for a server
   **Then** it does NOT trigger OAuth (waits for explicit connection attempt)

---

### User Story 5 - OAuth Completion Detection (Priority: P1)

After OAuth completes (browser callback received), the system must reconnect with new tokens.

**Acceptance Scenarios**:

1. **Given** OAuth callback is received with authorization code
   **When** token exchange succeeds
   **Then** tokens are saved to persistent storage
   **And** OAuth completion event is emitted
   **And** HTTP client is recreated with new tokens
   **And** connection is established

2. **Given** OAuth completion event is emitted
   **When** any client for that server is in Error/Disconnected state
   **Then** reconnection is triggered
   **And** reconnection uses the newly stored tokens

3. **Given** OAuth completed but client appears connected
   **When** tokens exist in storage but HTTP client doesn't have them
   **Then** the client is disconnected and reconnected to load tokens

---

### User Story 6 - Manual OAuth Re-authentication (Priority: P2)

A user can force OAuth re-authentication when needed.

**Acceptance Scenarios**:

1. **Given** a server has existing tokens
   **When** user runs `mcpproxy auth login --server=X --force`
   **Then** existing tokens are cleared
   **And** a browser window opens for fresh authentication
   **And** rate limiting does NOT prevent this (manual flows bypass rate limit)

2. **Given** a server has invalid/expired tokens
   **When** user runs `mcpproxy auth login --server=X`
   **Then** a browser window opens
   **And** new tokens replace old tokens after authentication

---

## Edge Cases

### EC-001: Concurrent OAuth Triggers
**Scenario**: Multiple goroutines try to trigger OAuth simultaneously
**Expected**: Only one OAuth flow runs; others wait or return "in progress"
**Test**: Spawn 10 goroutines calling Connect() simultaneously; verify single browser

### EC-002: OAuth During Shutdown
**Scenario**: OAuth is in progress when application shuts down
**Expected**: OAuth flow is cancelled gracefully; no hanging processes
**Test**: Start OAuth, trigger shutdown, verify clean cancellation

### EC-003: Database Lock During OAuth
**Scenario**: CLI tries to save tokens but database is locked by daemon
**Expected**: Cross-process notification via file or polling works
**Test**: Run daemon, run CLI OAuth, verify daemon detects new tokens

### EC-004: Token Saved but HTTP Client Not Updated
**Scenario**: OAuth completes, tokens saved, but connection still fails
**Expected**: System detects this and forces client recreation
**Test**: Mock scenario where tokens exist but client reports auth failure

### EC-005: Rapid Reconnection Attempts
**Scenario**: Connection fails repeatedly, triggering rapid reconnection
**Expected**: Exponential backoff prevents rapid OAuth triggers
**Test**: Simulate 10 connection failures; verify backoff timing

### EC-006: Stale OAuth In-Progress Flag
**Scenario**: OAuth flag says "in progress" but no actual flow running
**Expected**: System detects stale state after timeout and clears it
**Test**: Set flag, don't start flow, verify timeout clears flag

### EC-007: Multiple Servers OAuth Simultaneously
**Scenario**: User authenticates multiple OAuth servers at once
**Expected**: Each server gets independent OAuth flow; no interference
**Test**: Trigger OAuth for 3 servers; verify 3 separate browser windows

### EC-008: Browser Opens But User Cancels
**Scenario**: Browser opens, user closes it without authenticating
**Expected**: OAuth times out, state is cleared, user can retry
**Test**: Start OAuth, simulate timeout, verify clean state

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST maintain a global OAuth-in-progress flag per server
- **FR-002**: System MUST check OAuth-in-progress before opening browser
- **FR-003**: System MUST check token storage before triggering OAuth
- **FR-004**: System MUST recreate HTTP client after OAuth completion
- **FR-005**: System MUST clear OAuth state on timeout or failure
- **FR-006**: System MUST NOT trigger OAuth from background scans when tokens don't exist
- **FR-007**: System MUST support cross-process OAuth completion detection
- **FR-008**: System MUST apply rate limiting only to successful browser opens
- **FR-009**: System MUST allow manual OAuth to bypass rate limiting
- **FR-010**: System MUST emit events when OAuth state changes

### Non-Functional Requirements

- **NFR-001**: OAuth flow MUST complete or timeout within 5 minutes
- **NFR-002**: Token storage MUST be atomic (no partial writes)
- **NFR-003**: OAuth state checks MUST be thread-safe
- **NFR-004**: Browser rate limit window MUST be 5 minutes
- **NFR-005**: Background token scan MUST NOT run more than once per 5 seconds per server

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Zero browser spam incidents (0 duplicate browser windows per OAuth flow)
- **SC-002**: OAuth completion triggers successful reconnection 100% of the time
- **SC-003**: Token loading after OAuth succeeds 100% of the time
- **SC-004**: All invariants hold under concurrent load testing
- **SC-005**: Integration tests pass for all 6 user stories
- **SC-006**: Edge case tests pass for all 8 scenarios

## Test Plan

### Unit Tests (`internal/oauth/state_machine_test.go`)

```go
// Test OAuth state transitions
func TestOAuthStateMachine_SingleBrowserInvariant(t *testing.T)
func TestOAuthStateMachine_TokenCheckBeforeOAuth(t *testing.T)
func TestOAuthStateMachine_CleanupOnTimeout(t *testing.T)
func TestOAuthStateMachine_RateLimitOnBrowserOpenOnly(t *testing.T)
func TestOAuthStateMachine_GlobalInProgressFlag(t *testing.T)
```

### Integration Tests (`internal/upstream/oauth_integration_test.go`)

```go
// Test full OAuth flows
func TestOAuth_FirstTimeAuthentication(t *testing.T)
func TestOAuth_ReconnectionWithExistingTokens(t *testing.T)
func TestOAuth_TokenExpiredDuringSession(t *testing.T)
func TestOAuth_NoBrowserSpam_ConcurrentTriggers(t *testing.T)
func TestOAuth_CompletionDetection(t *testing.T)
func TestOAuth_ManualReauthentication(t *testing.T)
```

### Edge Case Tests (`internal/upstream/oauth_edge_cases_test.go`)

```go
func TestOAuth_EC001_ConcurrentTriggers(t *testing.T)
func TestOAuth_EC002_ShutdownDuringOAuth(t *testing.T)
func TestOAuth_EC003_DatabaseLock(t *testing.T)
func TestOAuth_EC004_TokenSavedClientNotUpdated(t *testing.T)
func TestOAuth_EC005_RapidReconnection(t *testing.T)
func TestOAuth_EC006_StaleInProgressFlag(t *testing.T)
func TestOAuth_EC007_MultipleServersSimultaneous(t *testing.T)
func TestOAuth_EC008_BrowserCancelled(t *testing.T)
```

### Invariant Tests (`internal/upstream/oauth_invariants_test.go`)

```go
// Property-based tests for invariants
func TestInvariant_INV001_SingleBrowserPerServer(t *testing.T)
func TestInvariant_INV002_TokenPersistenceBeforeCompletion(t *testing.T)
func TestInvariant_INV003_ClientRecreationAfterOAuth(t *testing.T)
func TestInvariant_INV004_RateLimitOnBrowserOnly(t *testing.T)
func TestInvariant_INV005_NoReconnectionDuringOAuth(t *testing.T)
func TestInvariant_INV006_GlobalProgressFlag(t *testing.T)
func TestInvariant_INV007_TokenCheckBeforeOAuth(t *testing.T)
func TestInvariant_INV008_CleanupOnFailure(t *testing.T)
```

## Implementation Approach

### Phase 1: Global OAuth State Manager

Create a new centralized OAuth state manager that:
1. Tracks OAuth-in-progress per server globally
2. Tracks browser open timestamps per server globally
3. Is shared across all client instances
4. Provides atomic state transitions

### Phase 2: Test Infrastructure

Create mock infrastructure for testing OAuth:
1. Mock browser opener (records open attempts)
2. Mock callback server (can simulate success/failure/timeout)
3. Mock token storage (can simulate locks)
4. Test helpers for OAuth scenarios

### Phase 3: Implement Tests First (TDD)

Write failing tests for each scenario before fixing:
1. Tests assert invariants
2. Tests verify single browser opens
3. Tests verify token loading after OAuth
4. Tests verify rate limiting behavior

### Phase 4: Fix Implementation to Pass Tests

Refactor existing code to pass all tests:
1. Centralize OAuth state in manager
2. Add pre-OAuth token checks
3. Add post-OAuth client recreation
4. Add proper cleanup on failure

## Out of Scope

- OAuth provider-specific behavior (handled by mcp-go library)
- Token refresh mechanisms (beyond triggering when needed)
- OAuth scope discovery (separate spec)
- UI improvements beyond current functionality

