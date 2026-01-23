# Fixes Summary

This document tracks the bug fixes implemented in this branch compared to `main`.

## 1. Header Secret Resolution (`internal/upstream/core/client.go`)

**Problem**: Server configuration headers containing secret references like `${keyring:pagerduty_token}` were not being resolved before being sent in HTTP requests. This caused servers like PagerDuty to fail with "Client is invalid or unknown" errors.

**Fix**: Added secret expansion for headers in `NewClientWithOptions()`:

```go
// Resolve secrets in headers (e.g., Authorization: Token ${keyring:token})
if len(resolvedServerConfig.Headers) > 0 {
    resolvedHeaders := make(map[string]string)
    for k, v := range resolvedServerConfig.Headers {
        resolvedValue, err := secretResolver.ExpandSecretRefs(ctx, v)
        // ... error handling and logging ...
        resolvedHeaders[k] = resolvedValue
    }
    resolvedServerConfig.Headers = resolvedHeaders
}
```

---

## 2. PersistentTokenStore Logger Fix (`internal/oauth/persistent_token_store.go`)

**Problem**: `PersistentTokenStore` used `zap.L()` to get a logger, which returns a no-op logger by default (since we never call `zap.ReplaceGlobals()`). This silently discarded all logging from token operations, making debugging impossible.

**Root Cause**: The zap library's global logger `zap.L()` returns a no-op logger until `zap.ReplaceGlobals()` is called with a configured logger.

**Fix**: Modified `NewPersistentTokenStore()` to accept an explicit logger parameter:

```go
func NewPersistentTokenStore(serverName, serverURL string, storage *storage.BoltDB, logger *zap.Logger) client.TokenStore {
    // Use provided logger, fallback to named global logger if nil
    if logger == nil {
        logger = zap.L()
    }
    logger = logger.Named("persistent-token-store").With(zap.String("server_key", serverKey))
    // ...
}
```

Updated all call sites in:
- `internal/oauth/config.go` - `CreateOAuthConfig()`
- `internal/upstream/manager.go` - Multiple locations for token checking

---

## 3. OAuth Token Loading on Reconnection (`internal/upstream/manager.go`)

**Problem**: Servers showed as "connected" in the UI, but tool calls failed with "no valid token available, authorization required". This occurred because:
1. OAuth completed successfully and tokens were persisted
2. Connection retry was triggered
3. Client appeared "connected" so retry was skipped
4. The HTTP client was never recreated with OAuth tokens loaded

**Fix**: Modified `RetryConnection()` to force reconnection when tokens exist:

```go
if client.IsConnected() {
    // Check if tokens exist in storage - if they do, force reconnection
    tokenManager := oauth.GetTokenStoreManager()
    hasRecentCompletion := tokenManager.HasRecentOAuthCompletion(serverName)
    
    // Check persistent storage for valid tokens
    if m.storage != nil {
        ts := oauth.NewPersistentTokenStore(...)
        if tok, err := ts.GetToken(...); err == nil && tok != nil {
            hasToken = true
            tokenValid = !tok.IsExpired()
        }
    }
    
    // Force reconnection if OAuth recently completed OR valid tokens exist
    if hasRecentCompletion || (hasToken && tokenValid) {
        // Force disconnect and reconnect to ensure HTTP client has tokens
    }
}
```

Also added a small delay (100ms) after disconnect to ensure HTTP client is fully closed before creating a new one.

---

## 4. OAuth Browser Spam Prevention (`internal/upstream/manager.go`)

**Problem**: Multiple OAuth browser windows would open repeatedly due to:
1. `scanForNewTokens()` triggering reconnections too aggressively
2. Reconnection attempts during active OAuth flows
3. Multiple simultaneous OAuth trigger points

**Fixes**:

### 4a. Check OAuth in-progress state before reconnection:
```go
if client.IsOAuthInProgress() {
    m.logger.Info("Skipping retry: OAuth already in progress", ...)
    return nil
}
```

### 4b. Rate limiting map for OAuth browser opens:
```go
oauthBrowserRateLimit   map[string]time.Time
oauthBrowserRateLimitMu sync.RWMutex
```

### 4c. Improved token scanning logic:
- Only trigger reconnection when tokens actually exist and are valid
- Skip servers with OAuth already in progress
- Rate-limit per-server (10s minimum between attempts)
- Check all enabled servers, not just those in Error state

---

## 5. OAuth Event Processing Improvements (`internal/upstream/manager.go`)

**Problem**: `processOAuthEvents()` wasn't properly handling cases where client appeared connected but didn't have tokens loaded.

**Fix**: Enhanced logic to verify tokens are actually loaded:

```go
} else if c.IsConnected() {
    // Client appears connected, but verify tokens are loaded
    if m.storage != nil {
        ts := oauth.NewPersistentTokenStore(...)
        if tok, err := ts.GetToken(...); err == nil && tok != nil {
            // Force reconnection to ensure HTTP client has tokens
            m.RetryConnection(event.ServerName)
        }
    }
}
```

