# Intent Manifest Specification v0.1

Status: Draft. Version 0.1 freezes at the Phase 1 retro (T14); until then additive changes land in 0.1 (ADR-019). The schema file is `spec/intent-manifest.schema.json`. Examples are in `spec/examples/`.

## Purpose

An Intent Manifest is a portable, user-owned record of one unit of intent: what a person is trying to accomplish, what has been produced toward it, and where it stands. Any AI tool may read or write manifests on behalf of the user. The user may read, edit, move, or delete them with any text editor.

## Design rules

1. **Plain JSON.** No binary, no custom encoding. One manifest per file.
2. **User's words first.** `intent.summary` should be the user's phrasing when available, not a model's paraphrase.
3. **Artifacts with a uri are identified by (manifest, uri) and keep their history in versions[].** Artifacts without a uri are identified by content hash. The top-level hash always points at the latest version.
4. **Provenance is mandatory.** Every manifest records which tool wrote it. A user can always answer "where did this come from."
5. **Additive evolution.** New versions add optional fields. Readers must ignore unknown fields.
6. **No secrets.** Manifests must not contain credentials, tokens, or session cookies. `sessionRef` is an opaque locator, never an auth token. If a secret reaches an artifact body anyway, `coal purge <hash>` deletes the body and marks every version that used it `skipped: "purged"`; this is the one exception to never deleting user data.

## Fields

### Top level

| Field | Type | Required | Description |
|---|---|---|---|
| `$schema` | string | yes | `https://coalmine.io/schema/intent-manifest/0.1.json` |
| `version` | string | yes | `"0.1"` |
| `id` | string (UUIDv7) | yes | Stable identity |
| `rev` | integer (1 or more) | no | Write revision. Incremented on every write. A writer passes back the `rev` it read and gets a conflict if it moved. |
| `intent` | Intent | yes | |
| `status` | enum | yes | `active` \| `paused` \| `done` \| `abandoned` |
| `artifacts` | Artifact[] | yes (may be empty) | |
| `state` | State | yes | |
| `provenance` | Provenance | yes | Who created this manifest |
| `links` | Link[] | no | Relations to other manifests |
| `contexts` | Context[] | no | Where this intent is relevant; drives surfacing without search |
| `owner` | Owner | no | For future multi-user and sync |
| `privacy` | Privacy | no | Defaults to `{ "scope": "local" }` |
| `createdAt` | ISO 8601 UTC | yes | |
| `updatedAt` | ISO 8601 UTC | yes | |
| `ext` | object | no | Namespaced extensions, e.g. `ext["com.example.tool"]` |

### Intent

| Field | Type | Required | Description |
|---|---|---|---|
| `summary` | string (1 to 200 chars) | yes | One line |
| `goal` | string | no | Longer statement; a model may write this |
| `source` | enum | no | `user` \| `model`. Who wrote `summary`. Readers should prefer `user`. Defaults to `model` when absent. |
| `tags` | string[] | no | Free-form |

### Artifact

| Field | Type | Required | Description |
|---|---|---|---|
| `hash` | string | yes | `sha256:<hex>` of the latest version |
| `title` | string | yes | Human name |
| `type` | enum | yes | `html` \| `code` \| `doc` \| `data` \| `image` \| `other` |
| `mime` | string | yes | e.g. `text/html` |
| `uri` | string | no | Original location (`file://`, `https://`). When present, the manifest and `uri` together identify the artifact, and `versions` holds its history. |
| `size` | integer | no | Bytes of the latest version |
| `createdAt` | ISO 8601 | yes | When the latest version was recorded |
| `producer` | Provenance | yes | Which tool produced the latest version (may differ from manifest provenance) |
| `versions` | ArtifactVersion[] | no | Every recorded body, oldest first. At least one entry when present. The last entry matches the top-level `hash`, `size`, `createdAt` and `producer`. |
| `skipped` | string | no | Why the latest body was not copied, for example a possible secret. Bodies over 5 MiB are recorded as skipped `"too large"` with no `size`. |
| `ext` | object | no | Namespaced extensions for this artifact, e.g. `ext["dev.agent-trace"]` |

