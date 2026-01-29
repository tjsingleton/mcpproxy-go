#!/bin/bash
# MCPProxy Daemon Script (mcpd)
# Manages mcpproxy-tray daemon with worktree/branch switching support
#
# Usage:
#   scripts/mcpd.sh                              # Build and start from current directory
#   scripts/mcpd.sh main                         # Build and start from .worktrees/main
#   scripts/mcpd.sh snapshot/recovery-2026-01-22 # Build from that worktree
#   scripts/mcpd.sh stop                         # Stop running daemon
#   scripts/mcpd.sh status                       # Show status
#   scripts/mcpd.sh doctor                       # Run diagnostic checks
#
# Dev Mode (isolated from local production):
#   scripts/mcpd.sh dev                          # Start isolated instance (port 8081)
#   scripts/mcpd.sh dev --config path/to/cfg.json # Start with custom config
#   scripts/mcpd.sh dev stop                     # Stop dev instance
#   scripts/mcpd.sh dev logs                     # Tail dev logs
#   scripts/mcpd.sh dev clean                    # Reset dev environment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR%/scripts}"

# Mode: "prod" (default) or "dev"
MODE="prod"

# Production paths (default)
MCPPROXY_DATA_DIR="${HOME}/.mcpproxy"
PID_FILE="${MCPPROXY_DATA_DIR}/mcpproxy-tray.pid"
LOG_FILE="${MCPPROXY_DATA_DIR}/daemon.log"

# Dev mode paths (isolated, in-repo, gitignored)
DEV_DATA_DIR="${REPO_ROOT}/.dev"
DEV_PID_FILE="${DEV_DATA_DIR}/mcpproxy.pid"
DEV_LOG_FILE="${DEV_DATA_DIR}/mcpproxy.log"
DEV_SOCKET="${DEV_DATA_DIR}/mcpproxy.sock"
DEV_CONFIG="${DEV_DATA_DIR}/mcp_config.json"
DEV_PORT="8081"

# Daemon flags - override with MCPPROXY_FLAGS env var
MCPPROXY_DEFAULT_FLAGS="--enable-prompts"
MCPPROXY_FLAGS="${MCPPROXY_FLAGS:-$MCPPROXY_DEFAULT_FLAGS}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${GREEN}[mcpproxy]${NC} $1"; }
warn() { echo -e "${YELLOW}[mcpproxy]${NC} $1"; }
error() { echo -e "${RED}[mcpproxy]${NC} $1" >&2; }
info() { echo -e "${CYAN}[mcpproxy]${NC} $1"; }

# Doctor command helpers
FAILURES=0
WARNINGS=0

check_pass() { echo -e "  ${GREEN}✓${NC} $1"; }
check_fail() { echo -e "  ${RED}✗${NC} $1"; FAILURES=$((FAILURES + 1)); }
check_warn() { echo -e "  ${YELLOW}!${NC} $1"; WARNINGS=$((WARNINGS + 1)); }
check_info() { echo -e "  ${CYAN}·${NC} $1"; }

# API call via Unix socket (bypasses auth)
api_call() {
    local endpoint="$1"
    curl -s --unix-socket "${MCPPROXY_DATA_DIR}/mcpproxy.sock" \
        "http://localhost${endpoint}" 2>/dev/null
}

mkdir -p "${MCPPROXY_DATA_DIR}"

get_pid() { [[ -f "${PID_FILE}" ]] && cat "${PID_FILE}"; }

