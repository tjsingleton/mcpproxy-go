# TUI with Stable Sort, Filtering, and Multi-Column Sorting

**Date:** 2026-02-09
**Status:** Design approved, ready for implementation
**Branch:** `.worktrees/tui` (existing foundation)

## Overview

Add interactive sorting, filtering, and multi-column support to the existing TUI (terminal user interface) for both the Activity log and Servers list views. Implement deterministic, stable sorting to ensure consistent output regardless of data source ordering.

## Problem

Current TUI displays data in a static, unsorted order:
- **Activity log**: Shows records in fetch order (not useful for investigation)
- **Servers list**: Inconsistent ordering between refreshes
- **No filtering**: Must scan entire list to find relevant records
- **No multi-column sort**: Can't use secondary sort keys for tiebreaking

Users need:
- Predictable sort order that never changes for identical data
- Quick filtering by status, server, type, etc.
- Keyboard-driven sorting/filtering (TUI-native, not modal dialogs)

## Architecture

### Core Components

**1. Sort State**
```go
type sortState struct {
    Column      string  // Primary sort column: "timestamp", "type", "server_name", etc.
    Descending  bool    // Sort direction
    Secondary   string  // Fallback sort column (e.g., "id" for stable tiebreaking)
}
```

**2. Filter State**
```go
type filterState map[string]interface{}
// Examples:
// {"status": "error", "server": "glean", "type": "tool_call"}
```

**3. UI Modes**
```go
type UIMode string
const (
    ModeNormal       UIMode = "normal"       // Navigate table
    ModeFilterEdit   UIMode = "filter_edit"  // Edit filters
    ModeSortSelect   UIMode = "sort_select"  // Choose sort column
    ModeSearch       UIMode = "search"       // Text search
    ModeHelp         UIMode = "help"         // Show keybindings
)
```

**4. Data Flow**

```
User Input (keyboard)
    ↓
handleKey() → Update sort/filter state
    ↓
fetchData() → Call backend API with sort/filter params
    ↓
sortData() → Apply stable sort (SliceStable)
    ↓
filterData() → Apply filters in-memory
    ↓
Render → Display table with sort indicators
```

### Keyboard Shortcuts

**Navigation:**
```
j / ↓         Navigate down (next row)
k / ↑         Navigate up (previous row)
g             Go to top of table
G             Go to bottom of table
Page Up/Dn    Scroll by page
```

**Filtering & Sorting:**
```
f             Toggle filter mode
  ↓ In filter mode:
    tab       Move to next filter
    shift+tab Move to previous filter
    ↓/↑       Cycle through filter values
    esc       Apply filters, return to normal mode

s             Enter sort mode (choose column)
  ↓ In sort mode:
    t         Sort by Time (descending)
    y         Sort by Type (ascending)
    s         Sort by Server (ascending)
    d         Sort by Duration (descending)
    a         Sort by Status (ascending)
    esc       Cancel, keep current sort

/             Search within visible rows (client-side)
c             Clear all filters & reset to default sort
```

**General:**
```
1             Switch to Servers tab
2             Switch to Activity tab
o             Refresh ALL OAuth tokens (trigger login flow)
?             Show help/keybindings
q             Quit TUI
space         Manual refresh (reload data)
```

## Views

### Activity Log View

**Columns (sortable):**
- Time (timestamp) — Primary sort default (DESC)
- Type (type)
- Server (server_name)
- Status (status)
- Duration (duration_ms)

**Filters Available:**
- Status: all | success | error | blocked
- Server: dropdown of available servers
- Type: multi-select (tool_call, server_event, etc.)
- Sensitive Data: all | detected | clean
- Severity (if sensitive data detected): critical | high | medium | low
- Session: dropdown
- Date Range: from/to datetime

**Render Example:**
```
Sort: ▼ Time | Type | Server | Status | Duration
Filter: [Status: error ✕] [Server: glean ✕] [Clear]

  TYPE          SERVER         TOOL              STATUS    DURATION  TIME
  ✓ tool_call   glean          retrieve_tools    success   42ms      14:23:01
  ✗ tool_call   github         list_repos        error     1023ms    14:22:58
  ✓ server_evt  amplitude      (none)            ok        5ms       14:22:45

j/k scroll | s sort | f filter | o refresh | c clear | ? help | q quit
```

**Default Sort:** `timestamp DESC, id ASC` (newest first, stable)

### Servers View

**Columns (sortable):**
- Name (name)
- State (admin_state)
- Tools (tool_count)
- Health (health_level)
- Token Expires (token_expires_at)

