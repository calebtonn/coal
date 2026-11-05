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

    subgraph coal["COAL (user-owned, local)"]
        MCP[MCP server]
        D[Daemon :4242]
        CORE[Core: validate, store, index]
        FS[(~/.coal/<br/>manifests/ artifacts/ index.db)]
    end

    CLI[coal CLI]
    WEB[Web UI]
    U((User))

    CC -- hooks --> MCP
    CD -- MCP --> MCP
    CU -- MCP --> MCP
    OT -- MCP --> MCP
    MCP --> CORE
    CLI --> CORE
    WEB --> D
    D --> CORE
    CORE --> FS
    U --> CLI
    U --> WEB
```

Every AI tool reads and writes the same layer. No tool owns it. The user can open the directory and read everything with no software.

## Package dependency graph

```mermaid
flowchart TD
    diamond["@coal/diamond<br/>types, zod schema, file store, FTS5 index"]
    coal["@coal/coal (the installable)<br/>CLI · daemon · MCP server · Claude Code hooks"]
    web["packages/coal/web<br/>Vite + React, built into @coal/coal"]

    coal --> diamond
    web -. served by .-> coal
```

Two packages. `diamond` has no runtime dependency on anything else in the repo. `coal` is everything the user installs. The daemon, MCP server and hooks are subcommands of one binary, not separate packages; they split out only when a second implementer needs one on its own.

## Capture flow (Claude Code)

```mermaid
sequenceDiagram
    participant U as User
    participant CC as Claude Code
    participant H as COAL hooks
    participant S as Storage
    participant Q as Queue worker

    U->>CC: "Build me an HTML budget tracker"
    CC->>CC: writes budget.html
    CC->>H: PostToolUse(Write, budget.html)
    H->>S: match or create manifest, write contexts[] and .coal
    H->>S: register artifact (sha256)
    CC->>H: Stop (every turn)
    H->>S: bump lastActivityAt
    U->>CC: ends session
    CC->>H: SessionEnd (1.5 s budget)
    H->>S: queue/<session>.json
    Q->>S: later: fill state.summary if the model did not
```

## Resume flow

```mermaid
sequenceDiagram
    participant U as User
    participant CLI as coal resume
    participant CC as Claude Code
    participant H as COAL hooks
    participant S as Storage

    U->>CLI: coal resume (in the budget directory)
    CLI->>S: match manifest by .coal or contexts[]
    CLI->>CC: launch with first prompt: open from nextStep
    CC->>H: SessionStart
    H->>S: read manifest
    H-->>CC: systemMessage banner + additionalContext
    CC-->>U: "You left off at CSV export. Continue?"
```

Plain `claude` without the launcher gets the banner and context from the hook, and the first reply (after the user types anything) opens by offering to continue.

## Find and resume flow (any tool)

```mermaid
sequenceDiagram
    participant U as User
    participant T as Any MCP client
    participant M as COAL MCP server
    participant S as Storage

    U->>T: "Find the budget thing I built"
    T->>M: coal_find("budget")
    M->>S: FTS5 query
    S-->>M: [manifest summaries]
    M-->>T: results with ids
    T->>M: coal_read_artifact(id, 0)
    M->>S: artifacts/<sha256>
    S-->>M: body
    M-->>T: HTML
    T-->>U: "Here it is. You left off at: add CSV export."
```

## Data model

```mermaid
erDiagram
    MANIFEST ||--o{ ARTIFACT : contains
    MANIFEST ||--o{ LINK : relates
    MANIFEST {
        uuid id PK
        string intent_summary
        string intent_goal
        string[] intent_tags
        enum status
        datetime createdAt
        datetime updatedAt
        datetime state_lastActivityAt
        string state_nextStep
        string[] state_openQuestions
        string provenance_tool
        string provenance_model
        string provenance_sessionRef
    }
    ARTIFACT {
        string sha256 PK
        string title
        enum type
        string uri
        string mime
        datetime createdAt
        string producer_tool
        string producer_sessionRef
    }
    LINK {
        enum rel
        uuid targetId
    }
```

Storage is file-first. The ER diagram describes the shape, not a relational schema. SQLite holds only an FTS5 virtual table plus a thin lookup table keyed by manifest id, both rebuildable.

## Why these choices

| Decision | Reason |
|---|---|
| Local-first, files as truth | The thesis is user ownership. If it needs our server, it is not user-owned. |
| JSON manifests, one per file | Human-readable, git-able, trivially portable, diffable. |
| SQLite FTS5 | Zero-dependency full-text search, fast enough for years of use on one machine. |
| MCP as the primary interface | It is the one protocol every AI tool is adopting. Building to it means COAL works with tools that do not exist yet. |
| Claude Code hooks for first capture | Deterministic lifecycle hooks, no browser hacks. Cursor, Codex CLI and Gemini CLI have hooks too; one adapter is enough to test the record, and the capture contract in ADR-011 keeps the next ones cheap. |
| TypeScript | MCP SDK, Claude Code hook ecosystem, and the web UI all live there. One language, one toolchain. |

## Phase map

```mermaid
flowchart LR
    P1["Phase 1: Index<br/>capture + find + resume<br/>(this MVP)"]
    P2["Phase 2: Reach<br/>browser capture (claude.ai, chatgpt.com)<br/>semantic search"]
    P3["Phase 3: Attention<br/>policy engine, interrupt gating<br/>Slack + calendar + IDE presence"]
    P4["Phase 4: Portability<br/>encrypted sync, export,<br/>spec v1.0 with other implementers"]
    P1 --> P2 --> P3 --> P4
```

The whiteboard's Sense / Model / Decide / Act loop is Phase 3. The MVP is the storage and interface it will run on.