---

## 6. Tool Call OAuth Error Recovery (`internal/upstream/manager.go`)

**Problem**: Tool calls failing with OAuth errors didn't trigger automatic recovery even when tokens existed in storage.

**Fix**: Added detection in `CallTool()` for "no valid token available" errors:

```go
if strings.Contains(errStr, "no valid token available") {
    // Check if tokens exist in storage - trigger reconnection if they do
    if m.storage != nil {
        ts := oauth.NewPersistentTokenStore(...)
        if tok, tokenErr := ts.GetToken(...); tokenErr == nil && tok != nil {
            go m.RetryConnection(serverName)
        }
    }
}
```

---

## 7. Increased Connection Timeout (`internal/upstream/manager.go`)

**Problem**: 30-second connection timeout was too short for servers requiring browser-based OAuth authentication.

**Fix**: Increased to 5 minutes:
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
```

---

## 8. Connection Flapping Fix (`internal/upstream/manager.go`)

**Problem**: Servers constantly showed "Connection Lost" and reconnected every 10 seconds, causing UI flapping and instability.

**Root Cause**: The `scanForNewTokens()` function was checking:
```go
shouldReconnect := hasRecentCompletion || (!tok.IsExpired() && tok.ExpiresAt.After(now))
```

The condition `(!tok.IsExpired() && tok.ExpiresAt.After(now))` is **always true** for valid tokens! This caused every server with valid tokens to be forcefully reconnected every 10 seconds.

**Fix**: Changed to ONLY reconnect when OAuth was recently completed (within 5 minutes):

```go
// Only force reconnection if OAuth was recently completed
if hasRecentCompletion {
    m.logger.Info("OAuth recently completed for connected server; forcing reconnect to load new tokens", ...)
    m.tokenReconnect[id] = now
    _ = m.RetryConnection(cfg.Name)
}
```

Also fixed `RetryConnection()` with the same logic - only force reconnection for already-connected servers when OAuth was recently completed.

---

## 10. Force Reconnection on Token Errors (`internal/upstream/manager.go`)

**Problem**: Tool calls would fail with "no valid token available" even though tokens existed in storage. The `CallTool` error handler triggered reconnection, but `RetryConnection()` skipped it because the client appeared "connected" and OAuth wasn't recently completed.

**Root Cause**: Fix #8 changed `RetryConnection()` to only reconnect when OAuth was recently completed:

```go
if hasRecentCompletion {
    // reconnect
} else {
    return nil // SKIP - this was the bug
}
```

When `CallTool` failed with a token error and tried to trigger reconnection, it was being skipped.

**Fix**: Added `RetryConnectionWithForce(serverName, force bool)` that accepts a `force` parameter:
- `force=false` (default): Only reconnect if OAuth was recently completed (prevents flapping)
- `force=true`: Always reconnect (used after tool call token errors to reload tokens)

```go
if hasRecentCompletion {
    // reconnect
} else if force {
    // reconnect (explicitly requested)
} else {
    return nil // skip
}
```

---

## 9. Stdio Transport Timeout Fix (`internal/runtime/supervisor/supervisor.go`)

**Problem**: Docker-isolated stdio servers (like `git` using `uvx mcp-server-git`) would fail with "context deadline exceeded" after exactly 30 seconds.

**Root Cause**: The supervisor's `executeAction()` function used a hardcoded 30-second timeout for all connection actions:

```go
ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
```

This was too short for Docker containers that need to:
1. Pull Docker images
2. Start containers  
3. Install packages via `uvx`/`npx`
4. Initialize MCP handshake

**Fix**: Increased the timeout to 5 minutes to match the `AddServer()` timeout:

```go
ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
```

---

## Debug Logging Added

For tracing token persistence issues, debug logging was added to:

1. `CreateOAuthConfig()` - logs storage nil check
2. `PersistentTokenStore.SaveToken()` - logs when called
3. `PersistentTokenStore.GetToken()` - logs token status

These can be removed once token persistence is confirmed stable.

---

## Testing

To verify OAuth token persistence is working:

1. Start the daemon: `./mcpproxy serve --log-level=debug`
2. Trigger OAuth for a server: `curl --unix-socket ~/.mcpproxy/mcpproxy.sock -X POST "http://localhost/api/v1/servers/atlassian/login"`
3. Complete OAuth in browser
4. Check logs for `persistent-token-store` entries:
   ```
   grep "persistent-token-store" ~/Library/Logs/mcpproxy/main.log
   ```
5. Should see:
   - `🔍 Loading OAuth token from persistent storage`
   - `✅ OAuth token is valid and not expiring soon`
   - `🔴 DEBUG: PersistentTokenStore.SaveToken CALLED!` (when new token saved)
