# Proposal: Context Compression & Knowledge Base Integration

**Status**: Draft
**Inspired by**: [claude-context-mode](https://github.com/mksglu/claude-context-mode)
**Date**: 2026-03-02

## Problem Statement

When AI agents use MCP tools through mcpproxy, tool responses frequently flood the context window with raw data. A single Playwright snapshot can consume 56 KB, a GitHub issue list 59 KB, access logs 45 KB. With 81+ tools active, 143K tokens (72% of the 200K window) can be consumed before the agent's first message. Sessions degrade after ~30 minutes.

mcpproxy already addresses **tool discovery bloat** via BM25 search (agents discover tools on-demand instead of loading all definitions). It also has a **truncation system** (`internal/truncate/`) with paginated caching via `read_cache`. But truncation is a blunt instrument — it cuts data at a character limit regardless of what the agent actually needs.

The [claude-context-mode](https://github.com/mksglu/claude-context-mode) project demonstrates a more intelligent approach: **intent-driven compression** that indexes full outputs and returns only the relevant portions. It achieves 94-98% context reduction while preserving the information the agent needs.

## Opportunity

mcpproxy is uniquely positioned to implement these capabilities natively because:

1. **It already sits in the proxy path** — every tool call response flows through `handleCallToolVariant` before reaching the agent
2. **It already has BM25 search** via Bleve (`internal/index/`) — the same technology needed for indexing tool outputs
3. **It already has truncation infrastructure** (`internal/truncate/`) — ready to be upgraded from "dumb cut" to "smart filter"
4. **It already tracks intent** (Spec 018) — the `intent_reason` field tells us _what the agent is trying to accomplish_, the perfect signal for filtering
5. **It already counts tokens** (`internal/server/tokens/`) — can measure and report actual savings
6. **It already has activity logging** — can track compression metrics per tool call

## Proposed Feature Set

### Feature 1: Response Compression Pipeline (Smart Truncation)

**What**: Replace the current truncation system with an intelligent compression pipeline that uses the agent's intent to extract only relevant information from large responses.

**How it works**:

```
Agent calls: call_tool_read("github:list_issues", args, intent_reason="find auth bugs")
                    │
                    ▼
         Upstream returns 59 KB of issues
                    │
                    ▼
         ┌─────────────────────────────┐
         │  Response Compression       │
         │  Pipeline                   │
         │                             │
         │  1. Check size threshold    │
         │     (configurable, e.g. 5KB)│
         │                             │
         │  2. Index full response     │
         │     into session-scoped     │
         │     Bleve index             │
         │                             │
         │  3. Search index using      │
         │     intent_reason as query  │
         │                             │
         │  4. Return matching         │
         │     sections + summary      │
         │     header                  │
         └─────────────────────────────┘
                    │
                    ▼
         Agent receives 2.1 KB (relevant issues only)
         + metadata: "Showing 4 of 127 issues matching 'auth bugs'"
         + cache key for full dataset access via read_cache
```

**Key design decisions**:

- **Session-scoped index**: Each MCP session gets its own Bleve index (temporary, cleaned up on session end). This isolates data between agents and avoids cross-contamination.
- **Intent as query signal**: The `intent_reason` parameter from Spec 018 is already being collected. When present and the response exceeds the threshold, use it as the BM25 query against the indexed response.
- **Graceful fallback**: If no intent is provided, fall back to current truncation behavior. If intent yields no matches, return a summary + cache key.
- **Preserves read_cache**: The full response is still cached. The agent can always call `read_cache` to paginate through the complete data.

**New configuration**:
```json
{
  "context_compression": {
    "enabled": true,
    "threshold_bytes": 5120,
    "max_result_bytes": 4096,
    "max_snippets": 10,
    "snippet_context_lines": 3,
    "index_ttl_minutes": 30
  }
}
```

**Files to modify/create**:

| File | Change |
|------|--------|
| `internal/compress/pipeline.go` | New — compression pipeline with Bleve indexing |
| `internal/compress/session_index.go` | New — per-session Bleve index lifecycle |
| `internal/compress/snippet.go` | New — smart snippet extraction around matches |
| `internal/server/mcp.go` | Modify — integrate pipeline into `handleCallToolVariant` response path |
| `internal/truncate/truncator.go` | Modify — delegate to compression pipeline when intent is available |
| `internal/config/config.go` | Modify — add `ContextCompression` config struct |

### Feature 2: `index_response` Built-in Tool

**What**: A new MCP tool that lets agents explicitly index a previous tool response for later querying. This handles the case where the agent wants to explore data it already received without re-fetching.

**Tool definition**:
```
index_response(
  cache_key: string,     // Cache key from a truncated response
  content: string,       // OR raw content to index (for non-cached data)
  tags: string[],        // Optional tags for organizing indexed data
) → { index_id, chunks_indexed, size_bytes }
```

**How it integrates**:
- Reuses the session-scoped Bleve index from Feature 1
- Content is chunked by structure (JSON arrays → per-item, text → by paragraph/heading)
- Returns an `index_id` that can be used with the `search_index` tool

### Feature 3: `search_index` Built-in Tool

**What**: Query the session's indexed content using BM25 full-text search with fuzzy matching.

**Tool definition**:
```
search_index(
  query: string,         // Natural language or keyword query
  index_id: string,      // Optional — scope to specific indexed response
  limit: int,            // Max results (default: 5)
  tags: string[],        // Optional — filter by tags
) → { results: [{ snippet, score, source_tool, source_cache_key }] }
```

**Key capabilities**:
- **BM25 ranking** — same algorithm already used for tool discovery
- **Fuzzy matching** — Porter stemming via Bleve analyzers (already a dependency)
- **Cross-response search** — query across all indexed data in the session
- **Source tracking** — each result links back to the original tool call and cache key

### Feature 4: `batch_call_tools` Built-in Tool

**What**: Execute multiple tool calls in a single request and return compressed aggregate results. This prevents the N+1 context problem where each tool call adds overhead.

**Tool definition**:
```
batch_call_tools(
  calls: [
    { name: "server:tool", args_json: "...", variant: "read" },
    { name: "server:tool", args_json: "...", variant: "read" },
    ...
  ],
  intent_reason: string,  // Shared intent for all calls
  compress: boolean,      // Apply compression pipeline (default: true)
  parallel: boolean,      // Execute in parallel (default: true for reads)
) → { results: [{ tool, status, compressed_response }], summary }
```

**Design**:
- Parallel execution for read-only calls (respects tool annotations)
- Sequential execution for write/destructive calls
- Each individual response goes through the compression pipeline
- Aggregate summary generated: "3 calls completed, 2 compressed (87 KB → 3.2 KB)"
- Full results cached individually and accessible via `read_cache`
- Activity logging creates a parent record linking all child calls

**Variant enforcement**: Each call in the batch specifies its variant. The handler validates against the caller's tool variant (e.g., `batch_call_tools` registered as destructive so it can proxy any variant, but individual calls are validated against their declared variant).

### Feature 5: Session Context Metrics

**What**: Expose real-time context usage metrics so agents and UIs can make informed decisions about when to compress, batch, or prune.

**New MCP resource** (using MCP resources spec):
```
context://session/metrics
→ {
    total_input_tokens: 14200,
    total_output_tokens: 89300,
    compressed_savings_tokens: 67000,
    compression_ratio: 0.82,
    tool_calls: 12,
    compressed_calls: 8,
    session_duration_minutes: 23,
    per_tool_metrics: {
      "github:list_issues": { calls: 3, raw_tokens: 42000, compressed_tokens: 3200 },
      ...
    }
  }
```

**Alternatively**, this could be a built-in tool `context_metrics()` that returns the same data:
```
context_metrics(
  detail_level: "summary" | "per_tool" | "full"
) → { ... metrics ... }
```

**Integration points**:
- Token counter (`internal/server/tokens/`) already tracks per-call metrics
- Activity service already stores duration and response sizes
- SSE events (`/events`) can stream metrics updates to the web UI
- Existing savings calculator (`tokens/savings.go`) extended with runtime data

## Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                    MCPProxy Server                        │
│                                                          │
│  ┌──────────────┐    ┌───────────────────────────────┐   │
│  │ Tool Registry │    │  Context Compression Engine    │   │
│  │               │    │                               │   │
│  │ retrieve_tools│    │  ┌─────────────────────────┐  │   │
│  │ call_tool_*   │    │  │ Response Pipeline       │  │   │
│  │ index_response│───▶│  │  threshold check        │  │   │
│  │ search_index  │    │  │  → chunk content        │  │   │
│  │ batch_call    │    │  │  → index into Bleve     │  │   │
│  │ context_metr. │    │  │  → query with intent    │  │   │
│  │ read_cache    │    │  │  → extract snippets     │  │   │
│  └──────────────┘    │  └─────────────────────────┘  │   │
│         │            │                               │   │
│         │            │  ┌─────────────────────────┐  │   │
│         │            │  │ Session Index Manager    │  │   │
│         │            │  │  per-session Bleve       │  │   │
│         │            │  │  lifecycle management    │  │   │
│         │            │  │  TTL-based cleanup       │  │   │
│         │            │  └─────────────────────────┘  │   │
│         │            │                               │   │
│         │            │  ┌─────────────────────────┐  │   │
│         │            │  │ Metrics Collector        │  │   │
│         │            │  │  per-session tracking    │  │   │
│         │            │  │  token savings calc      │  │   │
│         │            │  └─────────────────────────┘  │   │
│         │            └───────────────────────────────┘   │
│         │                                                │
│         ▼                                                │
│  ┌──────────────────┐  ┌────────────┐  ┌─────────────┐  │
│  │ Upstream Manager  │  │ Cache      │  │ Activity    │  │
│  │ (existing)        │  │ (existing) │  │ (existing)  │  │
│  └──────────────────┘  └────────────┘  └─────────────┘  │
└──────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Response Compression Pipeline (Feature 1)
**Effort**: Core value. Integrates into the existing response flow at the truncation point.

1. Create `internal/compress/` package with pipeline, session index manager, and snippet extractor
2. Add `ContextCompression` config to `internal/config/config.go`
3. Modify `handleCallToolVariant` in `mcp.go` to route through compression pipeline when intent is available and response exceeds threshold
4. Update token metrics to track compression savings
5. Add activity metadata for compression events

### Phase 2: Index & Search Tools (Features 2 & 3)
**Effort**: Builds on Phase 1's session index infrastructure.

1. Register `index_response` and `search_index` tools in `registerTools()`
2. Implement handlers that delegate to the session index manager
3. Add content chunking strategies (JSON-aware, markdown-aware, plain text)

### Phase 3: Batch Tool Calls (Feature 4)
**Effort**: Independent of Phase 1/2 but benefits from compression.

1. Register `batch_call_tools` tool with variant-aware validation
2. Implement parallel/sequential execution with goroutine orchestration
3. Integrate with compression pipeline for aggregate results
4. Add parent-child activity logging

### Phase 4: Context Metrics (Feature 5)
**Effort**: Aggregation layer over existing infrastructure.

1. Create session-scoped metrics collector
2. Register `context_metrics` tool or MCP resource
3. Extend SSE events with metrics stream
4. Add web UI widget for session metrics

## Key Differences from claude-context-mode

| Aspect | claude-context-mode | This Proposal |
|--------|-------------------|---------------|
| Runtime | Node.js + SQLite FTS5 | Go + Bleve (already a dependency) |
| Scope | Standalone MCP server | Integrated into proxy response path |
| Compression trigger | External tool call | Automatic on threshold + intent |
| Code execution | 11-language sandbox | Existing JS sandbox (code_execution) |
| Index persistence | Per-process SQLite | Per-session Bleve (temp directory) |
| Intent signal | Explicit `intent` param | Reuses Spec 018 `intent_reason` |
| Batch execution | `batch_execute` tool | `batch_call_tools` with variant enforcement |
| Search | SQLite FTS5 + fuzzy | Bleve BM25 + Porter stemming (same as tool discovery) |
| Credential handling | Env passthrough | Already handled by upstream manager |
| Throttling | Progressive (calls 1-3/4-8/9+) | Not needed (proxy manages all routing) |

## What We Deliberately Don't Include

1. **Multi-language code execution sandbox**: claude-context-mode supports 11 languages. mcpproxy already has JS execution via `code_execution`. Adding Python/Ruby/etc. sandboxes is orthogonal to context compression and adds significant security surface.

2. **PreToolUse hook injection**: claude-context-mode injects routing instructions into subagent prompts. mcpproxy operates at the protocol level — compression is transparent to agents, no prompt injection needed.

3. **Progressive search throttling**: claude-context-mode throttles after N search calls. mcpproxy's approach of automatic compression on the response path makes this unnecessary — the agent never sees the bloated data in the first place.

4. **URL fetching/indexing**: claude-context-mode has `fetch_and_index`. This is a separate concern — if agents need web fetching, that should be an upstream MCP server, not built into the proxy.

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Bleve index overhead per session | Temp directory with TTL cleanup; index size bounded by `max_indexed_bytes` config |
| Compression removes critical data | Always preserve cache key for full access; include "X of Y total" metadata |
| Intent-based filtering too aggressive | Configurable snippet count and context lines; fallback to summary when <2 matches |
| Batch tool calls increase blast radius | Variant enforcement per-call; parallel only for reads; sequential for writes |
| Session index grows unbounded | TTL eviction; configurable max entries; cleanup on session close |
