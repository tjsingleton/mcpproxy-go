# OAuth Proactive Refresh Fix

**Status**: Ready for Review
**Priority**: P1 (OAuth tokens expire, requiring manual re-authentication)
**Branch**: To be created from `upstream/main`

## Problem Statement

Proactive OAuth token refresh does not work. When tokens approach expiration, the RefreshManager schedules a refresh but it never executes, requiring users to manually re-authenticate via the tray menu.

**Observed behavior** (from logs):
1. 17:34:32 - Glean connected successfully with valid token
2. 17:50-17:56 - Tool calls worked fine
3. 18:07:40 - Token became invalid ("no valid token available")
4. 18:08-18:58 - Multiple reconnection attempts all failed
5. User had to manually click "OAuth Login" from tray menu

## Root Cause Analysis

### The Refresh Flow (Current)

```
RefreshManager timer fires
    ↓
Manager.RefreshOAuthToken(serverName)
    ↓
client.ForceReconnect("oauth_token_refresh")
    ↓
ForceReconnect checks: mc.IsConnected()
    ↓
Returns early if already connected ← BUG: refresh never happens
```

### Why ForceReconnect Doesn't Work

**File**: `internal/upstream/managed/client.go` (~L296-302)

```go
func (mc *Client) ForceReconnect(reason string) {
    if mc.IsConnected() {
        mc.logger.Debug("Force reconnect skipped - client already connected",
            zap.String("server", serverName),
            zap.String("reason", reason))
        return  // ← Early return, no refresh!
    }
    // ... reconnection logic
}
```

Even if reconnection occurred, mcp-go's OAuth handler only refreshes when `token.IsExpired()` returns true:

**File**: mcp-go `client/transport/oauth.go` (L184-191)

```go
func (h *OAuthHandler) getValidToken(ctx context.Context) (*Token, error) {
    token, err := h.config.TokenStore.GetToken(ctx)
    // ...
    if err == nil && !token.IsExpired() && token.AccessToken != "" {
        return token, nil  // ← Uses existing token, no refresh
    }
    // Only refreshes if token.IsExpired() == true
}
```

## Solution: Direct RefreshToken Call

Bypass the reconnect mechanism entirely by calling `OAuthHandler.RefreshToken()` directly.

### Implementation

**1. Add transport accessor to core.Client**

```go
// File: internal/upstream/core/client.go

// GetOAuthHandler returns the OAuth handler if the transport supports OAuth.
// Returns nil if no OAuth handler is configured or transport doesn't support OAuth.
func (c *Client) GetOAuthHandler() *transport.OAuthHandler {
    c.mu.RLock()
    mcpClient := c.client
    c.mu.RUnlock()

    if mcpClient == nil {
        return nil
    }

    // Type assert to get OAuth handler from transport
    type oauthTransport interface {
        GetOAuthHandler() *transport.OAuthHandler
    }

    t := mcpClient.GetTransport()
    if ot, ok := t.(oauthTransport); ok {
        return ot.GetOAuthHandler()
    }
    return nil
}
```

**2. Add direct refresh method to core.Client**

```go
// File: internal/upstream/core/client.go

// RefreshOAuthTokenDirect forces an OAuth token refresh without reconnecting.
// This is used by the RefreshManager for proactive token refresh.
func (c *Client) RefreshOAuthTokenDirect(ctx context.Context) error {
    handler := c.GetOAuthHandler()
    if handler == nil {
        return fmt.Errorf("no OAuth handler available for %s", c.config.Name)
    }

    // Get current refresh token from storage
    serverKey := oauth.GenerateServerKey(c.config.Name, c.config.URL)
    record, err := c.storage.GetOAuthToken(serverKey)
    if err != nil {
        return fmt.Errorf("failed to get stored token: %w", err)
    }
    if record.RefreshToken == "" {
        return fmt.Errorf("no refresh token available for %s", c.config.Name)
    }

    c.logger.Info("Executing direct OAuth token refresh",
        zap.String("server", c.config.Name))

    // Call mcp-go's RefreshToken directly - this bypasses IsExpired() check
    _, err = handler.RefreshToken(ctx, record.RefreshToken)
    if err != nil {
        return fmt.Errorf("OAuth refresh failed: %w", err)
    }

    c.logger.Info("Direct OAuth token refresh completed successfully",
        zap.String("server", c.config.Name))

    return nil
}
```

