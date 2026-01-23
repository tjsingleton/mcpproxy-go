# OAuth Reliability Requirements Checklist

## Invariants

- [x] **INV-001**: Single Browser Per Server - `IsOAuthInProgress()` check added
- [x] **INV-002**: Token Persistence Before Completion - Tokens saved before marking complete
- [x] **INV-003**: Client Recreation After OAuth - `RetryConnection` forces disconnect/reconnect
- [x] **INV-004**: Rate Limit on Browser Opens Only - Only updated on successful browser open
- [x] **INV-005**: No Reconnection During OAuth - `RetryConnection` checks `IsOAuthInProgress()`
- [x] **INV-006**: OAuth Progress is Global - Token manager has global state
- [x] **INV-007**: Token Check Before OAuth - `scanForNewTokens` checks token existence
- [x] **INV-008**: Cleanup on Failure - OAuth state cleared on timeout/error

## Functional Requirements

- [x] **FR-001**: System maintains global OAuth-in-progress flag per server
- [x] **FR-002**: System checks OAuth-in-progress before opening browser
- [x] **FR-003**: System checks token storage before triggering OAuth
- [x] **FR-004**: System recreates HTTP client after OAuth completion
- [x] **FR-005**: System clears OAuth state on timeout or failure
- [x] **FR-006**: System does NOT trigger OAuth from background scans when tokens don't exist
- [ ] **FR-007**: System supports cross-process OAuth completion detection (partial - DB events)
- [x] **FR-008**: System applies rate limiting only to successful browser opens
- [x] **FR-009**: System allows manual OAuth to bypass rate limiting
- [x] **FR-010**: System emits events when OAuth state changes

## Test Coverage

### Unit Tests (Mock-based)
- [x] `TestInvariant_INV001_SingleBrowserPerServer` - Concurrent triggers, rate limiting
- [x] `TestInvariant_INV002_TokenPersistenceBeforeCompletion`
- [x] `TestInvariant_INV003_ClientRecreationAfterOAuth`
- [x] `TestInvariant_INV004_RateLimitOnBrowserOnly`
- [x] `TestInvariant_INV005_NoReconnectionDuringOAuth`
- [x] `TestInvariant_INV006_GlobalProgressFlag`
- [x] `TestInvariant_INV007_TokenCheckBeforeOAuth`
- [x] `TestInvariant_INV008_CleanupOnFailure`

### Edge Case Tests
- [x] `TestOAuth_EC001_ConcurrentTriggers`
- [x] `TestOAuth_EC002_ShutdownDuringOAuth`
- [x] `TestOAuth_EC004_TokenSavedClientNotUpdated`
- [x] `TestOAuth_EC005_RapidReconnection`
- [x] `TestOAuth_EC006_StaleInProgressFlag`
- [x] `TestOAuth_EC007_MultipleServersSimultaneous`
- [x] `TestOAuth_EC008_BrowserCancelled`

### Real Code Tests
- [x] `TestRealManager_OAuthInProgressCheck`
- [x] `TestRealManager_ScanForNewTokens_NoOAuthTrigger`
- [x] `TestRealManager_RetryConnection_SkipsWhenOAuthInProgress`
- [x] `TestRealManager_ConcurrentRetryConnections`
- [x] `TestRealTokenManager_GlobalOAuthState`
- [x] `TestTokenReconnectRateLimiting`
- [x] `TestOAuthCompletionCallback`
- [x] `TestManagerShutdownPreventsReconnection`
- [x] `TestOAuthInProgressSkipsRetry`
- [x] `TestManagerHasOAuthBrowserRateLimitMap`

## Implementation Status

### Browser Spam Prevention
- [x] Added `IsOAuthInProgress()` check in `scanForNewTokens()`
- [x] Added `IsOAuthInProgress()` check in `RetryConnection()`
- [x] Added public `IsOAuthInProgress()` method to core client
- [x] Added public `IsOAuthInProgress()` method to managed client
- [x] Added `oauthBrowserRateLimit` map to Manager (prepared for global rate limiting)

### Token Loading After OAuth
- [x] `RetryConnection` forces disconnect before reconnect
- [x] OAuth completion triggers reconnection via callback
- [x] `scanForNewTokens` checks for valid tokens before reconnecting

### Rate Limiting
- [x] Rate limit timestamp only updated on successful browser open
- [x] Failed browser opens don't update rate limit
- [x] Manual OAuth flows bypass rate limiting

## Files Modified

- `internal/upstream/manager.go` - Added OAuth checks, rate limit map
- `internal/upstream/managed/client.go` - Added `IsOAuthInProgress()` method
- `internal/upstream/core/connection.go` - Added public `IsOAuthInProgress()` method

## Test Files Created

- `internal/upstream/oauth_test_helpers.go` - Mock infrastructure
- `internal/upstream/oauth_invariants_test.go` - Invariant tests
- `internal/upstream/oauth_edge_cases_test.go` - Edge case tests
- `internal/upstream/oauth_real_code_test.go` - Real implementation tests

## Remaining Work

1. **Integration Testing**: Add E2E test with real OAuth server (mock)
2. **Metrics**: Add metrics for OAuth success/failure rates
3. **Logging**: Ensure all OAuth state transitions are logged
4. **Documentation**: Update CLAUDE.md with OAuth debugging guide
