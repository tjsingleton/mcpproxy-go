# Feature Specification: CLI-Daemon Communication Reliability

**Feature Branch**: `001-cli-daemon-communication`  
**Created**: 2025-12-04  
**Status**: Draft  
**Input**: User description: "Let's encode our bug fixes into the specs."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Add Servers via CLI Without Database Lock (Priority: P1)

As a user running the mcpproxy daemon, I want to add MCP servers via CLI commands without encountering database lock errors, so that I can manage my server configuration efficiently.

**Why this priority**: Database lock errors prevent users from managing servers when the daemon is running, forcing them to stop the daemon or manually edit config files. This is a critical usability issue that blocks core functionality.

**Independent Test**: Can be fully tested by starting the daemon, then running `mcpproxy call tool --tool-name=upstream_servers --json_args='{"operation":"add",...}'` and verifying no database lock error occurs. The command should successfully add the server via daemon communication.

**Acceptance Scenarios**:

1. **Given** the daemon is running and socket file exists, **When** user runs CLI command to add a server, **Then** command uses socket/client mode and successfully adds server without database lock error
2. **Given** the daemon is running but socket file was deleted (Unix quirk), **When** user runs CLI command, **Then** command detects daemon via TCP fallback and uses client mode instead of standalone mode
3. **Given** the daemon is not running, **When** user runs CLI command, **Then** command uses standalone mode and opens database directly (expected behavior)
4. **Given** built-in tools like `upstream_servers` are called via CLI, **When** daemon is running but socket communication fails, **Then** command returns clear error message instead of attempting standalone mode (which would cause database lock)

---

### User Story 2 - Clean Socket File Cleanup (Priority: P2)

As a system administrator, I want the daemon to clean up socket files gracefully without logging warnings for normal conditions, so that logs remain clean and actionable.

**Why this priority**: Warning messages about missing socket files during shutdown create noise in logs and can mask real issues. This is a quality-of-life improvement for operations.

**Independent Test**: Can be fully tested by starting the daemon, manually deleting the socket file while daemon is running, then shutting down the daemon. Verify no warning is logged about failing to remove a non-existent socket file.

**Acceptance Scenarios**:

1. **Given** the daemon is shutting down, **When** socket file exists, **Then** daemon removes it successfully and logs at debug level
2. **Given** the daemon is shutting down, **When** socket file does not exist (already removed or never created), **Then** daemon logs at debug level that file doesn't exist (not a warning)
3. **Given** the daemon encounters an error during socket creation, **When** cleanup attempts to remove socket file, **Then** missing file errors are ignored (not logged as warnings)

---

### User Story 3 - OAuth Login via Daemon (Priority: P2)

As a user running the daemon, I want OAuth login commands to use daemon communication instead of opening the database directly, so that I can authenticate servers without database lock conflicts.

**Why this priority**: OAuth login commands currently always use standalone mode, causing database locks when daemon is running. This prevents users from authenticating servers via CLI when daemon is active.

**Independent Test**: Can be fully tested by starting the daemon, then running `mcpproxy auth login --server=atlassian` and verifying it uses socket/client mode to trigger OAuth via daemon API instead of opening database directly.

**Acceptance Scenarios**:

1. **Given** the daemon is running, **When** user runs `mcpproxy auth login --server=<name>`, **Then** command detects daemon and uses socket/client mode to trigger OAuth via daemon API
2. **Given** the daemon is not running, **When** user runs `mcpproxy auth login --server=<name>`, **Then** command uses standalone mode and opens database directly (expected behavior)
3. **Given** OAuth login is triggered via daemon API, **When** OAuth flow completes, **Then** authentication state is properly persisted and server connection succeeds

---

### Edge Cases

- What happens when socket file exists but daemon process crashed (stale socket)?
- What happens when multiple CLI commands run simultaneously while daemon is running?
- What happens when socket file permissions are incorrect?
- What happens when TCP fallback is attempted but daemon is listening on different port?
- What happens when OAuth callback times out while using daemon mode?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: CLI commands MUST detect daemon availability before attempting database access
- **FR-002**: CLI commands MUST use socket/client mode when daemon is detected and socket is available
- **FR-003**: CLI commands MUST fall back to TCP detection when socket file is missing but daemon may be running
- **FR-004**: Built-in tool commands (like `upstream_servers`) MUST NOT attempt standalone mode when daemon is running (to prevent database locks)
- **FR-005**: Socket cleanup during daemon shutdown MUST check file existence before attempting removal
- **FR-006**: Socket cleanup MUST log missing file conditions at debug level, not warning level
- **FR-007**: OAuth login command MUST detect daemon and use socket/client mode when available
- **FR-008**: OAuth login command MUST provide clear error messages when daemon is not accessible
- **FR-009**: CLI client MUST support OAuth login endpoint via daemon API (`/api/v1/servers/{name}/login`)
- **FR-010**: Daemon detection MUST check both socket file existence and TCP connectivity as fallback

### Key Entities

- **Daemon Process**: The running mcpproxy server instance that manages upstream connections and holds database lock
- **Socket File**: Unix domain socket file used for local IPC between CLI and daemon (macOS/Linux)
- **CLI Command**: Command-line interface commands that can operate in client mode (via socket) or standalone mode (direct database access)
- **Database Lock**: BBolt database lock that prevents multiple processes from accessing the same database file simultaneously

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can add servers via CLI commands while daemon is running without encountering database lock errors (100% success rate)
- **SC-002**: OAuth login commands complete successfully via daemon API when daemon is running (no database lock errors)
- **SC-003**: Socket cleanup warnings are eliminated for normal shutdown scenarios (zero false-positive warnings)
- **SC-004**: CLI commands correctly detect daemon availability in all scenarios (socket exists, socket missing but daemon running, daemon not running)
- **SC-005**: Users receive clear error messages when daemon communication fails, guiding them to restart daemon or check connectivity

## Assumptions

- BBolt database only allows one process to access it at a time (database lock behavior)
- Socket files may be deleted while daemon still has file descriptor open (Unix quirk)
- Daemon listens on TCP port 8080 by default (configurable)
- Socket file location is `~/.mcpproxy/mcpproxy.sock` on Unix systems
- CLI commands should prefer socket communication over TCP for performance and security
- Standalone mode is acceptable fallback when daemon is not running

## Dependencies

- Existing socket detection infrastructure (`internal/socket`)
- Existing CLI client infrastructure (`internal/cliclient`)
- Existing daemon API endpoints (`/api/v1/servers/{name}/login`)
- BBolt database locking behavior (cannot be changed)

## Out of Scope

- Changing BBolt database locking behavior (fundamental limitation)
- Adding new daemon API endpoints (uses existing endpoints)
- Modifying socket creation logic (only cleanup behavior)
- Supporting multiple concurrent database access (not possible with BBolt)
