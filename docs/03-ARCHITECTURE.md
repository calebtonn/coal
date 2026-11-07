# Architecture

## Where COAL sits

```mermaid
flowchart LR
    subgraph tools["AI tools (any vendor)"]
        CC[Claude Code]
        CD[Claude Desktop]
        CU[Cursor]
        OT[Other MCP clients]
    end

    subgraph coal["COAL (user-owned, local): the coal binary"]
        HOOK[coal hook]
        MCP[coal mcp]
        D[coal serve :4242]
        CLI[coal CLI commands]
        CORE[Core: validate, queue, store, index]
        FS[(~/.coal/<br/>manifests/ artifacts/ queue/ index.db)]
    end

    WEB[Web UI]
    U((User))

    CC -- hooks --> HOOK
    CC -- MCP --> MCP
    CD -- MCP --> MCP
    CU -- MCP --> MCP
    OT -- MCP --> MCP
    HOOK --> CORE
    MCP --> CORE
    CLI --> CORE
    WEB --> D
    D --> CORE
    CORE --> FS
    U --> CLI
    U --> WEB
```

Every AI tool reads and writes the same layer. No tool owns it. The user can open the directory and read everything with no software. Hooks write to storage through the `coal` binary, never through the MCP server: the MCP server is how a model reads and records on purpose, the hooks are how capture happens without the model doing anything.

## Package dependency graph

```mermaid
flowchart TD
    cmd["cmd/coal<br/>one binary: CLI, daemon, MCP server, hooks"]
    daemon["internal/daemon<br/>HTTP API, embedded UI"]
    mcpserver["internal/mcpserver<br/>MCP tools and resource"]
    fold["internal/fold<br/>folds capture events into manifests"]
    summarize["internal/summarize<br/>transcript reader, opt-in summarizer"]
    importer["internal/importer<br/>coal import claude-code"]
    app["internal/app<br/>store and index behind one API"]
    capture["internal/capture<br/>capture contract, Claude Code translation"]
    queue["internal/queue<br/>event files, single writer"]
    marker["internal/marker<br/>.coal marker, project root, matching"]
    store["internal/store<br/>JSON files, locks, rev, artifacts, trash, git"]
    index["internal/index<br/>SQLite FTS5"]
    manifest["internal/manifest<br/>types, ids, validation"]
    metrics["internal/metrics"]
    paths["internal/paths"]
    spec["spec/<br/>JSON Schema, embedded"]
    web["web/<br/>Svelte 5 UI, built with Bun, embedded"]

    cmd --> daemon
    cmd --> mcpserver
    cmd --> fold
    cmd --> importer
    cmd --> summarize
    cmd --> queue
    web -. "go:embed" .-> cmd
    daemon --> app
    mcpserver --> app
    mcpserver --> metrics
    fold --> app
    fold --> capture
    fold --> marker
    fold --> summarize
    fold --> metrics
    importer --> app
    importer --> marker
    importer --> summarize
    summarize --> app
    summarize --> capture
    queue --> capture
    app --> store
    app --> index
    store --> manifest
    store --> paths
    index --> manifest
    capture --> manifest
    marker --> manifest
    manifest --> spec
```

One Go module, one binary (ADR-018). `cmd/coal` holds the cobra commands; the daemon, MCP server and hooks are subcommands of it, not separate programs. Everything under `internal/` is private to the module. `spec/` holds the JSON Schema and embeds it so the binary validates with the same file the spec publishes. `web/` holds the Svelte 5 UI, built with Bun and embedded with `go:embed`. `internal/manifest` depends on nothing else in the repo but `spec/`; `internal/app` puts the store and the index behind one API that the CLI, daemon, MCP server and fold all use, so there is one place where a write validates, locks, bumps `rev`, updates the index and records history.

## Capture flow (Claude Code)

```mermaid
sequenceDiagram
    participant U as User
    participant CC as Claude Code
    participant H as coal hook
    participant Q as Queue and fold (single writer)
    participant S as Storage

    U->>CC: "Build me an HTML budget tracker"
    CC->>H: UserPromptSubmit(prompt)
    H->>Q: queue/<eventId>.json, drain in process
    Q->>S: keep the first prompt in sessions/
    CC->>CC: writes budget.html
    CC->>H: PostToolUse(Write, budget.html)
    H->>Q: queue, drain
    Q->>S: match or create manifest, write contexts[] and .coal
    Q->>S: register artifact (sha256, a new version if the uri is known)
    CC->>H: Stop (every turn)
    H->>Q: queue, drain
    Q->>S: bump lastActivityAt, keep the last reply
    U->>CC: ends session
    CC->>H: SessionEnd (1.5 s budget)
    H->>Q: queue/<eventId>.json only, exit
    Q->>S: later (daemon tick, next SessionStart or coal queue drain): extract state.summary and nextStep unless the model recorded state
    Q->>S: queue a summarize job when config.summarizer is set
```