Simple readers can ignore `versions` and use the top-level fields, which always describe the latest version.

When an [Agent Trace](https://agent-trace.dev/) record exists for a code artifact, the artifact references it under `ext["dev.agent-trace"]` as `{"uri": "...", "hash": "sha256:..."}`. Either field may be left out. COAL does not add line-level attribution itself: Agent Trace records which lines came from which conversation, and COAL records the intent and state around them (ADR-015).

### ArtifactVersion

| Field | Type | Required | Description |
|---|---|---|---|
| `hash` | string | yes | `sha256:<hex>` |
| `size` | integer | no | Bytes |
| `createdAt` | ISO 8601 | yes | When this version was recorded |
| `producer` | Provenance | yes | Which tool produced this version |
| `skipped` | string | no | Why this body was not copied |

### State

| Field | Type | Required | Description |
|---|---|---|---|
| `lastActivityAt` | ISO 8601 | yes | |
| `nextStep` | string | no | What the user or tool said comes next |
| `openQuestions` | string[] | no | Unresolved items |
| `summary` | string | no | Short narrative of where things stand |

### Provenance

| Field | Type | Required | Description |
|---|---|---|---|
| `tool` | string | yes | e.g. `claude-code`, `claude.ai`, `cursor`, `coal-cli`, `human` |
| `model` | string | no | e.g. `claude-opus-5-5` |
| `sessionRef` | string | no | Opaque locator the tool can use to reopen the session. The schema rejects values that start like common tokens (`Bearer `, `sk-`, `xoxb-`, `ghp_`, `github_pat_`, `AKIA`). |
| `capturedBy` | string | no | The adapter that wrote the record, e.g. `coal-adapter-claude-code@0.1.0` |

### Link

| Field | Type | Required | Description |
|---|---|---|---|
| `rel` | enum | yes | `related` \| `supersedes` \| `supersededBy` \| `derivedFrom` \| `partOf` |
| `id` | string (UUIDv7) | yes | Target manifest |

### Context

A context is a place or moment where this intent is relevant. When a tool reports it is in a matching context (the working directory is under `key`, the calendar event is starting, the channel is open), the manifest is surfaced without a search. This is the hook anticipation hangs on.

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | enum | yes | `repo` \| `path` \| `url` \| `calendar` \| `channel` \| `project` \| `other` |
| `key` | string | yes | A stable identifier: repo remote URL, absolute path, calendar event id, channel id |
| `label` | string | no | Human name for display |

### Owner

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Local id or DID in future versions |

### Privacy

| Field | Type | Required | Description |
|---|---|---|---|
| `scope` | enum | yes | `local` \| `shared` \| `public` |

## Conformance

A **writer** conforms if every manifest it produces validates against the schema and respects the design rules.

A **reader** conforms if it accepts any valid manifest, ignores unknown fields, and never requires fields marked optional.

A **store** conforms if a user can retrieve every manifest and every referenced artifact body without the store's software (for the reference implementation: the files in `~/.coal/`).

## Open questions for v1.0

- Should `intent` support hierarchy natively, or is `links[rel=partOf]` enough? ADR-017 (Places, PR 19, Proposed) answers this with `links[rel=partOf]` plus an optional manifest `kind` (`project` or `area`). Proposed, not decided.
- How should artifacts larger than a few MB be handled (reference only, no copy)? ADR-017 (PR 19, Proposed) proposes versions kept by reference, built on `versions[]`: such a version records `uri`, `size` and a new `modifiedAt`, sets `skipped: "reference"`, and has no `hash`, so `hash` would become optional on a version only when `skipped` is set. Proposed, not decided; the schema does not carry these fields yet.
- Signing: should manifests carry a signature so other implementers can trust provenance?
- Policy objects (the attention and interruption rules from Phase 3): separate schema, or an `ext` namespace on the manifest?
