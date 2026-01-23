# Feature Specification: Fix OAuth Retry Loops and Add Force Re-auth

**Feature Branch**: `006-fix-oauth-retry`  
**Created**: 2025-12-03  
**Status**: Draft  
**Input**: User description: "Fix OAuth retry loops and add force re-auth capability"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - OAuth Flows Complete Without Retry Loops (Priority: P1)

Users with OAuth-enabled servers should be able to authenticate without experiencing infinite retry loops that prevent successful authentication.

**Why this priority**: This is a critical bug fix that prevents users from authenticating with OAuth-enabled servers. Without this fix, users cannot use servers requiring OAuth authentication, blocking core functionality.

**Independent Test**: Can be fully tested by attempting to connect to an OAuth-enabled server and verifying that the OAuth flow completes successfully without getting stuck in retry loops. Delivers working OAuth authentication.

**Acceptance Scenarios**:

1. **Given** a server requires OAuth authentication, **When** the system attempts to connect, **Then** the OAuth flow starts and completes successfully without retry loops
2. **Given** an OAuth flow is already in progress, **When** a retry attempt occurs, **Then** the system waits for the existing flow to complete instead of failing immediately
3. **Given** OAuth state is marked as "in progress" but no actual flow is running, **When** a new OAuth attempt occurs, **Then** the system detects stale state and starts a new flow
4. **Given** multiple servers require OAuth simultaneously, **When** OAuth flows are triggered, **Then** each server's OAuth flow completes independently without interference

---

### User Story 2 - Force Re-authentication via REST API (Priority: P2)

Administrators should be able to force OAuth re-authentication for a server via the REST API, even when tokens exist, to resolve authentication issues or refresh credentials.

**Why this priority**: This provides administrators with a programmatic way to resolve OAuth issues without requiring CLI access. It enables automation and troubleshooting workflows.

**Independent Test**: Can be fully tested by calling the REST API endpoint with the force parameter and verifying that OAuth re-authentication occurs even when valid tokens exist. Delivers administrative control over OAuth flows.

**Acceptance Scenarios**:

1. **Given** a server has existing OAuth tokens, **When** an administrator calls the login endpoint with `force=true`, **Then** the system clears existing tokens and triggers a fresh OAuth flow
2. **Given** a server has no OAuth tokens, **When** an administrator calls the login endpoint with `force=false`, **Then** the system triggers OAuth flow normally
3. **Given** a server has expired or invalid tokens, **When** an administrator calls the login endpoint with `force=true`, **Then** the system clears stale tokens and triggers a fresh OAuth flow

---

### User Story 3 - Force Re-authentication via Web UI (Priority: P2)

Users should be able to force OAuth re-authentication for a server via the web interface, providing an easy way to resolve authentication issues without using the CLI.

**Why this priority**: This provides a user-friendly way to resolve OAuth issues directly from the web interface, improving accessibility and reducing the need for CLI knowledge.

**Independent Test**: Can be fully tested by clicking the "Force Re-auth" button in the web UI and verifying that OAuth re-authentication occurs. Delivers user-friendly OAuth troubleshooting.

**Acceptance Scenarios**:

1. **Given** a user is viewing a server detail page for an OAuth-enabled server, **When** the user clicks "Force Re-auth" in the Actions menu, **Then** the system triggers OAuth re-authentication and opens the browser for authentication
2. **Given** a user triggers force re-auth, **When** the OAuth flow completes, **Then** the system updates the server status to show successful authentication
3. **Given** a user triggers force re-auth, **When** the OAuth flow fails or times out, **Then** the system displays an appropriate error message

---

### Edge Cases

- What happens when OAuth callback server port is already in use?
- How does system handle OAuth timeout during retry wait?
- What happens when multiple force re-auth requests are made simultaneously for the same server?
- How does system handle OAuth state clearing when a background OAuth flow is still waiting for callback?
- What happens when OAuth tokens exist but are invalid or expired?
- How does system handle OAuth flow interruption (user closes browser, network disconnects)?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST NOT mark OAuth as "in progress" until the manual OAuth flow actually starts
- **FR-002**: System MUST wait for in-progress OAuth flows to complete instead of failing immediately when retry attempts occur
- **FR-003**: System MUST detect and handle stale OAuth "in progress" state that doesn't correspond to an actual running flow
- **FR-004**: System MUST provide a mechanism to force OAuth re-authentication via REST API with a `force` query parameter
- **FR-005**: System MUST provide a "Force Re-auth" action in the web UI for OAuth-enabled servers
- **FR-006**: System MUST clear existing OAuth tokens and state when force re-authentication is requested
- **FR-007**: System MUST coordinate OAuth state across concurrent connection attempts to prevent duplicate flows
- **FR-008**: System MUST handle OAuth completion detection and trigger reconnection after successful authentication
- **FR-009**: System MUST provide appropriate error messages when OAuth flows fail or timeout
- **FR-010**: System MUST support force re-authentication for both HTTP and SSE OAuth transport types

### Key Entities *(include if feature involves data)*

- **OAuth State**: Represents the current state of OAuth authentication flow (in progress, completed, cleared). Tracks whether OAuth is actively running and when it was last attempted.
- **OAuth Tokens**: Represents stored authentication credentials for OAuth-enabled servers. Can be cleared or refreshed during force re-authentication.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: OAuth authentication flows complete successfully without retry loops for 95% of attempts
- **SC-002**: Users can force OAuth re-authentication via REST API or web UI within 2 seconds of initiating the action
- **SC-003**: OAuth retry loops are eliminated - system waits for existing flows instead of creating duplicate attempts
- **SC-004**: Force re-authentication successfully clears stale tokens and triggers fresh OAuth flows in 100% of cases
- **SC-005**: OAuth state management correctly handles concurrent connection attempts without race conditions
- **SC-006**: Users experience no more than one manual OAuth authentication per server per session (unless force re-auth is explicitly requested)

## Assumptions

- OAuth callback servers can be started on available ports
- Browser can be opened for OAuth authorization (or users can manually open URLs)
- OAuth providers support standard OAuth 2.0 flows with PKCE
- Network connectivity is available during OAuth flows
- Users have appropriate permissions to authenticate with OAuth providers

## Dependencies

- Existing OAuth infrastructure and token storage
- REST API authentication and authorization
- Web UI server management interface
- OAuth callback server coordination system

## Out of Scope

- Changes to OAuth provider configuration or endpoints
- Modifications to OAuth token refresh mechanisms (beyond force re-auth)
- CLI force flag changes (already implemented)
- OAuth flow UI improvements beyond force re-auth button