Hooks never run git. A hook opens the store with deferred history, so each write touches `.history-pending`; the daemon tick, `coal queue drain` or any non-hook command commits `manifests/` in a batch under the store-wide `.git.lock` once that marker has been quiet for two seconds (ADR-014).

## Resume flow

```mermaid
sequenceDiagram
    participant U as User
    participant CLI as coal resume
    participant CC as Claude Code
    participant H as coal hook
    participant S as Storage

    U->>CLI: coal resume (in the budget directory)
    CLI->>S: match manifest by .coal or contexts[]
    CLI->>CC: launch with first prompt: open from nextStep
    CC->>H: SessionStart
    H->>S: drain the queue, read manifest
    H-->>CC: systemMessage banner + additionalContext
    CC-->>U: "You left off at CSV export. Continue?"
```

Plain `claude` without the launcher gets the banner and context from the hook, and the first reply (after the user types anything) opens by offering to continue.

## Find and resume flow (any tool)

```mermaid
sequenceDiagram
    participant U as User
    participant T as Any MCP client
    participant M as coal mcp
    participant S as Storage

    U->>T: "Find the budget thing I built"
    T->>M: coal_find("budget")
    M->>S: FTS5 query
    S-->>M: [manifest summaries]
    M-->>T: results with ids
    T->>M: coal_read_artifact(id, artifactIndex 0)
    M->>S: artifacts/<sha256 hex>
    S-->>M: body
    M-->>T: HTML
    T-->>U: "Here it is. You left off at: add CSV export."
```

## Data model

```mermaid
erDiagram
    MANIFEST ||--o{ ARTIFACT : contains
    MANIFEST ||--o{ CONTEXT : anchors
    MANIFEST ||--o{ LINK : relates
    ARTIFACT ||--o{ ARTIFACT_VERSION : versions
    MANIFEST {
        uuid id PK
        int rev
        string intent_summary
        enum intent_source
        string intent_goal
        string[] intent_tags
        enum status
        datetime createdAt
        datetime updatedAt
        datetime state_lastActivityAt
        string state_summary
        string state_nextStep
        string[] state_openQuestions
        string provenance_tool
        string provenance_model
        string provenance_sessionRef
        object ext
    }
    ARTIFACT {
        string uri "identity with the manifest when present"
        string hash "latest version"
        string title
        enum type
        string mime
        int size
        datetime createdAt
        string producer_tool
        string producer_sessionRef
        string skipped "why the latest body was not copied"
        object ext
    }
    ARTIFACT_VERSION {
        string hash
        int size
        datetime createdAt
        string producer_tool
        string skipped
    }
    CONTEXT {
        enum kind
        string key
        string label
    }
    LINK {
        enum rel
        uuid targetId
    }
```

Storage is file-first. The ER diagram describes the shape, not a relational schema. An artifact with a `uri` is identified by (manifest, `uri`) and its history is `versions[]`, oldest first; one without a `uri` is identified by its hash; the top-level artifact fields always describe the latest version. SQLite holds only an FTS5 virtual table plus a thin lookup table keyed by manifest id, both rebuildable.

## Why these choices

| Decision | Reason |
|---|---|
| Local-first, files as truth | The thesis is user ownership. If it needs our server, it is not user-owned. |
| JSON manifests, one per file | Human-readable, git-able, trivially portable, diffable. |
| SQLite FTS5 | Zero-dependency full-text search, fast enough for years of use on one machine. |
| MCP as the primary interface | It is the one protocol every AI tool is adopting. Building to it means COAL works with tools that do not exist yet. |
| Claude Code hooks for first capture | Deterministic lifecycle hooks, no browser hacks. Cursor, Codex CLI and Gemini CLI have hooks too; one adapter is enough to test the record, and the capture contract in ADR-011 keeps the next ones cheap. |
| Go, one binary (ADR-018, supersedes ADR-005) | The proof of concept on `poc/go` ran the whole definition of done, so main adopted it on 2026-11-07. One static binary with pure-Go SQLite (no cgo) is what hooks that must finish in milliseconds, a daemon and an MCP server all want; there is no runtime or native build to install. |
| Artifacts identified by (manifest, uri) with `versions[]` (ADR-019) | A file that is edited again is the same artifact with a new version, not a second artifact. Every addition to schema 0.1 for this (`rev`, `versions[]`, `skipped`, artifact `ext`) is optional, so simple readers keep using the top-level fields. |

## Phase map

```mermaid
flowchart LR
    P1["Phase 1: Record<br/>capture + find + resume<br/>(this MVP)"]
    P2["Phase 2: Reach<br/>Cursor and Codex adapters, coal import<br/>chat apps, semantic search"]
    P3["Phase 3: Attention<br/>policy engine, interrupt gating<br/>Slack + calendar + IDE presence"]
    P4["Phase 4: Portability<br/>encrypted sync, export,<br/>spec v1.0 with other implementers"]
    P1 --> P2 --> P3 --> P4
```

The whiteboard's Sense / Model / Decide / Act loop is Phase 3. The MVP is the storage and interface it will run on.
