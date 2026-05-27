# Working with Output and Rendering

The Open CLI Collective's CLIs are primarily consumed by **agents** (Claude, Codex, etc.) operating on behalf of a user, sometimes directly via tool-invocation and sometimes wrapped in an MCP layer. Token efficiency is therefore not a nice-to-have; it is the design constraint that shapes default output, format flags, and the data/presentation seam.

This document is the family-wide output standard: format principles, the four output-shape flags, formatting conventions, stream discipline, name/ID resolution, and the data-layer ↔ presentation-layer split.

This is **normative for new CLIs.** For new commands inside existing CLIs that have their own internal specs (jtk's `internal/cmd/OUTPUT_SPEC.md`), this doc is the family-wide layer the tool-specific spec instantiates.

Companion pillars:
- `command-surface.md` — verbs, positional-vs-flag, mutation safety, flag conventions.
- `scriptability.md` — `--non-interactive`, exit codes, installer-script ergonomics. Stream-discipline implications live there.
- `working-with-secrets.md` — credential ingress; mandates `--json` for several control-plane outputs (see §2).
- `working-with-state.md` — config + cache; defines the `refresh` command this doc cross-refs in §7.

**When this doc appears to conflict with `working-with-secrets.md` or `working-with-state.md`, those win** — they are the older normative source on the surfaces they cover.

---

## §1 Design principles

1. **Text is the primary format.** Stable `Key: Value` blocks and pipe-delimited tables are parseable without JSON overhead. An agent reading `Status: In Code Review` needs no less capability than one reading `{"status":"In Code Review"}` — and text wins on token density.
2. **Default output is contextually rich, not minimal.** An agent reasoning about an issue needs labels, sprint, parent, points, components — not just key/summary/status. The default output carries the semantic weight required for decision-making without flags.
3. **Administrative detail hides behind `--extended`.** Anything schema-level, rarely-used, or audit-oriented requires the flag. The test: would a developer need this monthly/yearly vs. daily? Monthly/yearly → `--extended`.
4. **The tool knows the instance.** A one-time `<tool> init` plus periodic cache `refresh` (see `working-with-state.md` §4.6) lets the CLI resolve custom fields, users, project types, statuses, link types, and workflow transitions without per-command API calls.

---

## §2 JSON scope

JSON is **default-off for upstream resource read output** — lists/gets/searches that hit the upstream service for the user's actual work (Jira issues, Confluence pages, Slack channels, Salesforce records). New CLIs MUST NOT add `-o json` or `--json` to those commands. The contextually-rich pipe-delimited / key:value text format (per §4) is the default *and* the only format for that surface.

This scope deliberately does NOT cover local-state reads (`config show`, `me`, diagnostic surfaces) — those are control-plane outputs and follow the table below.

JSON IS the natural format for **control-plane signals and round-trip payloads**, in the cases the other docs mandate:

| Surface | Status | Owning standard |
|---|---|---|
| `<tool> config show --json` | PARTIAL CURRENT PRECEDENT — shipped in `gro` per-command (`google-readonly/internal/cmd/config/config.go:88`), in `slck` via global `-o json` (`slack-chat-api/internal/cmd/config/show.go:72`), in `nrq` via global `-o json` (`newrelic-cli/internal/cmd/configcmd/config.go:333`). jtk and cfl have not adopted it yet. | `working-with-secrets.md` §1.6 |
| `<tool> set-credential --json` | STANDARD TARGET — no CLI ships this yet. New CLIs adding `set-credential` MUST ship `--json` from the start. | `working-with-secrets.md` §1.5.2 |
| `_migration` field at top level of JSON responses on a run where migration occurred | Normative for any JSON output path; see §5 for routing. | `working-with-secrets.md` §1.8 |
| Round-trip export — `jtk automation export` and equivalents | CURRENT PRECEDENT — shipped. Bypasses the global flag system; writes JSON directly to stdout. | tool-local |

The principle: **a user reading a list of issues should not get JSON; a script verifying `set-credential` succeeded reasonably should.** Resource read commands MUST NOT add JSON just because cobra makes it easy.

The three-tier classification ("STANDARD TARGET" / "PARTIAL CURRENT PRECEDENT" / "CURRENT PRECEDENT") in the table is deliberate — when reviewing a CLI's JSON surface, distinguish "the standard mandates this and no one has shipped it" from "shipped here, not there" from "shipped and stable."

---

## §3 Output-shape flags

Four global flags form a coordinate system for "how should the result render?" — orthogonal to *what* is fetched. New list/get commands MUST support each flag **where it is meaningful for that command's output type**, and MUST NOT register a flag where it is a no-op (so `--help` accurately advertises the surface). The default-applicability is: `--id` MUST be supported on every list/get command; `--extended`/`--fulltext`/`--fields` are supported when there is corresponding surface (admin/schema columns, prose cells, per-row columns). A command MAY omit any of the three latter flags when the underlying data has no such surface (e.g., an ID-only listing). All four are long-only (cross-ref `command-surface.md` §7.2).

- **`--id`** — emit only primary identifiers. Overrides ALL other output-shape flags including `--fields`. Contract: machine-friendly output, one identifier per line, suitable for piping into `xargs`.
- **`--extended`** — widen the default column set with admin/schema/audit fields. Implies `--fulltext`.
- **`--fulltext`** — disable truncation of prose cells (descriptions, comment bodies).
- **`--fields <csv>`** — explicit column selection. Replaces the default set entirely. Accepts header labels, upstream field IDs, or human names; matching is case-insensitive. Unknown field name → error listing the valid set for that resource. Empty CSV → falls back to the default set. When a `--fields` selection contains a prose column (descriptions, comment bodies) the column is truncated per §4.4 unless `--fulltext` is also passed.

**Mental model:** there is a default column set → `--extended` widens it → `--fields` overrides the whole selection → `--id` short-circuits to identifiers only and overrides everything above. **Truncation is independent of column selection:** `--extended` implies `--fulltext` regardless of whether `--fields` is also passed. `--fields` overrides only the column set, never the truncation behavior. To force truncation off while narrowing columns, use `--fields ... --fulltext`.

Reference implementation of the projection registry pattern (the `--fields` machinery): `atlassian-cli/tools/jtk/internal/present/projection/spec.go` — per-presenter `Registry`, `ColumnSpec` with `Fetch` to derive the minimum upstream field set, alias / identity / extended flags.

---

## §4 Formatting conventions

### §4.1 Lists — pipe-delimited tables

- Headers in `ALL_CAPS`.
- Separator: ` | ` (space-pipe-space).
- Empty/null values: `-`.
- `--extended` adds columns; it does not replace default columns.
- Sorted most-recent-first where time-ordered (sprints, releases, deploys, etc.).
- **Separator collision:** the ` | ` triplet (space-pipe-space) is reserved. When a cell value contains it, the presentation layer MUST replace the embedded ` | ` with a single space (or a similar non-collision substitution) before emission. A naked `|` inside a value, surrounded by other characters, is fine — the triplet is the discriminator. Document the substitution choice per-CLI; do not silently mangle without telling the user.

```
KEY | STATUS | TYPE | PTS | ASSIGNEE | SUMMARY
MON-4810 | In Code Review | SDLC | 5 | Aaron Wong | Audit accessibility on CapOne surfaces
MON-4807 | In Code Review | SDLC | 3 | Aaron Wong | Make CapOne key-stack authoritative
MON-4809 | Backlog | SDLC | - | - | Bump PostHog sampling to 100% for CapOne sessions
```

### §4.2 Single resources — header + key:value block

- First line: `ID  Name` (two spaces between).
- Attribute lines: `Key: Value   Key: Value` (three spaces between same-line pairs).
- Optional rows (`Labels`, `Components`) appear ONLY when non-empty.
- Description: blank line → `Description:` label → body text, always last.

```
MON-4810  Audit accessibility on CapOne surfaces
Status: In Code Review   Type: SDLC   Priority: Medium   Points: 5
Assignee: Aaron Wong   Updated: 2026-04-16
Sprint: MON Sprint 70 (active)
Parent: MON-3165 — 2025-26 Capital One launch (Epic)

Description:
Perform an accessibility-focused review and remediation pass...
```

### §4.3 Dates — ISO-8601

- Default: `YYYY-MM-DD`.
- `--extended`: full ISO 8601 with timezone (`2026-04-16T07:16:24+0000`).
- Missing/not-yet-set: `-`.
- Relative formatting ("3 days ago") is **not supported.** Agents reason in absolute time and humans can convert; the saved tokens add up.

### §4.4 Text truncation

- Descriptions and comment bodies truncate in default mode.
- Truncation trailer: `[truncated — use --fulltext for complete body]`.
- `--fulltext` disables truncation; `--extended` implies `--fulltext`.

### §4.5 Errors — plain prose to stderr

- No structured format.
- Ambiguity errors list all matches (see §7).
- Unknown-entity errors suggest `<tool> refresh <resource>` where a cache could be stale (see `working-with-state.md` §4.6).

### §4.6 Mutation success output — mirror the post-state

A mutation's success output mirrors the `get` output of the affected entity. The caller sees the post-state in a single call — no follow-up fetch required.

- `--id` on any mutation emits only the affected entity's identifier.
- Delete / archive / remove: confirmation line only (`Deleted MON-4820`, `Archived MON-4820`).
- After create: re-fetch if the upstream API returns incomplete data from the create response (Jira does this; treat it as the norm).

---

## §5 Stdout/stderr stream discipline

The two streams have separate semantic roles. New commands MUST respect this discipline — agents and scripts depend on it.

- **stdout** = primary data. List rows, single-resource blocks, JSON envelopes for the §2 control-plane surfaces. When a command's contract is a JSON response, any `_migration` block goes here at the **top level** of that response per `working-with-secrets.md` §1.8.
- **stderr** = side channel for human-readable runs. Prompts, progress, warnings, deprecation notices, the human-readable `Migrated <field> from <source> to <dest>` line for state migrations, diagnostic prose, error prose.

A JSON output path does NOT duplicate the human migration notice on stderr; that is the whole point of the `_migration` field — automation gets it inline, humans get it on stderr, and the two paths do not interleave.

The user can always `1>` data and `2>` chatter without contamination.

Renderer reference: `atlassian-cli/shared/present/render.go:15` — `Render(model *OutputModel, style Style) RenderedOutput` returns a struct with separate `Stdout` and `Stderr` strings. Routing is **explicit** via section types (`MessageSection` etc. set `StreamStderr`); the zero value routes to stdout. Do not infer routing from section type alone.

---

## §6 Pagination

Every paginated command takes:

- **`-m, --max <int>`** — page size. Default **50** unless documented otherwise; a non-50 default requires a justification in the command's help text.
- **`--next-page-token <string>`** — cursor (long-only; rarely typed by hand).

A paginated list MUST append a continuation line when more results exist:

```
More results available (next: eyJzdGFydEF0IjoxMH0)
```

Absence of the continuation line signals a complete result set. The token is opaque; consumers MUST NOT parse it.

---

## §7 Name/ID resolution behavior

All entity-reference flags and positionals that accept a human-readable form MUST route through the CLI's instance cache (`working-with-state.md` §4 — `<tool> refresh` populates / invalidates it):

- **Unique match** (by name, email, key, or ID) → resolve silently.
- **Ambiguous** → fail, listing all matches with disambiguating identifiers.
- **No match + looks like a raw ID** → pass through unchanged; let the upstream return its own 404.
- **No match + looks like a name** → fail with a hint to `<tool> refresh <resource>` if this resource was recently added.

```
$ jtk issues assign MON-4820 "John Smith"
Ambiguous user "John Smith" — 3 matches:
  5a1b2c... | John Smith | john.smith@ibm.com
  6d3e4f... | John Smith | jsmith@ibm.com
  7g8h9i... | John A. Smith | jasmith@ibm.com
Use account ID or email to disambiguate.
```

Reference: jtk's `resolve.New(client).Board/Sprint/User` at `atlassian-cli/tools/jtk/internal/cmd/{boards,sprints,issues}/...`.

---

## §8 Color stance

Production **resource output has no color.** New CLIs MUST NOT add color to list/get/search output.

Color in **setup and diagnostic surfaces** — `init` wizards, `me`, `config test`, `config clear`, success/error glyphs on mutation confirmations — is acceptable and consistent with existing CLIs (jtk/cfl/sfdc/nrq use `fatih/color` decorators; gro uses `lipgloss`). Where any color is rendered, a `--no-color` flag MUST be supported and respected; `isatty` is NOT checked.

Color choice: prefer `fatih/color` (the family default; honors a `--no-color` toggle via a package-level var) or `lipgloss` (honors `NO_COLOR` env natively). Either is fine; pick one per CLI and stick with it.

---

## §9 Data layer ↔ presentation layer

### §9.1 The seam

A CLI typically has three layers:

- **Data layer** — the API client. Talks to the upstream service; returns typed Go values. Does not format anything for display. Owns retry, pagination, decoding, error classification.
- **Presentation layer** — pure functions from typed data to rendered text. No I/O it was not handed. No network. No time-of-day or env reads.
- **Command layer** — cobra glue. Parses flags, calls the data layer, hands the result to the presentation layer, writes to stdout/stderr.

The presentation layer SHOULD be testable in isolation with no mocks beyond the typed input value. The command layer SHOULD be testable with a mocked data layer (typically `httptest`) and the real presentation layer.

### §9.2 Presenter shape

The presenter takes typed Go values and returns rendered text — either as a pure return value (the canonical shape, `Render(model, style) RenderedOutput` at `atlassian-cli/shared/present/render.go:15`) OR via caller-provided writers. Either form is acceptable; the invariant is that the presenter does no I/O it was not handed and reads no environmental state.

### §9.3 Data-layer projection

Token efficiency starts at the data layer, not the presenter. When the upstream API supports server-side field selection (Jira's `?fields=`, Salesforce's `SELECT <cols>`), the data layer SHOULD use it — deserializing and discarding fields the presenter will not render is wasted work AND wasted tokens if any of those fields end up in error messages or logs.

Guideline: for small or unknown-size fields, prefer over-fetching — deserialize the field and let the presentation layer drop it. For large prose fields (rendered descriptions, comment bodies, attachment payloads, large blob columns), narrow at the data layer when the command does not render them. The cost asymmetry is what drives the split: a missed small field is a cheap re-fetch; an over-fetched comment thread can blow a context window in one call.

### §9.4 Provider abstraction

If a CLI is designed against a class of upstream providers — e.g., a code-review CLI that supports GitHub, GitLab, and Bitbucket — the data layer SHOULD define a provider-neutral domain model and adapt each upstream into it. A confluence-only CLI does not need this abstraction.

This doc does not prescribe the abstraction shape; that lives with the CLI's own design. If the family acquires a second multi-provider CLI, the pattern will be lifted here.

### §9.5 Testing presentation — golden files

Golden-file tests for presenters are the recommended pattern: write a fixture input, render it, compare against a checked-in golden output, fail loud on diff. No CLI in the family currently has output goldens — cfl has goldens for markdown↔XHTML conversion (`atlassian-cli/tools/cfl/pkg/md/fidelity_test.go`) but not for command output. jtk has inline-literal assertions (`atlassian-cli/tools/jtk/internal/cmd/users/users_test.go:262` notes "Exact-string golden locks the full output shape") but no `.golden` files.

This doc records goldens as a recommended pattern, NOT a normative requirement, pending a `cli-common` test helper that handles the `-update` flag and diff formatting. When that helper lands, this section is upgraded to normative.

---

## §10 Current divergences

The new docs are forward-looking. The following current divergences from this standard are called out so a future audit knows what to fix and so new CLIs do not cargo-cult them.

- **Resource-read JSON is widespread.** slck, sfdc, nrq, cfl all expose JSON on resource reads via global `-o json`; gro exposes it via per-command `--json`. Only jtk holds the §2 line (reserves JSON for `automation export`). New CLIs MUST NOT add resource-read JSON.
- **gro has no root `--no-color` flag** — `google-readonly/internal/cmd/root/root.go:107-109` registers a global `--verbose` and the credstore backend flag but no color flag. Its lipgloss styling at `google-readonly/internal/view/view.go:34` honors the `NO_COLOR` env natively, which papers over the gap for users who set the env, but the missing flag is a divergence from §8.
- **No CLI gates color on `isatty`.** This is intentional and consistent with §8 — `--no-color` is the documented opt-out.
- **No CLI has output goldens.** Per §9.5 this is recommended-not-normative pending the cli-common helper.

Command-surface divergences (init flag-skip failures, missing `set-credential`) are catalogued in `command-surface.md` §9. Scriptability divergences (missing `--non-interactive`, `me` not exiting non-zero) are catalogued in `scriptability.md` §9.
