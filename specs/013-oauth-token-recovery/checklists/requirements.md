# Requirements Checklist: OAuth Token Recovery

## Functional Requirements

- [x] **FR-013-001**: System MUST check token storage when tool call fails with token error
  - Implementation: `manager.go:CallTool()` checks for "no valid token available" error
- [x] **FR-013-002**: System MUST trigger forced reconnection when tokens exist but HTTP client doesn't have them
  - Implementation: `manager.go:CallTool()` calls `RetryConnectionWithForce(serverName, true)`
- [x] **FR-013-003**: System MUST support `RetryConnectionWithForce(serverName, force bool)` API
  - Implementation: `manager.go:RetryConnectionWithForce()`
- [x] **FR-013-004**: System MUST track browser open timestamps per server in `oauthBrowserRateLimit`
  - Implementation: `manager.go` field `oauthBrowserRateLimit map[string]time.Time`
- [x] **FR-013-005**: System MUST skip OAuth if browser was opened within 10 seconds for same server
  - Implementation: `scanForNewTokens()` rate limit check
- [x] **FR-013-006**: System MUST use 5-minute timeout for AddServer() connection
  - Implementation: `manager.go:AddServer()` line ~355
- [x] **FR-013-007**: System MUST check `IsOAuthInProgress()` before attempting OAuth
  - Implementation: `scanForNewTokens()` and `RetryConnectionWithForce()`
- [x] **FR-013-008**: `scanForNewTokens()` MUST check all enabled servers
  - Implementation: Removed `!c.IsConnected()` and `StateError` filters

## Non-Functional Requirements

- [x] **NFR-013-001**: Token recovery MUST be async (not block caller)
  - Implementation: `go func() { m.RetryConnectionWithForce(...) }()`
- [x] **NFR-013-002**: Browser rate limit window MUST be 10 seconds minimum
  - Implementation: `now.Sub(last) < 10*time.Second`
- [ ] **NFR-013-003**: Force reconnection MUST complete within 30 seconds
  - Needs verification: May need explicit timeout
- [x] **NFR-013-004**: Token recovery MUST be idempotent
  - Implementation: Rate limiting prevents duplicate attempts

## Test Coverage

### Unit Tests

- [ ] `TestCallTool_TokenError_TriggersRecovery`
- [ ] `TestCallTool_TokenError_NoTokens_NoRecovery`
- [ ] `TestCallTool_NonTokenError_NoRecovery`
- [ ] `TestOAuthBrowserRateLimit_PreventsDuplicateOpens`
- [ ] `TestOAuthBrowserRateLimit_AllowsAfterExpiry`
- [ ] `TestOAuthBrowserRateLimit_PerServer`
- [ ] `TestRetryConnectionWithForce_True_ReconnectsConnected`
- [ ] `TestRetryConnectionWithForce_False_SkipsConnected`
- [ ] `TestRetryConnectionWithForce_RecentOAuth_Reconnects`
- [ ] `TestAddServer_OAuthServer_5MinuteTimeout`
- [ ] `TestScanForNewTokens_ChecksAllEnabled`
- [ ] `TestScanForNewTokens_SkipsOAuthInProgress`

### Integration Tests

- [ ] `TestOAuthRecovery_EndToEnd`
- [ ] `TestOAuthRecovery_WithRefreshManager`
- [ ] `TestOAuthRecovery_NoBrowserSpam`

### Invariant Tests

- [ ] `TestInvariant_013_001_TokenErrorRecovery`
- [ ] `TestInvariant_013_002_BrowserRateLimitIndependent`
- [ ] `TestInvariant_013_003_ForceReconnectAvailable`
- [ ] `TestInvariant_013_004_TimeoutAdequate`
- [ ] `TestInvariant_013_005_NoRefreshManagerInterference`

## Implementation Status

| Component | Implemented | Tested | Reviewed |
|-----------|:-----------:|:------:|:--------:|
| `oauthBrowserRateLimit` map | ✅ | ❌ | ❌ |
| `RetryConnectionWithForce()` | ✅ | ❌ | ❌ |
| CallTool token error recovery | ✅ | ❌ | ❌ |
| AddServer 5-min timeout | ✅ | ❌ | ❌ |
| `scanForNewTokens()` improvements | ✅ | ❌ | ❌ |

## Notes

- All implementation is currently in uncommitted changes (stash pop from `docs/add-configuration-reference` branch)
- Need to verify compatibility with upstream's `RefreshManager`
- Tests should be written before committing changes (TDD approach)



