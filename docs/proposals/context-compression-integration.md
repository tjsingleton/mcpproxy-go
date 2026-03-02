# Proposal: Context Compression & Knowledge Base Integration (v2)

**Status**: Draft (v2 — revised after gap analysis)
**Inspired by**: [claude-context-mode](https://github.com/mksglu/claude-context-mode)
**Date**: 2026-03-02

## Problem Statement

When AI agents use MCP tools through mcpproxy, tool responses frequently flood the context window with raw data. A single Playwright snapshot can consume 56 KB, a GitHub issue list 59 KB, access logs 45 KB. With 81+ tools active, 143K tokens (72% of the 200K window) can be consumed before the agent's first message. Sessions degrade after ~30 minutes.

mcpproxy already addresses **tool discovery bloat** via BM25 search (agents discover tools on-demand instead of loading all definitions). It also has a **truncation system** (`internal/truncate/`) with paginated caching via `read_cache`. But truncation is a blunt instrument — it cuts data at a character limit regardless of what the agent actually needs.

**Additionally**, agents running on providers like AWS Bedrock lack access to Claude's built-in `WebSearch` tool. These agents have no way to search the web or fetch URLs, making them blind to external information. This is a fundamental capability gap that the proxy is positioned to fill.

The [claude-context-mode](https://github.com/mksglu/claude-context-mode) project demonstrates an intelligent approach: **intent-driven compression** that indexes full outputs and returns only the relevant portions. It achieves 94-98% context reduction while preserving the information the agent needs.

## Gap Analysis: What We Lose Without claude-context-mode's Approach

### What we CAN'T replicate at the proxy level

| Capability | Why it can't be replicated | Impact |
|-----------|---------------------------|--------|
| **Intercepting agent-side tools** (Bash curl, Read, Grep) | These tools run client-side (e.g., inside Claude Code). They never flow through mcpproxy. context-mode's PreToolUse hooks intercept them before execution. | **Medium** — Only affects Claude Code users. Agents using other MCP clients already can't use Bash/Read/Grep through the proxy. |
| **Subagent prompt injection** | context-mode injects routing instructions into child agent prompts via the Task hook, forcing subagents to use batch_execute/search instead of dumping raw data. | **Low-Medium** — Proxy-level compression handles MCP tool responses regardless. The issue is when subagents bypass MCP entirely. |
| **Blocking WebFetch/curl at the client** | context-mode redirects these to `fetch_and_index()` so raw HTML never enters context. The proxy can't prevent agents from using client-side tools. | **Low** — If the agent fetches via a proxy-provided tool instead, the redirect is unnecessary. |

### What we do BETTER at the proxy level

| Capability | Why proxy is better |
|-----------|-------------------|
| **Universal compression** | Works for ALL MCP tool responses from ALL upstream servers. context-mode only compresses data routed through its specific tools. |
| **Client-agnostic** | Works with any MCP client — Claude Code, Cursor, Continue, custom agents. context-mode only works with Claude Code's hook system. |
| **Transparent operation** | No hook wiring, no prompt injection, no client modifications. Agents get compressed responses automatically. |
| **Intent-driven filtering** | Leverages Spec 018's `intent_reason` (already collected) as the query signal. context-mode requires explicit `intent` parameters. |
| **Existing infrastructure** | Bleve BM25 (same as tool discovery), BBolt caching, session management, activity logging — all already in place. |

### What we DO need to add (gaps worth closing)

1. **Secure web search & fetch** (highest priority — fills provider capability gaps)
2. **Content-aware chunking** (markdown/HTML → structured chunks, not just JSON arrays)
3. **Session-scoped response indexing** (search across previous tool outputs)

## How claude-context-mode's Modifications to Claude Code Work

context-mode installs 5 PreToolUse hooks that intercept Claude Code tool calls before execution:

| Hook | What it intercepts | What it does |
|------|-------------------|-------------|
| **Bash** | `curl`, `wget`, inline HTTP requests | Blocks the command, returns instruction to use `fetch_and_index()` instead |
| **WebFetch** | All URL fetching | Completely blocked; redirected to `fetch_and_index()` |
| **Read** | Files >50 lines | Nudges agent to use `execute_file()` to keep content sandboxed |
| **Grep** | Raw grep output | Recommends sandboxed `execute()` instead of dumping results into context |
| **Task** (subagents) | Subagent creation | Injects a ROUTING_BLOCK into the subagent's prompt forcing it to use batch_execute, search, and write artifacts to files |

**Assessment**: These hooks are **essential for context-mode** because it's a standalone MCP server — without hooks, agents bypass it entirely. For mcpproxy, hooks are **nice-to-have** because compression happens transparently at the proxy level. However, for Claude Code users specifically, providing an optional hook configuration could prevent agents from bypassing MCP tools via Bash curl/Read.

**Recommendation**: Ship an optional `.claude/settings.json` hook configuration that Claude Code users can install. It would redirect WebFetch to `web_fetch` (our built-in tool) and warn on large Read operations. This is Phase 4 work — low priority, Claude Code-specific.

## How Multi-Language Code Sandboxes Are Actually Used

In context-mode, the sandbox IS the compression mechanism:

```
Agent needs to analyze a 200KB log file
    │
    ▼
Instead of: Read file → 200KB enters context → agent analyzes
    │
    ▼
context-mode: execute("python", code="
    import re
    errors = [l for l in open('app.log') if 'ERROR' in l]
    print(f'Found {len(errors)} errors')
    for e in errors[:5]:
        print(e.strip())
")
    │
    ▼
Only stdout (~500 bytes) enters context
If stdout > 5KB → auto-indexed, returns search results instead
```

The 11-language support (JS, TS, Python, Shell, Ruby, Go, Rust, PHP, Perl, R, Elixir) exists because:
- **Python** for data analysis (pandas, json parsing)
- **Shell** for log processing (grep, awk, sed pipelines)
- **Go/Rust** for parsing structured data
- Each language runs as a subprocess — raw output stays in subprocess memory

**Assessment for mcpproxy**: The proxy-level approach achieves the same compression effect differently — it compresses the *response* after the upstream tool returns it, rather than keeping data inside a sandbox. mcpproxy's existing `code_execution` tool (JS via Goja) already provides orchestration for multi-tool workflows. Adding more language runtimes would:
- Add significant binary size and security surface
- Duplicate what upstream MCP servers already do (e.g., `npx @anthropic/code-sandbox`)
- Not improve compression (proxy already handles that)

**Recommendation**: Don't add multi-language sandboxes. The existing JS sandbox + proxy-level compression + upstream tool diversity covers the use cases. If a user needs Python data analysis, they connect a Python MCP server as an upstream.

## Proposed Feature Set

### Feature 1: Secure Web Search & Fetch (NEW — highest priority)

**What**: Built-in MCP tools that give agents web search and URL fetching capabilities, with security hardening. This fills the gap for providers (Bedrock, Azure, etc.) that don't expose Claude's native WebSearch tool.

**Why built-in, not an upstream**: Web content is often massive (100KB+ for a web page). If fetched by an upstream MCP server, the full content flows through the proxy and gets truncated by the dumb truncator. By building it in, the fetch → convert → index → compress pipeline is a single operation — raw HTML never enters the context window.

**Tool definitions**:

```
web_search(
  query: string,          // Search query
  num_results: int,       // Max results (default: 5, max: 20)
  site: string,           // Optional domain filter (e.g., "docs.python.org")
) → { results: [{ title, url, snippet }], query, total_results }
```

```
web_fetch(
  url: string,            // URL to fetch
  intent: string,         // What the agent is looking for (used for compression)
  selector: string,       // Optional CSS selector to extract specific content
  raw: boolean,           // Return full markdown without compression (default: false)
) → { content, url, title, metadata: { original_size, compressed_size, chunks_indexed } }
```

**Security model**:

| Protection | Implementation |
|-----------|---------------|
| **SSRF prevention** | Block private IPs (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16), loopback (127.0.0.0/8, ::1), link-local (169.254.0.0/16). Resolve DNS before connecting to prevent DNS rebinding. |
| **HTTPS enforcement** | Default to HTTPS. HTTP allowed only for explicitly configured domains. |
| **Domain allowlist/blocklist** | Configurable lists. Default blocklist includes internal infrastructure domains. |
| **Rate limiting** | Per-session: 30 requests/minute. Global: 100 requests/minute. Configurable. |
| **Content size limit** | Max 5MB per fetch. Reject larger responses. |
| **Content sanitization** | Strip scripts, iframes, tracking pixels. Convert HTML → clean markdown via go-readability + html-to-markdown. |
| **Timeout** | 30s per request. Configurable. |
| **User-Agent** | Identifies as mcpproxy. Respects robots.txt (configurable). |

**Pipeline**:
```
Agent calls: web_fetch("https://docs.python.org/3/library/json.html", intent="json.dumps options")
    │
    ▼
┌──────────────────────────────────────┐
│  1. URL validation (SSRF check)      │
│  2. DNS resolution + IP validation   │
│  3. HTTPS fetch with timeout         │
│  4. HTML → clean markdown            │
│  5. Chunk by headings/sections       │
│  6. Index chunks in session Bleve    │
│  7. Search with intent query         │
│  8. Return relevant sections only    │
└──────────────────────────────────────┘
    │
    ▼
Agent receives ~2KB of relevant docs
+ cache key for full page access
```

**For web_search**: We need a search backend. Options:

| Option | Pros | Cons |
|--------|------|------|
| **SearXNG** (self-hosted) | Free, private, no API keys | User must run SearXNG instance |
| **Brave Search API** | Good free tier (2K/month), privacy-focused | API key required |
| **Google Custom Search** | Most comprehensive | Expensive, API key required |
| **Configurable** | User chooses | More config complexity |

**Recommendation**: Make the search backend configurable with a provider interface. Ship with SearXNG and Brave Search support. SearXNG is the default (self-hosted, no API key needed). Brave Search as the easy alternative (API key, no infra).

**Configuration**:
```json
{
  "web_fetch": {
    "enabled": true,
    "search_provider": "brave",
    "brave_api_key": "BSA-...",
    "searxng_url": "http://localhost:8888",
    "allowed_domains": [],
    "blocked_domains": ["internal.company.com"],
    "rate_limit_per_minute": 30,
    "max_content_size_kb": 5120,
    "timeout_seconds": 30,
    "respect_robots_txt": true,
    "https_only": true
  }
}
```

**Files to create/modify**:

| File | Change |
|------|--------|
| `internal/webfetch/fetcher.go` | New — HTTP client with SSRF protection, DNS validation, content limits |
| `internal/webfetch/sanitizer.go` | New — HTML → markdown conversion, script stripping, content cleaning |
| `internal/webfetch/search.go` | New — search provider interface + implementations |
| `internal/webfetch/search_brave.go` | New — Brave Search API implementation |
| `internal/webfetch/search_searxng.go` | New — SearXNG API implementation |
| `internal/webfetch/ssrf.go` | New — IP validation, DNS rebinding prevention |
| `internal/webfetch/ratelimit.go` | New — per-session and global rate limiting |
| `internal/server/mcp.go` | Modify — register `web_search` and `web_fetch` tools |
| `internal/config/config.go` | Modify — add `WebFetch` config struct |

### Feature 2: Response Compression Pipeline (Smart Truncation)

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
- **Content-aware chunking**: Unlike the current truncator (JSON arrays only), support markdown (by heading), HTML (by section), plain text (by paragraph), and JSON (by array element/object key).

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
| `internal/compress/chunker.go` | New — content-aware chunking (JSON, markdown, HTML, plain text) |
| `internal/server/mcp.go` | Modify — integrate pipeline into `handleCallToolVariant` response path |
| `internal/truncate/truncator.go` | Modify — delegate to compression pipeline when intent is available |
| `internal/config/config.go` | Modify — add `ContextCompression` config struct |

### Feature 3: `index_response` and `search_index` Built-in Tools

**What**: Let agents explicitly index and search across previous tool responses in their session.

**Tool definitions**:
```
index_response(
  cache_key: string,     // Cache key from a truncated response
  content: string,       // OR raw content to index (for non-cached data)
  tags: string[],        // Optional tags for organizing indexed data
) → { index_id, chunks_indexed, size_bytes }

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
- **Cross-response search** — query across all indexed data in the session (tool outputs + fetched web pages)
- **Source tracking** — each result links back to the original tool call/URL and cache key

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

### Feature 5: Session Context Metrics

**What**: Expose real-time context usage metrics so agents and UIs can make informed decisions about when to compress, batch, or prune.

**Tool definition**:
```
context_metrics(
  detail_level: "summary" | "per_tool" | "full"
) → {
    total_input_tokens, total_output_tokens,
    compressed_savings_tokens, compression_ratio,
    tool_calls, compressed_calls,
    web_fetches, web_searches,
    session_duration_minutes,
    per_tool_metrics: { ... }
  }
```

**Integration points**:
- Token counter (`internal/server/tokens/`) already tracks per-call metrics
- Activity service already stores duration and response sizes
- SSE events (`/events`) can stream metrics updates to the web UI
- Existing savings calculator (`tokens/savings.go`) extended with runtime data

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                    MCPProxy Server                              │
│                                                                │
│  ┌──────────────┐    ┌───────────────────────────────────┐    │
│  │ Tool Registry │    │  Context Intelligence Engine       │    │
│  │               │    │                                   │    │
│  │ retrieve_tools│    │  ┌─────────────────────────────┐  │    │
│  │ call_tool_*   │    │  │ Response Compression        │  │    │
│  │ web_search   ─┼───▶│  │  threshold → chunk → index  │  │    │
│  │ web_fetch    ─┼───▶│  │  → query with intent        │  │    │
│  │ index_response│───▶│  │  → extract snippets         │  │    │
│  │ search_index  │    │  └─────────────────────────────┘  │    │
│  │ batch_call    │    │                                   │    │
│  │ context_metr. │    │  ┌─────────────────────────────┐  │    │
│  │ read_cache    │    │  │ Secure Web Fetch Layer       │  │    │
│  └──────────────┘    │  │  SSRF prevention             │  │    │
│         │            │  │  DNS validation               │  │    │
│         │            │  │  HTML → markdown → chunks     │  │    │
│         │            │  │  rate limiting                │  │    │
│         │            │  │  search provider interface    │  │    │
│         │            │  └─────────────────────────────┘  │    │
│         │            │                                   │    │
│         │            │  ┌─────────────────────────────┐  │    │
│         │            │  │ Session Index Manager        │  │    │
│         │            │  │  per-session Bleve           │  │    │
│         │            │  │  tool outputs + web pages    │  │    │
│         │            │  │  TTL-based cleanup           │  │    │
│         │            │  └─────────────────────────────┘  │    │
│         │            └───────────────────────────────────┘    │
│         │                                                      │
│         ▼                                                      │
│  ┌──────────────────┐  ┌────────────┐  ┌─────────────┐       │
│  │ Upstream Manager  │  │ Cache      │  │ Activity    │       │
│  │ (existing)        │  │ (existing) │  │ (existing)  │       │
│  └──────────────────┘  └────────────┘  └─────────────┘       │
└──────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Secure Web Search & Fetch (Feature 1)

**Rationale**: Highest immediate value. Fills a real capability gap for Bedrock/Azure users. Can be shipped independently — no dependency on compression pipeline.

1. Create `internal/webfetch/` package with SSRF-hardened HTTP client
2. Implement HTML → markdown → clean content pipeline
3. Implement search provider interface with Brave Search + SearXNG backends
4. Register `web_search` and `web_fetch` MCP tools
5. Add rate limiting (per-session + global)
6. Integrate with existing cache (fetched pages cached for re-query via `read_cache`)
7. Add `WebFetch` config to `internal/config/config.go`

### Phase 2: Response Compression Pipeline (Feature 2)

**Rationale**: Core value for context management. Builds on session index infrastructure that Phase 1 needs anyway.

1. Create `internal/compress/` package with pipeline, session index manager, and snippet extractor
2. Add content-aware chunking (JSON, markdown, HTML, plain text)
3. Add `ContextCompression` config to `internal/config/config.go`
4. Modify `handleCallToolVariant` in `mcp.go` to route through compression pipeline when intent is available and response exceeds threshold
5. Retroactively apply compression to `web_fetch` results (Phase 1 uses the same pipeline)
6. Update token metrics to track compression savings
7. Add activity metadata for compression events

### Phase 3: Index & Search + Batch Tools (Features 3 & 4)

**Rationale**: Builds on Phase 2's session index infrastructure. Batch calls benefit from compression.

1. Register `index_response` and `search_index` tools in `registerTools()`
2. Implement handlers that delegate to the session index manager
3. Register `batch_call_tools` tool with variant-aware validation
4. Implement parallel/sequential execution with goroutine orchestration
5. Integrate with compression pipeline for aggregate results
6. Add parent-child activity logging for batch calls

### Phase 4: Metrics + Optional Claude Code Hooks (Feature 5 + hooks)

**Rationale**: Aggregation/polish layer. Claude Code hooks are nice-to-have.

1. Create session-scoped metrics collector
2. Register `context_metrics` tool
3. Extend SSE events with metrics stream
4. Ship optional `.claude/settings.json` hook configuration for Claude Code users:
   - Redirect WebFetch → `web_fetch` (built-in)
   - Warn on large Read operations (suggest using upstream tools)
   - Note: NOT mandatory. Claude Code users who don't install hooks still get compression on MCP tool responses.

## Key Differences from claude-context-mode

| Aspect | claude-context-mode | This Proposal |
|--------|-------------------|---------------|
| Runtime | Node.js + SQLite FTS5 | Go + Bleve (already a dependency) |
| Scope | Standalone MCP server + Claude Code hooks | Integrated into proxy response path |
| Compression trigger | External tool call | Automatic on threshold + intent |
| Code execution | 11-language sandbox | Existing JS sandbox (`code_execution`) |
| **Web fetch** | `fetch_and_index` (no SSRF protection) | `web_fetch` + `web_search` with SSRF hardening |
| Index persistence | Per-process SQLite | Per-session Bleve (temp directory) |
| Intent signal | Explicit `intent` param | Reuses Spec 018 `intent_reason` |
| Batch execution | `batch_execute` tool | `batch_call_tools` with variant enforcement |
| Search | SQLite FTS5 + fuzzy | Bleve BM25 + Porter stemming (same as tool discovery) |
| Client support | Claude Code only (requires hooks) | Any MCP client |
| **Search providers** | None (fetch only) | Brave Search, SearXNG (configurable) |

## What We Deliberately Don't Include

1. **Multi-language code execution sandbox**: context-mode supports 11 languages (JS, TS, Python, Shell, Ruby, Go, Rust, PHP, Perl, R, Elixir). The sandbox IS the compression mechanism — code runs in subprocess, raw output stays in subprocess memory, only stdout enters context. mcpproxy achieves the same effect differently: proxy-level compression keeps large data out of context regardless of what language produced it. If an agent needs Python analysis, they connect a Python MCP server upstream (and its output gets compressed by the proxy). Adding 11 language runtimes would add massive binary size, security surface, and maintenance burden for marginal benefit over the proxy approach.

2. **Mandatory Claude Code hooks**: context-mode's hooks are critical because it's a standalone server — without hooks, agents bypass it. mcpproxy doesn't need mandatory hooks because compression is transparent at the proxy level. We ship optional hook configs for users who want extra protection against agents using Bash curl instead of `web_fetch`.

3. **Progressive search throttling**: context-mode throttles after N search calls (calls 1-3 get full results, 4-8 get summaries, 9+ get titles only). mcpproxy's approach of automatic compression on the response path makes this unnecessary — the agent never sees bloated data in the first place.

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| **SSRF via web_fetch** | IP validation (block private ranges), DNS pre-resolution (prevent rebinding), domain allowlist/blocklist, HTTPS enforcement |
| **Search API costs** | SearXNG (self-hosted, free) as default. Brave free tier (2K/month). Rate limiting per session. |
| **Bleve index overhead per session** | Temp directory with TTL cleanup; index size bounded by `max_indexed_bytes` config |
| **Compression removes critical data** | Always preserve cache key for full access; include "X of Y total" metadata |
| **Intent-based filtering too aggressive** | Configurable snippet count and context lines; fallback to summary when <2 matches |
| **Batch tool calls increase blast radius** | Variant enforcement per-call; parallel only for reads; sequential for writes |
| **Session index grows unbounded** | TTL eviction; configurable max entries; cleanup on session close |
| **Web content injection/XSS** | HTML sanitization strips scripts/iframes before markdown conversion; content never rendered as HTML |

## Dependencies

| Dependency | Purpose | Status |
|-----------|---------|--------|
| `github.com/blevesearch/bleve/v2` | BM25 full-text search | Already in go.mod |
| `github.com/go-shiori/go-readability` | HTML → readable content extraction | New dependency |
| `github.com/JohannesKaufmann/html-to-markdown` | Clean markdown conversion | New dependency |
| Search API client (Brave/SearXNG) | Web search | HTTP only, no external library needed |