**Filters Available:**
- State: enabled | disabled | quarantined
- Health Level: healthy | degraded | unhealthy
- OAuth Status: authenticated | expired | needs_auth

**Render Example:**
```
Sort: ▼ Name | State | Tools | Health | Token Expires
Filter: [Health: degraded ✕] [Clear]

  NAME              STATE   TOOLS  HEALTH        TOKEN EXPIRES
  ✓ glean           ▶       12     healthy       4h 23m
  △ github          ▶       8      degraded      (soon)
  ✗ amplitude       ✗       0      unhealthy     EXPIRED

j/k scroll | s sort | f filter | o refresh | c clear | ? help | q quit
```

**Default Sort:** `name ASC, id ASC` (alphabetical, stable)

## Implementation

### File Structure

```
.worktrees/tui/
├── internal/tui/
│   ├── model.go          # Main Bubble Tea model (existing, extend with sort/filter state)
│   ├── views.go          # Render functions (existing, add sort indicators)
│   ├── styles.go         # Lipgloss styles (existing)
│   ├── handlers.go       # NEW: Keyboard input, mode switching
│   ├── sort.go           # NEW: Sort logic and state management
│   ├── filter.go         # NEW: Filter logic and state management
│   └── table.go          # NEW: Reusable table component (optional refactor)
└── ...
```

### Model Updates

**Extended `model` struct:**
```go
type model struct {
    // ... existing fields ...

    // NEW: Sorting
    sortState   sortState

    // NEW: Filtering
    filterState map[string]interface{}  // Active filter values

    // NEW: UI Mode
    uiMode         UIMode  // Current interaction mode
    focusedFilter  string  // Which filter is being edited
    filterQuery    string  // Temporary text input for filters
}
```

### Sort Implementation

**1. Stable Sort (Go stdlib):**
```go
func (m *model) sortActivities() {
    sort.SliceStable(m.activities, func(i, j int) bool {
        return m.compareActivities(m.activities[i], m.activities[j])
    })
}

func (m *model) compareActivities(a, b activityInfo) bool {
    // Compare by primary sort column
    cmp := compareField(a, b, m.sortState.Column)

    // Tiebreak with secondary sort (ensures stable output)
    if cmp == 0 && m.sortState.Secondary != "" {
        cmp = compareField(a, b, m.sortState.Secondary)
    }

    if m.sortState.Descending {
        return cmp > 0
    }
    return cmp < 0
}
```

**2. Backend API Integration:**
```go
// When sort state changes, fetch fresh data with sort params:
// GET /api/v1/activity?sort=timestamp&sort_order=desc&sort_secondary=id&sort_secondary_order=asc&...
```

### Filter Implementation

**1. In-Memory Filter:**
```go
func (m *model) filterActivities() {
    filtered := make([]activityInfo, 0, len(m.activities))
    for _, a := range m.activities {
        if m.matchesAllFilters(a) {
            filtered = append(filtered, a)
        }
    }
    m.activities = filtered
}

func (m *model) matchesAllFilters(a activityInfo) bool {
    if status, ok := m.filterState["status"].(string); ok && status != "" {
        if a.Status != status {
            return false
        }
    }
    if server, ok := m.filterState["server"].(string); ok && server != "" {
        if !strings.Contains(a.ServerName, server) {
            return false
        }
    }
    // ... check other filters ...
    return true
}
```

**2. Filter Mode UX:**
- Press `f` → Enter filter mode, focus on first filter
- Use `tab`/`shift+tab` to move between filters
- Use `↓`/`↑` to cycle through options (or type to search)
- Press `esc` → Apply filters, return to normal mode
- Active filters shown as badges: `[Status: error ✕] [Server: glean ✕]`

### Keyboard Handler

```go
func (m *model) Update(msg tea.Msg) (model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.handleKey(msg.String())
    // ... handle other message types ...
    }
    return m, nil
}

func (m *model) handleKey(key string) (model, tea.Cmd) {
    // Global shortcuts
    if key == "q" {
        return m, tea.Quit
    }
    if key == "o" {
        return m, m.triggerOAuthRefresh()
    }

    // Mode-specific handling
    switch m.uiMode {
    case ModeNormal:
        return m.handleNormalMode(key)
    case ModeFilterEdit:
        return m.handleFilterMode(key)
    case ModeSortSelect:
        return m.handleSortMode(key)
    }
    return m, nil
}
```

### OAuth Refresh Command

