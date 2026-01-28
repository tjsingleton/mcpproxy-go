# dev-mcpproxy.sh Doctor Command Design

**Date**: 2025-01-27
**Status**: Ready for implementation

## Overview

Add a `doctor` subcommand to `scripts/dev-mcpproxy.sh` that runs comprehensive diagnostics for development workflows. Unlike `mcpproxy doctor` (which requires the daemon), this works in all states.

## Decisions

| Question | Decision |
|----------|----------|
| Focus | Full health suite (pre-flight + runtime) |
| OAuth depth | Status + token details (no test calls) |
| Process checks | Git-aware (branch/commit comparison) |
| Output style | Checklist with ✓/✗ markers |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All checks pass |
| `1` | One or more failures |
| `2` | Warnings only (degraded but functional) |

## Usage

```bash
./scripts/dev-mcpproxy.sh doctor
```

## Output Format

```
[mcpproxy] Running diagnostics...

Binary & Build:
  ✓ mcpproxy-tray binary exists
  · Worktree: fix/oauth-proactive-refresh (d7cfb84)

Process:
  ✓ Daemon running (PID: 12345)
  ✓ Binary: /path/to/mcpproxy-tray
  ✓ Branch matches worktree

Connectivity:
  ✓ Socket available
  ✓ API responding

Upstream Servers:
  · 15 total, 12 connected, 2 connecting, 1 error
  ✗ sequential-thinking: Error - context deadline exceeded

OAuth Status:
  ✗ glean_agents: Not logged in
  ! atlassian: Token expires in 45m (warning)
  ✓ amplitude: Logged in (expires in 3h 15m)

[mcpproxy] 2 issues found
```

## Implementation

### Helper Functions

```bash
# API call via Unix socket (bypasses auth)
api_call() {
    local endpoint="$1"
    curl -s --unix-socket "${MCPPROXY_DATA_DIR}/mcpproxy.sock" \
        "http://localhost${endpoint}" 2>/dev/null
}

# Output formatting
check_pass() { echo -e "  ${GREEN}✓${NC} $1"; }
check_fail() { echo -e "  ${RED}✗${NC} $1"; FAILURES=$((FAILURES + 1)); }
check_warn() { echo -e "  ${YELLOW}!${NC} $1"; WARNINGS=$((WARNINGS + 1)); }
check_info() { echo -e "  ${CYAN}·${NC} $1"; }
```

### Check Functions

#### 1. Binary & Build Checks

```bash
check_binary() {
    log "Binary & Build:"

    local tray_binary="${REPO_ROOT}/mcpproxy-tray"

    if [[ -x "${tray_binary}" ]]; then
        check_pass "mcpproxy-tray binary exists"
    else
        check_fail "mcpproxy-tray binary not found (run 'make build')"
    fi

    local worktree_commit=$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null)
    local worktree_branch=$(git -C "${REPO_ROOT}" rev-parse --abbrev-ref HEAD 2>/dev/null)

    if [[ -n "${worktree_branch}" ]]; then
        check_info "Worktree: ${worktree_branch} (${worktree_commit})"
    fi
}
```

#### 2. Process Checks

```bash
check_process() {
    log "Process:"

    cleanup_stale_pid

    if ! is_running; then
        check_fail "Daemon not running"
        return 1
    fi

    local pid=$(get_pid)
    check_pass "Daemon running (PID: ${pid})"

    # Get binary path from process
    local binary_path=$(ps -p ${pid} -o command= 2>/dev/null | awk '{print $1}')
    if [[ -n "${binary_path}" ]]; then
        check_info "Binary: ${binary_path}"

        # Compare branches
        local binary_dir=$(dirname "${binary_path}")
        local running_branch=$(git -C "${binary_dir}" rev-parse --abbrev-ref HEAD 2>/dev/null)
        local worktree_branch=$(git -C "${REPO_ROOT}" rev-parse --abbrev-ref HEAD 2>/dev/null)

        if [[ "${running_branch}" == "${worktree_branch}" ]]; then
            check_pass "Branch matches worktree"
        else
            check_warn "Running ${running_branch}, worktree is ${worktree_branch}"
        fi
    fi

    return 0
}
```

