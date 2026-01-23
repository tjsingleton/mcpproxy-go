# Feature Specification: OAuth Token Recovery & Connection Resilience

**Feature Branch**: `013-oauth-token-recovery`  
**Created**: 2025-12-14  
**Status**: Draft  
**Input**: User description: "OAuth keeps breaking - fixes for token loading, browser spam prevention, forced reconnection, and connection timeouts"

## Problem Statement

This spec addresses remaining OAuth reliability issues not covered by upstream's `RefreshManager` (PR #180) and related fixes:

1. **Tool Call Token Recovery**: When tool calls fail with "no valid token available" but tokens exist in storage, the system should automatically reconnect to load them
2. **Browser Rate Limiting**: Prevent browser spam during rapid connection retries (complements upstream's `OAuthFlowCoordinator`)
3. **Forced Reconnection**: Allow bypassing the "already connected" check when tokens need to be reloaded
4. **Connection Timeout**: Longer timeouts for OAuth-based servers that require browser authentication
5. **Improved Token Scanning**: Check all enabled servers for token issues, not just those in Error state

## Relationship to Existing Specs & Upstream

### Builds On
- **spec/007-oauth-reliability**: Defines OAuth state machine and invariants
- **spec/009-proactive-oauth-refresh**: Defines upstream's `RefreshManager`
- **Upstream PR #180**: Proactive token refresh
- **Upstream PR #181**: Skip browser when token exists

### Complements (Does Not Replace)
- **RefreshManager**: Handles proactive token refresh before expiration
- **OAuthFlowCoordinator**: Prevents concurrent OAuth flows for same server
- **This Spec**: Handles reactive recovery when tokens exist but aren't loaded

## Key Differences from RefreshManager

| Aspect | RefreshManager (Upstream) | This Spec |
|--------|---------------------------|-----------|
| Trigger | Proactive (before expiry) | Reactive (after failure) |
| Scope | Token refresh scheduling | Connection recovery |
| Rate Limiting | Per-token refresh | Per-browser open |
| Force Reconnect | Not supported | Supported (`force=true`) |

## OAuth Recovery State Machine

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                    OAuth Token Recovery Flow                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────┐                                                    │
│  │  Tool Call Made     │                                                    │
│  └──────────┬──────────┘                                                    │
│             │                                                                │
│             ▼                                                                │
│  ┌─────────────────────┐                                                    │
│  │  CallTool()         │                                                    │
│  └──────────┬──────────┘                                                    │
│             │                                                                │
│       ┌─────┴─────┐                                                         │
│       │           │                                                         │
│       ▼           ▼                                                         │
│  [success]    [error]                                                       │
│       │           │                                                         │
│       │           ▼                                                         │
│       │    ┌──────────────────────┐                                         │
│       │    │ Check Error Type     │                                         │
│       │    └──────────┬───────────┘                                         │
│       │               │                                                      │
│       │         ┌─────┴─────┐                                                │
│       │         │           │                                                │
│       │         ▼           ▼                                                │
│       │  [token error]  [other error]                                        │
│       │         │           │                                                │
│       │         ▼           │                                                │
│       │  ┌──────────────────┐ │                                              │
│       │  │ Check Token Store│ │                                              │
│       │  └────────┬─────────┘ │                                              │
│       │           │           │                                              │
│       │     ┌─────┴─────┐     │                                              │
│       │     │           │     │                                              │
│       │     ▼           ▼     │                                              │
│       │ [tokens     [no       │                                              │
│       │  exist]     tokens]   │                                              │
│       │     │           │     │                                              │
│       │     ▼           │     │                                              │
│       │ ┌───────────────┐     │                                              │
│       │ │ Force         │     │                                              │
│       │ │ Reconnection  │     │                                              │
│       │ │ (async)       │     │                                              │
│       │ └───────┬───────┘     │                                              │
│       │         │             │                                              │
│       │         ▼             ▼                                              │
│       │   [reconnect    [return error                                        │
│       │    to load       to caller]                                          │
│       │    tokens]           │                                               │
│       │         │             │                                              │
│       └─────────┴─────────────┘                                              │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Invariants (Must Always Be True)

### INV-013-001: Token Error Recovery
When a tool call fails with a token error AND valid tokens exist in storage, reconnection MUST be triggered.

### INV-013-002: Browser Rate Limit Independence
Browser rate limiting MUST be independent from connection attempts (only track actual browser opens).

### INV-013-003: Force Reconnection Available
The system MUST support forcing reconnection even when client appears connected.

### INV-013-004: Timeout Adequacy
Connection timeout MUST be sufficient for browser-based OAuth completion (5 minutes minimum).

### INV-013-005: No Interference with RefreshManager
Token recovery MUST NOT interfere with proactive token refresh scheduling.

## User Scenarios & Testing

### User Story 1 - Tool Call Token Recovery (Priority: P0)

A user's tool call fails because tokens exist in storage but the HTTP client doesn't have them loaded. The system should automatically reconnect to load the tokens.

**Why this priority**: This is the most common failure mode - OAuth completed but tokens weren't loaded.

**Independent Test**: Can be tested by simulating a tool call failure with tokens in storage.

**Acceptance Scenarios**:

1. **Given** a connected server with tokens in storage but HTTP client without tokens
   **When** a tool call fails with "no valid token available"
   **Then** the system checks token storage
   **And** finds valid tokens exist
   **And** triggers forced reconnection (async)
   **And** logs the recovery attempt
   **And** subsequent tool calls succeed after reconnection

2. **Given** a tool call fails with "no valid token available"
   **When** no tokens exist in storage
   **Then** NO reconnection is triggered
   **And** the error is returned to the caller as-is

3. **Given** a tool call fails with a non-token error (e.g., network error)
   **When** the error is processed
   **Then** NO token recovery is attempted
   **And** the error is returned to the caller as-is

---

### User Story 2 - Browser Spam Prevention (Priority: P0)

Rapid connection retries should not open multiple browser windows.

**Why this priority**: Browser spam is the most visible and annoying OAuth failure mode.

**Independent Test**: Trigger 10 rapid reconnections; verify at most 1 browser opens.

**Acceptance Scenarios**:

1. **Given** a browser was opened for OAuth within 10 seconds
   **When** another reconnection attempts to trigger OAuth
   **Then** NO additional browser window opens
   **And** the rate limit is logged

2. **Given** the `oauthBrowserRateLimit` map tracks server -> timestamp
   **When** a browser open is successful
   **Then** the timestamp is updated for that server

3. **Given** OAuth is in progress (checked via `IsOAuthInProgress()`)
   **When** reconnection is attempted
   **Then** reconnection is skipped
   **And** "OAuth already in progress" is logged

---

### User Story 3 - Forced Reconnection (Priority: P1)

The system supports forcing reconnection even when the client appears connected.

**Why this priority**: Essential for token recovery when client state is stale.

**Independent Test**: Force reconnect a connected client and verify it reconnects.

**Acceptance Scenarios**:

1. **Given** a client reports `IsConnected() == true`
   **And** tokens exist in storage but aren't loaded
   **When** `RetryConnectionWithForce(serverName, true)` is called
   **Then** the client is disconnected
   **And** reconnection proceeds
   **And** tokens are loaded into the new HTTP client

2. **Given** a client reports `IsConnected() == true`
   **And** OAuth was recently completed
   **When** `RetryConnectionWithForce(serverName, false)` is called
   **Then** reconnection proceeds (recent completion bypasses check)

3. **Given** a client reports `IsConnected() == true`
   **And** OAuth was NOT recently completed
   **When** `RetryConnectionWithForce(serverName, false)` is called
   **Then** reconnection is skipped (prevents flapping)

---

### User Story 4 - Connection Timeout for OAuth Servers (Priority: P1)

OAuth-based servers need longer timeouts for browser authentication.

**Why this priority**: Users need time to complete OAuth in their browser.

**Independent Test**: Connect to an OAuth server; verify 5-minute timeout allows authentication.

**Acceptance Scenarios**:

1. **Given** a server requires OAuth
   **When** `AddServer()` is called
   **Then** the connection timeout is 5 minutes (not 30 seconds)

2. **Given** OAuth browser is opened
   **When** user takes 3 minutes to authenticate
   **Then** the connection does NOT timeout
   **And** OAuth completes successfully

---

### User Story 5 - Improved Token Scanning (Priority: P2)

Background token scanning should check all enabled servers, not just those in Error state.

**Why this priority**: Connected servers may have stale token state.

**Independent Test**: Verify scanning finds token issues on connected servers.

**Acceptance Scenarios**:

1. **Given** a connected server with OAuth completion within 5 minutes
   **When** `scanForNewTokens()` runs
   **Then** reconnection is triggered to load new tokens

2. **Given** a connected server with NO recent OAuth completion
   **When** `scanForNewTokens()` runs
   **Then** reconnection is NOT triggered (prevents flapping)

3. **Given** a server with OAuth in progress
   **When** `scanForNewTokens()` runs
   **Then** that server is skipped
   **And** "OAuth already in progress" is logged

---

## Edge Cases

### EC-013-001: Force Reconnect During Active Tool Call
**Scenario**: Force reconnect is triggered while a tool call is in progress
**Expected**: Current tool call completes with error; reconnection proceeds
**Test**: Start long-running tool call, trigger force reconnect, verify both complete

### EC-013-002: Token Recovery Loop Prevention
**Scenario**: Token recovery triggers reconnection, which fails, which triggers recovery again
**Expected**: Rate limiting prevents infinite loop; max 1 recovery per 10 seconds per server
**Test**: Mock persistent token errors; verify recovery doesn't loop

### EC-013-003: RefreshManager vs Token Recovery Race
**Scenario**: RefreshManager schedules proactive refresh while token recovery is triggered
**Expected**: Both proceed independently; last token wins; no duplicate reconnections
**Test**: Trigger both simultaneously; verify clean state

### EC-013-004: Stale Browser Rate Limit
**Scenario**: Browser rate limit timestamp is stale but server needs new OAuth
**Expected**: Rate limit expires after 5 minutes; new browser can open
**Test**: Set old timestamp; verify new browser opens after expiry

## Requirements

### Functional Requirements

- **FR-013-001**: System MUST check token storage when tool call fails with token error
- **FR-013-002**: System MUST trigger forced reconnection when tokens exist but HTTP client doesn't have them
- **FR-013-003**: System MUST support `RetryConnectionWithForce(serverName, force bool)` API
- **FR-013-004**: System MUST track browser open timestamps per server in `oauthBrowserRateLimit`
- **FR-013-005**: System MUST skip OAuth if browser was opened within 10 seconds for same server
- **FR-013-006**: System MUST use 5-minute timeout for AddServer() connection
- **FR-013-007**: System MUST check `IsOAuthInProgress()` before attempting OAuth
- **FR-013-008**: `scanForNewTokens()` MUST check all enabled servers

### Non-Functional Requirements

- **NFR-013-001**: Token recovery MUST be async (not block caller)
- **NFR-013-002**: Browser rate limit window MUST be 10 seconds minimum
- **NFR-013-003**: Force reconnection MUST complete within 30 seconds
- **NFR-013-004**: Token recovery MUST be idempotent

## Success Criteria

### Measurable Outcomes

- **SC-013-001**: Tool calls with token errors + tokens in storage trigger recovery 100% of time
- **SC-013-002**: Zero browser spam during rapid reconnections (max 1 browser per 10s per server)
- **SC-013-003**: Force reconnection successfully loads tokens 100% of time
- **SC-013-004**: OAuth servers don't timeout during normal browser authentication
- **SC-013-005**: All invariants hold under concurrent testing

## Test Plan

### Unit Tests (`internal/upstream/oauth_recovery_test.go`)

```go
// Test token recovery detection
func TestCallTool_TokenError_TriggersRecovery(t *testing.T)
func TestCallTool_TokenError_NoTokens_NoRecovery(t *testing.T)
func TestCallTool_NonTokenError_NoRecovery(t *testing.T)

// Test browser rate limiting
func TestOAuthBrowserRateLimit_PreventsDuplicateOpens(t *testing.T)
func TestOAuthBrowserRateLimit_AllowsAfterExpiry(t *testing.T)
func TestOAuthBrowserRateLimit_PerServer(t *testing.T)

// Test forced reconnection
func TestRetryConnectionWithForce_True_ReconnectsConnected(t *testing.T)
func TestRetryConnectionWithForce_False_SkipsConnected(t *testing.T)
func TestRetryConnectionWithForce_RecentOAuth_Reconnects(t *testing.T)

// Test timeout
func TestAddServer_OAuthServer_5MinuteTimeout(t *testing.T)

// Test scanning
func TestScanForNewTokens_ChecksAllEnabled(t *testing.T)
func TestScanForNewTokens_SkipsOAuthInProgress(t *testing.T)
```

### Integration Tests (`internal/upstream/oauth_recovery_integration_test.go`)

```go
func TestOAuthRecovery_EndToEnd(t *testing.T)
func TestOAuthRecovery_WithRefreshManager(t *testing.T)
func TestOAuthRecovery_NoBrowserSpam(t *testing.T)
```

### Invariant Tests (`internal/upstream/oauth_recovery_invariants_test.go`)

```go
func TestInvariant_013_001_TokenErrorRecovery(t *testing.T)
func TestInvariant_013_002_BrowserRateLimitIndependent(t *testing.T)
func TestInvariant_013_003_ForceReconnectAvailable(t *testing.T)
func TestInvariant_013_004_TimeoutAdequate(t *testing.T)
func TestInvariant_013_005_NoRefreshManagerInterference(t *testing.T)
```

## Implementation Status

### Already Implemented (In Stash)

| Component | Status | File | Lines Changed |
|-----------|--------|------|---------------|
| `oauthBrowserRateLimit` map | ✅ Done | `manager.go` | +10 |
| `RetryConnectionWithForce()` | ✅ Done | `manager.go` | +30 |
| CallTool token error recovery | ✅ Done | `manager.go` | +20 |
| AddServer 5-min timeout | ✅ Done | `manager.go` | +2 |
| `scanForNewTokens()` improvements | ✅ Done | `manager.go` | +40 |

### Needs Testing

| Test File | Status |
|-----------|--------|
| `oauth_recovery_test.go` | ❌ Not Written |
| `oauth_recovery_integration_test.go` | ❌ Not Written |
| `oauth_recovery_invariants_test.go` | ❌ Not Written |

## Potential Conflicts with RefreshManager

### No Conflicts Identified

The token recovery mechanism is **complementary** to RefreshManager:

1. **RefreshManager**: Proactively refreshes tokens BEFORE they expire
2. **Token Recovery**: Reactively recovers AFTER a tool call fails with token error

These operate in different scenarios:
- RefreshManager handles normal token lifecycle
- Token Recovery handles edge cases where tokens exist but aren't loaded

### Integration Points

Both systems interact with:
- `PersistentTokenStore` - both read/write tokens
- `OAuthFlowCoordinator` - both check `IsFlowActive()`
- Connection state - both may trigger reconnection

### Recommended Coordination

1. Token Recovery checks `RefreshManager.IsRefreshScheduled()` before triggering (optional)
2. RefreshManager events trigger token recovery for faster loading (optional)
3. Both respect `OAuthFlowCoordinator` locks

## Out of Scope

- Token refresh implementation (handled by RefreshManager)
- OAuth provider specifics (handled by mcp-go)
- UI changes (no Web UI modifications)
- CLI changes beyond existing functionality



