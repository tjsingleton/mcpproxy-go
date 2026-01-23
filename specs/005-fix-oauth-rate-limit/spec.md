# Feature Specification: Fix OAuth Browser Rate Limit

**Feature Branch**: `005-fix-oauth-rate-limit`  
**Created**: 2025-12-04  
**Status**: Draft  
**Input**: User description: "Let's encode our bug fixes into the specs."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - OAuth Authentication When Browser Fails to Open (Priority: P1)

Users attempting to authenticate via OAuth should be able to manually open the authorization URL when automatic browser opening fails, without being incorrectly rate-limited.

**Why this priority**: This is a critical bug fix that prevents users from authenticating when the browser fails to open automatically. Users cannot complete authentication if they're incorrectly rate-limited, blocking access to OAuth-protected services.

**Independent Test**: Can be fully tested by simulating browser opening failures and verifying that rate limiting only applies when browser actually opens successfully. Delivers the ability for users to authenticate even when automatic browser opening fails.

**Acceptance Scenarios**:

1. **Given** a user needs to authenticate via OAuth, **When** the system attempts to open the browser but it fails (e.g., no GUI available, browser not installed, permission denied), **Then** the system should display the authorization URL for manual opening without applying rate limiting
2. **Given** a user previously attempted OAuth authentication where the browser failed to open, **When** the user attempts OAuth authentication again immediately, **Then** the system should attempt to open the browser again (not rate-limited) since the previous attempt didn't actually open a browser
3. **Given** a user successfully opened a browser for OAuth authentication, **When** the user attempts OAuth authentication again within 5 minutes, **Then** the system should apply rate limiting and display the authorization URL for manual opening

---

### User Story 2 - Accurate Rate Limiting Based on Actual Browser Opens (Priority: P1)

The OAuth browser rate limiting mechanism should only trigger when a browser is actually opened successfully, not when OAuth flows start or complete.

**Why this priority**: Accurate rate limiting ensures users aren't blocked from authenticating when they haven't actually opened a browser, while still preventing browser spam when browsers are successfully opened.

**Independent Test**: Can be fully tested by tracking browser opening attempts and verifying rate limit state only changes when browser opens successfully. Delivers accurate rate limiting that matches user behavior.

**Acceptance Scenarios**:

1. **Given** an OAuth authentication flow starts, **When** the browser opening attempt fails, **Then** the rate limit timestamp should not be updated
2. **Given** an OAuth authentication flow completes successfully, **When** the browser was never opened (e.g., manual URL entry), **Then** the rate limit timestamp should not be updated
3. **Given** a browser is successfully opened for OAuth, **When** the OAuth flow completes (successfully or with error), **Then** the rate limit timestamp should reflect the browser opening time, not the completion time

---

### Edge Cases

- What happens when multiple OAuth flows start simultaneously but browsers fail to open?
  - Each failed browser opening should not update the rate limit timestamp
- How does system handle OAuth flows that start but never complete?
  - Rate limit should only reflect browser opening, not flow completion
- What happens when browser opening succeeds but user closes browser immediately?
  - Rate limit should still apply since browser was successfully opened
- How does system handle OAuth flows in headless/remote environments where browser cannot open?
  - Rate limiting should not apply since browser never opens

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST only update browser rate limit timestamp when browser is successfully opened
- **FR-002**: System MUST NOT update rate limit timestamp when OAuth flow starts if browser opening fails
- **FR-003**: System MUST NOT update rate limit timestamp when OAuth flow completes if browser was never opened
- **FR-004**: System MUST apply rate limiting only when a browser was successfully opened within the rate limit window (5 minutes)
- **FR-005**: System MUST allow unlimited OAuth attempts when browser opening consistently fails
- **FR-006**: System MUST display authorization URL for manual opening when rate limited or when browser fails to open

### Assumptions

- OAuth authentication flows support automatic browser opening as the primary method
- Rate limiting window is 5 minutes (existing system behavior)
- System can detect when browser opening succeeds vs. fails
- Users can manually open URLs when automatic opening fails
- Multiple OAuth flows may occur simultaneously for different services

### Dependencies

- Existing OAuth authentication infrastructure
- Browser opening mechanism (platform-specific)
- Rate limiting infrastructure

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can attempt OAuth authentication without rate limiting when browser fails to open (100% of failed browser opening attempts do not trigger rate limiting)
- **SC-002**: Rate limiting only applies when browser actually opens successfully (0% false positive rate limit triggers)
- **SC-003**: Users can complete OAuth authentication even when automatic browser opening fails (no blocking due to incorrect rate limiting)
- **SC-004**: Rate limiting accurately prevents browser spam when browsers are successfully opened (rate limit triggers correctly for successful browser opens within 5-minute window)
