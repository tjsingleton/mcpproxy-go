---
# mcpproxy-go-ic3h
title: Implement hybrid OAuth token refresh timing
status: completed
type: task
priority: normal
created_at: 2026-01-20T14:46:47Z
updated_at: 2026-01-20T15:15:17Z
---

## Summary

Changed OAuth token refresh timing from pure percentage (80%) to a hybrid approach that provides better protection for short-lived tokens.

## Changes Made

### `internal/oauth/refresh_manager.go`

1. **New constant**: `MinRefreshBuffer = 5 * time.Minute`
   - Industry best practice minimum buffer before token expiry
   - Ensures adequate time for retries even with short-lived tokens

2. **Changed default threshold**: `DefaultRefreshThreshold = 0.75` (was 0.80)
   - More aggressive refresh for long-lived tokens

3. **Hybrid calculation in `scheduleRefreshLocked`**:
   ```go
   // Refresh at the EARLIER of:
   percentageDelay := lifetime * 0.75
   bufferDelay := lifetime - 5*time.Minute
   refreshDelay = min(percentageDelay, bufferDelay)
   ```

4. **Enhanced logging**: Added `buffer` and `strategy` fields to understand which approach was used

### `internal/oauth/refresh_manager_test.go`

1. Renamed `TestRefreshManager_ScheduleAt80PercentLifetime` → `TestRefreshManager_HybridRefreshStrategy`
2. Added `TestRefreshManager_BufferBasedStrategy` for short-lived token scenario

## Results by Token Lifetime

| Token Lifetime | Old (80%) | New (Hybrid) | Buffer |
|----------------|-----------|--------------|--------|
| 1 hour | 48 min (12 min buffer) | 45 min (15 min buffer) | +3 min |
| 30 min | 24 min (6 min buffer) | 25 min (5 min buffer) | -1 min |
| 10 min | 8 min (2 min buffer) | 5 min (5 min buffer) | +3 min |
| 5 min | 4 min (1 min buffer) | 0 min (5 min buffer) | +4 min |

## Testing

- ✅ `TestRefreshManager_HybridRefreshStrategy` - verifies 1-hour tokens use 75% (percentage-based)
- ✅ `TestRefreshManager_BufferBasedStrategy` - verifies 10-min tokens use 5-min buffer
- ✅ All existing refresh manager tests pass
- ✅ Linter passes with 0 issues

## Related

- Parent: mcpproxy-go-daji (OAuth monitoring bean)