**3. Expose through managed.Client**

```go
// File: internal/upstream/managed/client.go

// RefreshOAuthTokenDirect delegates to core client for direct token refresh.
func (mc *Client) RefreshOAuthTokenDirect(ctx context.Context) error {
    return mc.coreClient.RefreshOAuthTokenDirect(ctx)
}
```

**4. Update Manager.RefreshOAuthToken**

```go
// File: internal/upstream/manager.go

func (m *Manager) RefreshOAuthToken(serverName string) error {
    client := m.GetClient(serverName)
    if client == nil {
        return fmt.Errorf("server not found: %s", serverName)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Use direct refresh instead of ForceReconnect
    err := client.RefreshOAuthTokenDirect(ctx)
    if err != nil {
        m.logger.Warn("Direct OAuth refresh failed, falling back to reconnect",
            zap.String("server", serverName),
            zap.Error(err))
        // Fall back to reconnect for cases where direct refresh isn't available
        client.ForceReconnect("oauth_token_refresh_fallback")
        return err
    }

    return nil
}
```

## Benefits Over PR #274's Approach

| Aspect | PR #274 (Token Expiry Marking) | This Solution (Direct Refresh) |
|--------|-------------------------------|-------------------------------|
| Data integrity | Writes fake expiry to storage | No data mutation |
| Race conditions | Window between mark and refresh | None |
| Rollback on failure | Token stays "expired" | Original token unchanged |
| Debugging | Confusing expiry times in logs | Clear, semantic calls |
| Code clarity | Workaround | Direct intent |

## Testing Strategy

### Unit Tests

1. `TestGetOAuthHandler_ReturnsHandler` - Verify handler extraction works
2. `TestGetOAuthHandler_ReturnsNil_NoOAuth` - Verify nil for non-OAuth servers
3. `TestRefreshOAuthTokenDirect_Success` - Mock successful refresh
4. `TestRefreshOAuthTokenDirect_NoRefreshToken` - Error when no refresh token
5. `TestRefreshOAuthTokenDirect_RefreshFails` - Error propagation

### Integration Tests

1. Test with mock OAuth server that returns new tokens
2. Verify RefreshManager triggers direct refresh at scheduled time
3. Verify token is updated in storage after refresh

### Manual Testing

1. Configure Glean with OAuth
2. Authenticate and get short-lived token
3. Wait for proactive refresh timer
4. Verify token refreshes without user intervention
5. Verify tool calls continue working

## Files Changed

| File | Change |
|------|--------|
| `internal/upstream/core/client.go` | Add `GetOAuthHandler()`, `RefreshOAuthTokenDirect()` |
| `internal/upstream/managed/client.go` | Add `RefreshOAuthTokenDirect()` delegation |
| `internal/upstream/manager.go` | Update `RefreshOAuthToken()` to use direct refresh |
| `internal/upstream/core/client_test.go` | Add unit tests |

## Commit Plan

```
fix(oauth): implement direct token refresh for proactive refresh

The proactive refresh mechanism was not working because:
1. ForceReconnect returns early if client is already connected
2. mcp-go only refreshes tokens that have actually expired

This change adds RefreshOAuthTokenDirect() which calls the OAuth
handler's RefreshToken() method directly, bypassing both issues.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
```

## Open Questions

1. Should we remove the fallback to ForceReconnect, or keep it for edge cases?
2. Should we add metrics/logging for proactive refresh success/failure rates?
3. Should the RefreshManager retry on failure before falling back?