is_running() {
    local pid=$(get_pid)
    [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null
}

find_running_processes() {
    # Find both tray and core processes
    { pgrep -f "mcpproxy-tray" 2>/dev/null; pgrep -f "mcpproxy serve" 2>/dev/null; } | sort -u || true
}

kill_all() {
    local pids=$(find_running_processes)
    if [[ -n "${pids}" ]]; then
        log "Killing mcpproxy processes: ${pids}"
        echo "${pids}" | xargs kill -9 2>/dev/null || true
        sleep 1
    fi
}

cleanup_stale_pid() {
    local pid=$(get_pid)
    if [[ -n "${pid}" ]] && ! kill -0 "${pid}" 2>/dev/null; then
        warn "Cleaning up stale PID file (process ${pid} not running)"
        rm -f "${PID_FILE}"
    fi
}

# Resolve worktree/branch to directory
resolve_build_dir() {
    local arg="$1"

    if [[ -z "$arg" ]]; then
        echo "$REPO_ROOT"
        return
    fi

    # Check if it's a worktree directory name
    if [[ -d "$REPO_ROOT/.worktrees/$arg" ]]; then
        echo "$REPO_ROOT/.worktrees/$arg"
        return
    fi

    # Check if current dir is on that branch
    local current_branch=$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    if [[ "$current_branch" == "$arg" ]]; then
        echo "$REPO_ROOT"
        return
    fi

    # Search worktrees for matching branch
    for wt in "$REPO_ROOT/.worktrees"/*; do
        if [[ -d "$wt" ]]; then
            local wt_branch=$(git -C "$wt" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
            if [[ "$wt_branch" == "$arg" ]]; then
                echo "$wt"
                return
            fi
        fi
    done

    # Not found
    echo ""
}

stop_daemon() {
    cleanup_stale_pid

    if ! is_running; then
        warn "mcpproxy-tray is not running"
        local orphans=$(find_running_processes)
        if [[ -n "${orphans}" ]]; then
            warn "Killing orphaned processes: ${orphans}"
            kill_all
        fi
        rm -f "${PID_FILE}"
        return 0
    fi

    local pid=$(get_pid)
    log "Stopping mcpproxy-tray (PID: ${pid})..."

    kill "${pid}" 2>/dev/null || true

    local count=0
    while kill -0 "${pid}" 2>/dev/null && [[ ${count} -lt 10 ]]; do
        sleep 1
        count=$((count + 1))
    done

    if kill -0 "${pid}" 2>/dev/null; then
        warn "Force killing..."
        kill -9 "${pid}" 2>/dev/null || true
    fi

    kill_all
    rm -f "${PID_FILE}"
    log "mcpproxy-tray stopped"
}

start_daemon() {
    local build_dir="$1"
    local tray_binary="${build_dir}/mcpproxy-tray"

    if [[ ! -x "${tray_binary}" ]]; then
        error "mcpproxy-tray not found at ${tray_binary}"
        error "Run 'make build' first"
        exit 1
    fi

    cleanup_stale_pid

    if is_running; then
        local pid=$(get_pid)
        warn "mcpproxy-tray already running (PID: ${pid}), stopping first..."
        stop_daemon
        sleep 2
    fi

    local orphans=$(find_running_processes)
    if [[ -n "${orphans}" ]]; then
        warn "Killing orphaned processes: ${orphans}"
        kill_all
        sleep 2
    fi

    log "Starting mcpproxy-tray from ${build_dir}..."
    [[ -n "${MCPPROXY_FLAGS}" ]] && info "Flags: ${MCPPROXY_FLAGS}"
    cd "${build_dir}"
    # shellcheck disable=SC2086
    nohup "${tray_binary}" ${MCPPROXY_FLAGS} >> "${LOG_FILE}" 2>&1 &
    local pid=$!
    echo "${pid}" > "${PID_FILE}"

    sleep 3

    if is_running; then
        log "mcpproxy-tray started (PID: ${pid})"
        info "Branch: $(git -C "${build_dir}" rev-parse --abbrev-ref HEAD)"
        info "Commit: $(git -C "${build_dir}" rev-parse --short HEAD)"
        info "Log: ${LOG_FILE}"
    else
        error "mcpproxy-tray failed to start"
        error "Check: ${LOG_FILE}"
        rm -f "${PID_FILE}"
        exit 1
    fi
}

show_status() {
    cleanup_stale_pid

    if is_running; then
        local pid=$(get_pid)
        log "mcpproxy-tray is running (PID: ${pid})"

        # Try to find which binary is running
        local binary_path=$(ps -p ${pid} -o command= 2>/dev/null | awk '{print $1}')
        if [[ -n "${binary_path}" ]]; then
            local binary_dir=$(dirname "${binary_path}")
            info "Binary: ${binary_path}"
            if [[ -d "${binary_dir}/.git" ]] || git -C "${binary_dir}" rev-parse --git-dir >/dev/null 2>&1; then
                info "Branch: $(git -C "${binary_dir}" rev-parse --abbrev-ref HEAD 2>/dev/null || echo 'unknown')"
                info "Commit: $(git -C "${binary_dir}" rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
            fi
        fi
    else
        warn "mcpproxy-tray is not running"
        local orphans=$(find_running_processes)
        if [[ -n "${orphans}" ]]; then
            warn "Found orphaned processes: ${orphans}"
        fi
    fi
}

show_logs() {
    if [[ -f "${LOG_FILE}" ]]; then
        tail -f "${LOG_FILE}"
    else
        error "Log file not found: ${LOG_FILE}"
        exit 1
    fi
}

# Doctor command check functions
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

    # Show servers in error state (use process substitution to avoid subshell)
    while IFS='|' read -r name err; do
        [[ -z "${name}" ]] && continue
        # Truncate long error messages
        if [[ ${#err} -gt 57 ]]; then
            err="${err:0:57}..."
        fi
        check_fail "${name}: ${err:-unknown error}"
    done < <(echo "${status}" | jq -r '
        .data.upstream_stats.servers | to_entries[] |
        select(.value.state == "Error") |
        "\(.key)|\(.value.last_error // "unknown error" | gsub("[\\n\\r]"; " ") | gsub("<[^>]*>"; "") | gsub("  +"; " "))"
    ' 2>/dev/null)
}

check_oauth() {
    log "OAuth Status:"

    local servers=$(api_call "/api/v1/servers")
    if [[ -z "${servers}" ]]; then
        check_info "Could not get OAuth status"
        return
    fi

    # Filter to OAuth-enabled servers and check their health
    local oauth_output=$(echo "${servers}" | jq -r '
        .data.servers[]? |
        select(.oauth != null) |
        "\(.name)|\(.health.level // "unknown")|\(.health.summary // "")|\(.health.action // "")|\(.oauth.token_expires_at // "")|\(.oauth.token_valid // false)"
    ' 2>/dev/null)

    if [[ -z "${oauth_output}" ]]; then
        check_info "No OAuth servers configured"
        return
    fi

    # Use process substitution to avoid subshell
    while IFS='|' read -r name level summary action expires_at token_valid; do
        [[ -z "${name}" ]] && continue

        # Build status message with expiry info
        local msg="${summary:-unknown}"
        if [[ -n "${expires_at}" ]] && [[ "${expires_at}" != "null" ]]; then
            # Calculate time until expiry
            local expires_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%S" "${expires_at%[-+]*}" "+%s" 2>/dev/null || echo "0")
            local now_epoch=$(date "+%s")
            local diff=$((expires_epoch - now_epoch))
            if [[ ${diff} -gt 0 ]]; then
                local hours=$((diff / 3600))
                local mins=$(((diff % 3600) / 60))
                if [[ ${hours} -gt 0 ]]; then
                    msg="Token expires in ${hours}h ${mins}m"
                else
                    msg="Token expires in ${mins}m"
                fi
            fi
        fi

        case "${level}" in
            healthy)
                if [[ "${token_valid}" == "true" ]]; then
                    check_pass "${name}: ${msg}"
                else
                    check_info "${name}: ${msg}"
                fi
                ;;
            degraded)
                check_warn "${name}: ${msg}"
                ;;
            unhealthy)
                if [[ "${action}" == "login" ]]; then
                    check_fail "${name}: Not logged in"
                else
                    check_fail "${name}: ${msg}"
                fi
                ;;
            *)
                check_info "${name}: ${msg}"
                ;;
        esac
    done <<< "${oauth_output}"
}

run_doctor() {
    FAILURES=0
    WARNINGS=0

    log "Running diagnostics..."
    echo ""

    check_binary
    echo ""

    if check_process; then
        echo ""
        if check_connectivity; then
            echo ""
            check_upstream
            echo ""
            check_oauth
        fi
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

#
# Dev Mode Functions
#

dev_ensure_dirs() {
    mkdir -p "${DEV_DATA_DIR}/logs" "${DEV_DATA_DIR}/data"
}

dev_create_default_config() {
    if [[ ! -f "${DEV_CONFIG}" ]]; then
        log "Creating default dev config..."
        cat > "${DEV_CONFIG}" << DEVCONFIG
{
  "listen": "127.0.0.1:${DEV_PORT}",
  "api_key": "dev-test-key",
  "enable_socket": true,
  "enable_web_ui": true,
  "data_dir": "${DEV_DATA_DIR}",
  "tray_endpoint": "${DEV_SOCKET}",
  "mcpServers": [
    {
      "name": "time",
      "command": "npx",
      "args": ["-y", "@anthropic-ai/time-mcp"],
      "protocol": "stdio",
      "enabled": true
    },
    {
      "name": "sequential-thinking",
      "command": "npx",
      "args": ["-y", "@anthropic-ai/sequential-thinking-mcp"],
      "protocol": "stdio",
      "enabled": true
    }
  ]
}
DEVCONFIG
    fi
}

dev_get_pid() {
    [[ -f "${DEV_PID_FILE}" ]] && cat "${DEV_PID_FILE}"
}

dev_is_running() {
    local pid=$(dev_get_pid)
    [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null
}

dev_start() {
    local config_file="${1:-}"
    local binary="${REPO_ROOT}/mcpproxy"

    dev_ensure_dirs

    # Determine config file
    if [[ -n "${config_file}" ]]; then
        if [[ ! -f "${config_file}" ]]; then
            error "Config file not found: ${config_file}"
            exit 1
        fi
        DEV_CONFIG="$(cd "$(dirname "${config_file}")" && pwd)/$(basename "${config_file}")"
    else
        dev_create_default_config
    fi

    if [[ ! -x "${binary}" ]]; then
        error "Binary not found: ${binary}"
        error "Run 'make build' first"
        exit 1
    fi

    if dev_is_running; then
        warn "Dev daemon already running (PID: $(dev_get_pid))"
        return 0
    fi

    # Clean up stale socket
    rm -f "${DEV_SOCKET}"

    log "Starting mcpproxy in dev mode..."
    info "Port: ${DEV_PORT}"
    info "Config: ${DEV_CONFIG}"
    info "Socket: ${DEV_SOCKET}"
    info "Log: ${DEV_LOG_FILE}"

    # Start core directly (not tray) with explicit config path
    nohup "${binary}" serve \
        --config "${DEV_CONFIG}" \
        --log-level debug \
        >> "${DEV_LOG_FILE}" 2>&1 &

    local pid=$!
    echo "${pid}" > "${DEV_PID_FILE}"

    sleep 2

    if dev_is_running; then
        log "Dev daemon started (PID: ${pid})"

        # Wait for healthz
        local count=0
        while [[ ${count} -lt 10 ]]; do
            if curl -s "http://127.0.0.1:${DEV_PORT}/healthz" >/dev/null 2>&1; then
                log "Dev server is ready on http://127.0.0.1:${DEV_PORT}"
                info "Web UI: http://127.0.0.1:${DEV_PORT}/ui/"
                return 0
            fi
            sleep 1
            count=$((count + 1))
        done
        warn "Server started but healthz not responding yet"
    else
        error "Dev daemon failed to start"
        error "Check: ${DEV_LOG_FILE}"
        rm -f "${DEV_PID_FILE}"
        exit 1
    fi
}

dev_stop() {
    if ! dev_is_running; then
        warn "Dev daemon is not running"
        rm -f "${DEV_PID_FILE}"
        return 0
    fi

    local pid=$(dev_get_pid)
    log "Stopping dev daemon (PID: ${pid})..."
    kill "${pid}" 2>/dev/null || true

    local count=0
    while kill -0 "${pid}" 2>/dev/null && [[ ${count} -lt 5 ]]; do
        sleep 1
        count=$((count + 1))
    done

    if kill -0 "${pid}" 2>/dev/null; then
        warn "Force killing..."
        kill -9 "${pid}" 2>/dev/null || true
    fi

    rm -f "${DEV_PID_FILE}" "${DEV_SOCKET}"
    log "Dev daemon stopped"
}

dev_status() {
    if dev_is_running; then
        local pid=$(dev_get_pid)
        log "Dev daemon is running (PID: ${pid})"
        info "Port: ${DEV_PORT}"

        # Check healthz
        if curl -s "http://127.0.0.1:${DEV_PORT}/healthz" >/dev/null 2>&1; then
            log "Health: OK"
        else
            warn "Health: Not responding"
        fi
    else
        warn "Dev daemon is not running"
    fi
}

dev_logs() {
    if [[ -f "${DEV_LOG_FILE}" ]]; then
        tail -f "${DEV_LOG_FILE}"
    else
        error "Log file not found: ${DEV_LOG_FILE}"
        exit 1
    fi
}

dev_clean() {
    dev_stop 2>/dev/null || true

    warn "Removing dev data directory: ${DEV_DATA_DIR}"
    rm -rf "${DEV_DATA_DIR}"
    log "Dev environment cleaned"
}

dev_usage() {
    echo "Dev Mode (isolated from production)"
    echo ""
    echo "Usage: $0 dev [command] [options]"
    echo ""
    echo "Commands:"
    echo "  (none)    Start dev daemon (port ${DEV_PORT})"
    echo "  stop      Stop dev daemon"
    echo "  status    Show dev daemon status"
    echo "  logs      Tail dev logs"
    echo "  clean     Remove all dev data (reset state)"
    echo ""
    echo "Options:"
    echo "  --config <path>  Use custom config file"
    echo ""
    echo "Isolation from production:"
    echo "  Port:     ${DEV_PORT} (vs 8080)"
    echo "  Socket:   .dev/mcpproxy.sock"
    echo "  Data:     .dev/"
    echo "  Logs:     .dev/mcpproxy.log"
}

usage() {
    echo "Usage: $0 [command|worktree|branch] [options]"
    echo ""
    echo "Commands:"
    echo "  stop        Stop running mcpproxy-tray"
    echo "  status      Show daemon status"
    echo "  logs        Follow daemon log"
    echo "  doctor      Run diagnostic checks"
    echo "  dev         Run in isolated dev mode (port ${DEV_PORT})"
    echo "  --skip-build  Start without rebuilding (use existing binaries)"
    echo ""
    echo "Worktree/Branch:"
    echo "  (none)      Build and start from current directory"
    echo "  main        Build and start from .worktrees/main"
    echo "  <branch>    Build and start from worktree on that branch"
    echo ""
    echo "Dev Mode (isolated):"
    echo "  dev                       Start isolated instance on port ${DEV_PORT}"
    echo "  dev --config <path>       Start with custom config"
    echo "  dev stop                  Stop dev instance"
    echo "  dev logs                  Tail dev logs"
    echo "  dev clean                 Reset dev environment"
    echo ""
    echo "Environment:"
    echo "  MCPPROXY_FLAGS  Override daemon flags (default: ${MCPPROXY_DEFAULT_FLAGS})"
    echo ""
    echo "Available worktrees:"
    git -C "$REPO_ROOT" worktree list 2>/dev/null || echo "  (none)"
}

# Main
ARG="${1:-}"
SKIP_BUILD=false

# Check for --skip-build flag
for arg in "$@"; do
    if [[ "$arg" == "--skip-build" ]]; then
        SKIP_BUILD=true
    fi
done

case "${ARG}" in
    stop)
        stop_daemon
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs
        ;;
    doctor)
        run_doctor
        ;;
    dev)
        # Dev mode - isolated from production
        shift  # Remove 'dev' from args
        DEV_ARG="${1:-}"
        DEV_CONFIG_FILE=""

        # Parse dev mode arguments
        while [[ $# -gt 0 ]]; do
            case "$1" in
                --config)
                    DEV_CONFIG_FILE="$2"
                    shift 2
                    ;;
                stop)
                    dev_stop
                    exit 0
                    ;;
                status)
                    dev_status
                    exit 0
                    ;;
                logs)
                    dev_logs
                    exit 0
                    ;;
                clean)
                    dev_clean
                    exit 0
                    ;;
                help|-h|--help)
                    dev_usage
                    exit 0
                    ;;
                *)
                    shift
                    ;;
            esac
        done
        dev_start "${DEV_CONFIG_FILE}"
        ;;
    -h|--help|help)
        usage
        ;;
    --skip-build)
        # Start from current directory without build
        start_daemon "${REPO_ROOT}"
        ;;
    *)
        # Treat as worktree/branch argument
        BUILD_DIR=$(resolve_build_dir "$ARG")

        if [[ -z "$BUILD_DIR" ]]; then
            error "Cannot find worktree or branch: $ARG"
            echo ""
            usage
            exit 1
        fi

        cd "${BUILD_DIR}"

        if [[ "$SKIP_BUILD" == "true" ]]; then
            log "Skipping build (using existing binaries)"
        else
            log "Building in: ${BUILD_DIR}"
            make build
        fi

        start_daemon "${BUILD_DIR}"
        ;;
esac
