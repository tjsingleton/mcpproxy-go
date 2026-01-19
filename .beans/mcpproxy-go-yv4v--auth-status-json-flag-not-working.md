---
# mcpproxy-go-yv4v
title: auth status --json flag not working
status: in-progress
type: bug
priority: normal
created_at: 2026-01-19T14:28:39Z
updated_at: 2026-01-19T14:40:56Z
---

The `mcpproxy auth status --json` command does not respect the --json flag and only outputs human-readable format instead of JSON.

## Investigation Results

After reviewing all CLI commands, here are the findings:

**Commands that properly support standard CLI output formatting** (using `GetOutputFormatter()` from `internal/cli/output`):
- ✓ `upstream list` 
- ✓ `activity list/show/summary/export`
- ✓ `tools list`
- ✓ `secrets list`

**Commands with their own output implementation** (not using standard system):
- `call tool/tool-read/tool-write/tool-destructive` - has `--output` flag with "pretty" and "json" formats (custom implementation)
- `doctor` - has `--output` flag with "pretty" and "json" formats (custom implementation)

**Commands that don't support --json at all:**
- ❌ `auth status` - **This is the primary issue**
- `auth login` - doesn't need JSON output (interactive flow)
- `auth logout` - doesn't need JSON output (confirmation message)
- `code exec` - outputs raw execution results, no formatting flags
- `trust-cert` - certificate installation, doesn't need JSON output

## Root Cause

The `auth status` command (cmd/mcpproxy/auth_cmd.go:172-419) directly outputs human-readable format with `fmt.Printf` and hardcoded emojis. It doesn't use the `internal/cli/output` package or check for any output format flags.

Compare to `upstream list` (cmd/mcpproxy/upstream_cmd.go:199-246):
- Line 298-302: Uses `GetOutputFormatter()` and `ResolveOutputFormat()`
- Line 307-313: Handles json/yaml output
- Line 316-397: Builds table format for pretty output

## Solution

Integrate the `auth status` command with the `internal/cli/output` package to support `--json`, `--yaml`, and `-o` flags consistently with other commands.

## Files to Modify

1. `cmd/mcpproxy/auth_cmd.go`:
   - Add output formatting support to `runAuthStatusClientMode()` function
   - Use `GetOutputFormatter()` and `ResolveOutputFormat()` 
   - Keep existing pretty format for table output
   - Add JSON/YAML support for structured output

## Checklist

- [ ] Add output formatting to `auth status` command
- [ ] Test with `--json` flag
- [ ] Test with `--yaml` flag  
- [ ] Test with `-o json` flag
- [ ] Test with `-o yaml` flag
- [ ] Test default (table) output still works
- [ ] Update any tests
- [ ] Run `./scripts/test-api-e2e.sh`