#### 3. Connectivity Checks

```bash
check_connectivity() {
    log "Connectivity:"

    local socket="${MCPPROXY_DATA_DIR}/mcpproxy.sock"

    if [[ -S "${socket}" ]]; then
        check_pass "Socket available"
    else
        check_fail "Socket not found at ${socket}"
        return 1
    fi

    local status=$(api_call "/api/v1/status")
    if [[ -n "${status}" ]] && echo "${status}" | jq -e '.success' >/dev/null 2>&1; then
        check_pass "API responding"
    else
        check_fail "API not responding"
        return 1
    fi

    return 0
}
```

#### 4. Upstream Server Checks

```bash
check_upstream() {
    log "Upstream Servers:"

    local status=$(api_call "/api/v1/status")
    if [[ -z "${status}" ]]; then
        check_fail "Could not get server status"
        return 1
    fi

    local total=$(echo "${status}" | jq -r '.data.upstream_stats.total_servers // 0')
    local connected=$(echo "${status}" | jq -r '.data.upstream_stats.connected_servers // 0')
    local connecting=$(echo "${status}" | jq -r '.data.upstream_stats.connecting_servers // 0')
    local errors=$((total - connected - connecting))

    check_info "${total} total, ${connected} connected, ${connecting} connecting, ${errors} error"

    # Show servers in error state
    echo "${status}" | jq -r '
        .data.upstream_stats.servers | to_entries[] |
        select(.value.state == "Error") |
        "\(.key): \(.value.last_error // "unknown error")"
    ' 2>/dev/null | while read -r line; do
        local name=$(echo "${line}" | cut -d: -f1)
        local err=$(echo "${line}" | cut -d: -f2-)
        check_fail "${name}:${err}"
    done
}
```

#### 5. OAuth Status Checks

```bash
check_oauth() {
    log "OAuth Status:"

    local servers=$(api_call "/api/v1/servers")
    if [[ -z "${servers}" ]]; then
        check_info "Could not get OAuth status"
        return
    fi

    # Filter to OAuth-enabled servers
    echo "${servers}" | jq -r '
        .data[]? |
        select(.oauth != null or .health.action == "login") |
        "\(.name)|\(.health.level // "unknown")|\(.health.summary // "")|\(.health.action // "")"
    ' 2>/dev/null | while IFS='|' read -r name level summary action; do
        case "${level}" in
            healthy)
                check_pass "${name}: ${summary}"
                ;;
            degraded)
                check_warn "${name}: ${summary}"
                ;;
            unhealthy)
                if [[ "${action}" == "login" ]]; then
                    check_fail "${name}: Not logged in"
                else
                    check_fail "${name}: ${summary}"
                fi
                ;;
            *)
                check_info "${name}: ${summary:-unknown status}"
                ;;
        esac
    done
}
```

### Main Doctor Function

```bash
run_doctor() {
    FAILURES=0
    WARNINGS=0

    log "Running diagnostics..."
    echo ""

    check_binary
    echo ""

    if check_process; then
        echo ""
        check_connectivity
        echo ""
        check_upstream
        echo ""
        check_oauth
    fi

    echo ""
    if [[ ${FAILURES} -gt 0 ]]; then
        error "${FAILURES} issue(s) found"
        exit 1
    elif [[ ${WARNINGS} -gt 0 ]]; then
        warn "${WARNINGS} warning(s)"
        exit 2
    else
        log "All checks passed"
        exit 0
    fi
}
```

### Case Statement Addition

```bash
case "${ARG}" in
    # ... existing cases ...
    doctor)
        run_doctor
        ;;
    # ... rest of cases ...
esac
```

## Dependencies

- `jq` for JSON parsing
- `curl` for API calls
- Existing script functions: `is_running`, `get_pid`, `cleanup_stale_pid`

## Related

- [mcpproxy-go-34y](mcpproxy-go-34y): Add socket-based API helpers to dev-mcpproxy.sh
- [mcpproxy-go-3yx](mcpproxy-go-3yx): 8-hour OAuth validation (discovered need for doctor)
