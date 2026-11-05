# Intent Manifest Specification v0.1

Status: Draft. The schema file is `spec/intent-manifest.schema.json`. Examples are in `spec/examples/`.

## Purpose

An Intent Manifest is a portable, user-owned record of one unit of intent: what a person is trying to accomplish, what has been produced toward it, and where it stands. Any AI tool may read or write manifests on behalf of the user. The user may read, edit, move, or delete them with any text editor.

## Design rules

1. **Plain JSON.** No binary, no custom encoding. One manifest per file.
2. **User's words first.** `intent.summary` should be the user's phrasing when available, not a model's paraphrase.
3. **Artifacts are referenced by content hash.** A manifest remains valid when files move. The `uri` is a hint; the `hash` is the identity.
4. **Provenance is mandatory.** Every manifest records which tool wrote it. A user can always answer "where did this come from."
5. **Additive evolution.** New versions add optional fields. Readers must ignore unknown fields.
6. **No secrets.** Manifests must not contain credentials, tokens, or session cookies. `sessionRef` is an opaque locator, never an auth token.

## Fields

### Top level

| Field | Type | Required | Description |
|---|---|---|---|
| `$schema` | string | yes | `https://coalmine.io/schema/intent-manifest/0.1.json` |
| `version` | string | yes | `"0.1"` |
| `id` | string (UUIDv7) | yes | Stable identity |
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
| `hash` | string | yes | `sha256:<hex>` |
| `title` | string | yes | Human name |
| `type` | enum | yes | `html` \| `code` \| `doc` \| `data` \| `image` \| `other` |
| `mime` | string | yes | e.g. `text/html` |
| `uri` | string | no | Original location (`file://`, `https://`) |
| `size` | integer | no | Bytes |
| `createdAt` | ISO 8601 | yes | |
| `producer` | Provenance | yes | Which tool produced this artifact (may differ from manifest provenance) |

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
| `sessionRef` | string | no | Opaque locator the tool can use to reopen the session |
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

- Should `intent` support hierarchy natively, or is `links[rel=partOf]` enough?
- How should artifacts larger than a few MB be handled (reference only, no copy)?
- Signing: should manifests carry a signature so other implementers can trust provenance?
- Policy objects (the attention and interruption rules from Phase 3): separate schema, or an `ext` namespace on the manifest?