```go
func (m *model) triggerOAuthRefresh() tea.Cmd {
    return tea.Batch(
        func() tea.Msg {
            ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
            defer cancel()

            // Trigger OAuth login for all servers needing auth
            err := m.client.TriggerOAuthLogin(ctx, "")
            if err != nil {
                return errMsg{err}
            }
            return nil
        },
        m.fetchData,  // Refresh UI after OAuth completes
    )
}
```

### Rendering with Sort Indicators

```go
func renderActivity(m model, maxHeight int) string {
    // Header with sort indicator
    sortMark := map[bool]string{true: "▼", false: "▲"}[m.sortState.Descending]

    header := fmt.Sprintf("  %-12s %-16s %-28s %-10s %-10s %s",
        addSortMark("TYPE", m.sortState.Column, "type", sortMark),
        addSortMark("SERVER", m.sortState.Column, "server_name", sortMark),
        // ... etc ...
    )
    // ... render rows ...
}

func addSortMark(label, currentCol, colKey, mark string) string {
    if currentCol == colKey {
        return label + " " + mark
    }
    return label
}
```

## Stable Sort Guarantee

**Problem:** Different data sources (API, cache, file) might return rows in arbitrary order, making sort output appear "random."

**Solution:** Use Go's `sort.SliceStable()`:
1. Sorts by primary column (e.g., `timestamp DESC`)
2. **Preserves relative order** of equal elements (stable sort property)
3. Applies secondary sort as tiebreaker (e.g., `id ASC`)
4. Result: Identical data always produces identical output

**Example:**
```go
// Three identical rows with same timestamp:
Row 1: {timestamp: "14:00", id: "aaa", type: "tool_call"}
Row 2: {timestamp: "14:00", id: "bbb", type: "tool_call"}
Row 3: {timestamp: "14:00", id: "ccc", type: "tool_call"}

// Sort by timestamp DESC, id ASC tiebreaker:
sort.SliceStable(rows, compare)

// Output always in this order (not random):
Row 1 → Row 2 → Row 3  (tied on timestamp, ordered by id)
```

## Testing Strategy

**Unit Tests:**
- Sort comparisons (timestamp, type, server, duration, status)
- Filter matching (each filter type independently)
- Sort stability (identical values preserve order)

**Integration Tests:**
- Multi-filter + multi-sort combinations
- Filter + sort persistence across tab switches
- OAuth refresh triggers re-fetch

**E2E Tests:**
- Full keyboard interaction flows
- Sort indicator accuracy
- Filter badge rendering

## Acceptance Criteria

✅ **Sorting:**
- [ ] Click any column header (or `s` + key) to sort ascending/descending
- [ ] Sort indicator (▼/▲) shows current sort column
- [ ] Multi-column sort with stable tiebreaker (secondary by `id`)
- [ ] Same data always produces same sort order (deterministic)

✅ **Filtering:**
- [ ] Press `f` to enter filter mode
- [ ] Tab between filters, cycle through options with `↓`/`↑`
- [ ] Active filters shown as badges: `[Status: error ✕]`
- [ ] `c` clears all filters and resets sort
- [ ] Filters persist across tab switches

✅ **OAuth Refresh:**
- [ ] Press `o` to refresh all OAuth tokens
- [ ] Non-blocking (UI stays responsive)
- [ ] Activity/Servers data re-fetches after completion
- [ ] Visual feedback in status bar

✅ **UX:**
- [ ] Keyboard-first (no mouse required)
- [ ] Help text (`?`) shows all keybindings
- [ ] Responsive to terminal size changes
- [ ] Works in both 80x24 and larger terminals

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Sort performance on large datasets (10k+ rows) | UI lag | Implement pagination; sort backend-side for large results |
| Filter state inconsistency across API calls | Confusing UX | Fetch with sort+filter params; re-apply locally as backup |
| Stable sort not respected by backend | Unpredictable output | Use `sort_secondary` param; validate in tests |

## Dependencies

- **Existing:** Bubble Tea, Lipgloss (already in `.worktrees/tui`)
- **No new:** Standard library `sort` package (Go 1.24)

## Timeline

- **Phase 1:** Merge sort + filter state management (~4 hours)
- **Phase 2:** Keyboard handlers + filter UX (~6 hours)
- **Phase 3:** Rendering + indicators (~3 hours)
- **Phase 4:** Testing + OAuth integration (~4 hours)

**Estimated Total:** ~17 hours over 2-3 sessions

## References

- Existing TUI: `.worktrees/tui/internal/tui/`
- Activity API: `GET /api/v1/activity?sort=...&filter=...`
- Servers API: `GET /api/v1/servers`
- Bubble Tea docs: https://github.com/charmbracelet/bubbletea
- Lipgloss docs: https://github.com/charmbracelet/lipgloss
