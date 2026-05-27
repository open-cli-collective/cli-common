# Working with the Command Surface

The Open CLI Collective ships a family of Go CLIs that, from a user's perspective, should feel like dialects of one language. This document is the family-wide command-surface standard: verbs, argument shapes, mutation safety, async operations, flag conventions, and naming hygiene.

This is **normative for new CLIs.** For new commands inside existing CLIs that have their own internal specs (jtk's `internal/cmd/GUARDRAILS.md`), this doc is the family-wide layer the tool-specific spec instantiates — when those drift apart, this doc wins for new surface and the tool's spec is updated to match.

Companion pillars:
- `working-with-secrets.md` — secret state, credential ingress, `--overwrite` for keyring writes.
- `working-with-state.md` — non-secret on-disk state (config + cache), the `refresh` command.
- `output-and-rendering.md` — output shape (`--id` / `--extended` / `--fulltext` / `--fields`), stdout/stderr discipline.
- `scriptability.md` — `--non-interactive`, exit codes, `--profile`, installer-script ergonomics.

This doc does not duplicate those — it cross-references them at the seams. **When this doc appears to conflict with `working-with-secrets.md` or `working-with-state.md`, those win** (they predate this one and are the source of truth for the surfaces they govern). See `docs/README.md` for the full conflict-resolution order.

---

## §1 Verb language

A small, fixed verb set with specific meanings. Pick the right verb and the user knows the safety profile and lifecycle of the operation before reading any docs. Reuse before invent — synonyms are how the family surface gets messy.

| Verb | Meaning | Alias |
|---|---|---|
| `create` | Bring a new top-level resource into existence | — |
| `delete` | Destroy a resource | `rm` |
| `add` | Attach a child to a parent | — |
| `remove` | Detach a child from its parent without destroying it | — |
| `archive` | Soft-stash a resource (restorable) | — |
| `restore` | Undo a soft-delete or archive | — |
| `enable` / `disable` | Toggle a resource's active state without modifying its definition | — |
| `list` | Return a paginated collection | `ls` (where established) |
| `get` | Return one or more resources by identifier | — |
| `update` | Modify fields on an existing resource | — |
| `search` | List with a query argument (jtk's `users search`, slck's search surfaces) | — |
| `types` | List the enum values valid for a `--type` flag elsewhere | — |

**Implications:**

- `delete` destroys; `remove` detaches. They are NOT synonyms. `dashboards gadgets remove` does not delete the gadget definition — it removes it from a dashboard. `attachments delete` actually destroys the attachment.
- The presence of a `restore` sibling signals soft-delete (`projects delete` → `projects restore`). Hard-delete commands have no `restore` sibling and document irreversibility in their description.
- `<resource> types` always means "list the valid values for `--type` on `create`/`update` of this resource" (`projects types`, `links types`, `issues types`).
- `set`/`unset`/`reset` are NOT in the verb set for resource operations. Use `update` (with explicit flags) or `delete`. The one exception: `<tool> config set` for non-secret config writes is allowed (nrq uses this at `newrelic-cli/internal/cmd/configcmd/config.go:160`). `<tool> set-credential` (per `working-with-secrets.md` §1.5.2) is a compound command name, not an application of the `set` verb, and the prohibition does not apply.

Tool-specific spec example: jtk's full verb decisions live at `atlassian-cli/tools/jtk/internal/cmd/GUARDRAILS.md` §1.

---

## §2 Resource references — positional vs flag

### §2.1 Positional when the entity is

1. The **primary subject** of the command — what's being acted on (`<tool> issues get MON-123`).
2. A **constitutive parent** — the parent of a child entity that cannot exist without it (comments-of-issue, attachments-of-issue, contexts-of-field).
3. The **destination of an `add`-style** operation, where the command reads as "add to X" (`<tool> sprints add MON-123 <sprint>`).
4. The **second party in a binary relation** (`<tool> links create <a> <b>`, `<tool> transitions do <issue> <transition>`).

### §2.2 Flag when the entity is

1. An **optional filter** narrowing a list.
2. A **required scope** that is neither subject nor constitutive parent (`<tool> sprints current --board`, `<tool> issues types --project`).
3. A **setter or payload** during `create`/`update`.
4. A `--to-<thing>` **destination** for `move`-style operations.

The rule is **role-based**, not type-based. The same entity type appears positionally in some commands and as a flag in others depending on the role it plays in that specific command.

### §2.3 Positional arity

`cobra.ExactArgs(1)` for single-resource commands (`<tool> issues get MON-123`). `cobra.MinimumNArgs(1)` is permitted for variadic batch reads (`jtk issues get MON-123 MON-124 MON-125` at `atlassian-cli/tools/jtk/internal/cmd/issues/get.go:34` uses `MinimumNArgs(1)` deliberately; the `Use:` declaration at `:24` shows the variadic shape). `cobra.ExactArgs(2)` for binary relations (sfdc's `record get <object> <id>` at `salesforce-cli/internal/cmd/recordcmd/get.go:26`).

**Do NOT introduce a resource-identity flag** like `--issue-id`, `--page-id`, or `--key` for the primary subject. The positional is canonical across the family for view/get commands (verified across jtk, cfl, slck, gro, sfdc, nrq — 100% conformance). The `--id` output-shape flag (`output-and-rendering.md` §3) is unrelated; that flag controls rendering, not identification.

### §2.4 Name-or-ID resolution

Where the upstream service has both human-readable names and canonical IDs (Jira boards/sprints/projects, Slack channels, Confluence spaces), the positional MUST accept either form and resolve internally:

- Unique match → resolve silently.
- Ambiguous → fail, listing all matches with disambiguating identifiers.
- No match + looks like a raw ID (per the upstream's documented ID format — Jira's `PROJ-123`, Salesforce's 15/18-char IDs, Slack's `C/U/G`-prefixed channel/user IDs, etc.) → pass through unchanged (the upstream will 404 with a clearer error than ours). Each CLI documents its accepted ID shapes in its tool-specific spec.
- No match + looks like a name → fail with a hint to refresh the relevant cache (`<tool> refresh <resource>`, per `working-with-state.md` §4.6).

Reference: jtk's `resolve.New(client).Board(ctx, args[0])` at `atlassian-cli/tools/jtk/internal/cmd/boards/boards.go:176`; slck's `c.ResolveChannel(channel)` at `slack-chat-api/internal/cmd/channels/get.go:35`.

---

## §3 Mutation safety — risk-tiered prompts

### §3.1 Resource mutations

The rule:

> **If a user runs this by accident, can they trivially undo it from another short command of the same CLI without external recovery?**
>
> - **Yes** → no prompt, no `--force` flag.
> - **No** → prompt by default, accept `--force` to skip.

"Trivially reversible" means *one short command away*. If undoing requires the user to remember state they no longer have access to (the contents of a comment, the bytes of an attachment, the body of an automation rule), it is not trivially reversible.

Corollaries:

- The presence of a `restore` sibling does not automatically make a delete low-risk. `projects delete` is restorable but still warrants the prompt because the soft-delete window can lapse and the impact radius is large.
- The flag for resource-mutation prompts is spelled `--force`. Do NOT reach for `--yes`, `--confirm`, `-y`. Long-only.

### §3.2 Credential/keyring writes — separate flag, separate concern

Credential and keyring writes use **`--overwrite`** per `working-with-secrets.md` §1.5.1 — a single, narrowly-scoped meaning: "the keyring entries I'm about to write may already exist; replace them instead of failing." It does not suppress prompts elsewhere, does not lower verification strictness, does not affect file overwrite behavior outside the keyring.

**New CLIs MUST use `--overwrite` for credential replacement, not `--force`.** `--force` is NOT a canonical synonym; it survives only as a compatibility alias on CLIs where it predates the §1.5.1 rule. From `working-with-secrets.md` §1.5.1: "`--overwrite` (preferred name; `--force` is the legacy alias)" — `--overwrite` is the standard form for new code.

**The two flags MUST NOT be conflated:**
- A resource-mutation `--force` does NOT lower keyring-write strictness — `<tool> issues delete --force` does not let `<tool> init` overwrite a credential.
- A credential `--overwrite` does NOT suppress resource-mutation prompts — `<tool> set-credential --overwrite` is unrelated to whether `<tool> issues delete` prompts.

Cross-ref: `working-with-secrets.md` §1.5.1 (init keyring writes), §1.5.2 (`set-credential` keyring writes).

---

## §4 Prompt classes — two kinds, kept separate

The family has **exactly two** kinds of interactive prompt. They are distinct in library, location, and scriptable-skip mechanism. Conflating them produces unscriptable CLIs.

### §4.1 Setup wizards

- **Where:** `<tool> init` only.
- **Library:** Two implementations are conformant. `charmbracelet/huh` is the family's most common choice (jtk, cfl, gro, sfdc) and is appropriate for any new wizard with more than two or three prompts. Hand-rolled prompts using `bufio` + `golang.org/x/term` (slck at `slack-chat-api/internal/cmd/initcmd/init.go`, nrq at `newrelic-cli/internal/cmd/initcmd/init.go`) are appropriate when the wizard is one or two prompts and pulling in a TUI library would be overweight. The contract below is what matters; library choice does not affect conformance.
- **Purpose:** First-time configuration — collect non-secret connection values, optionally bridge a secret into the keyring, write the config file, smoke-test the connection.
- **Scriptable skip:** Every wizard input MUST have a corresponding non-interactive equivalent flag, AND the wizard MUST detect a non-interactive environment and bail. See `scriptability.md` §1.
- **Anti-pattern:** Running the wizard regardless of supplied flags — e.g., `jtk init` at `atlassian-cli/tools/jtk/internal/cmd/initcmd/initcmd.go:219` always invokes `form.Run()` even when every flag value is supplied. The wizard is not flag-skippable; that is the divergence.

### §4.2 Safety confirmations

- **Where:** Any risky mutation (resource deletions, automation-rule changes, attachment deletes — anywhere the §3.1 rule says "prompt").
- **Library:** Hand-rolled `y/N` read; never `huh`. Safety confirmations are one-shot binary prompts; huh forms are overkill and have their own non-TTY failure modes.
- **Scriptable skip:** `--force` flag (§3.1).
- **Non-TTY behavior:** When stdin is not a TTY and a safety confirmation would block, the command MUST fail with a hint to use `--force` rather than auto-confirm or hang.

The wizard-parity rule from `scriptability.md` §1 applies ONLY to §4.1 setup wizards. Safety confirmations are flag-skipped, not flag-mirrored.

---

## §5 Boolean flags

### §5.1 Defaults

A boolean flag's default should match the safest or most common case. `--wait` defaults to true because synchronous is more predictable; `--notify` defaults to true because notifications are usually wanted.

### §5.2 Documentation discipline

Any default-true boolean MUST:

- State the default explicitly in help text.
- Explicitly document the `--no-X` negation form alongside the positive form.
- Show both behaviors in examples.

**pflag does not auto-generate `--no-X`.** Register `--no-X` explicitly alongside the positive flag (e.g., `--no-wait` alongside `--wait`); in `RunE`, apply the override before use (`if noWait { wait = false }`). Help text and examples are the only surface where the negation becomes discoverable to users who haven't read the source.

---

## §6 Async operations

For operations that are async on the upstream side (`jtk issues move` is the family's illustrative example):

- The originating command takes `--wait` (default true) and runs synchronously by default.
- Passing `--no-wait` returns immediately with a task ID.
- A companion `<command>-status <task-id>` command polls the operation's status.

New async surface MUST follow this shape: same flag name, same default, same companion-command pattern. Do not invent `--async`, `--background`, `--detach`. Same-shape async is what lets a user wrap any async command in the same wait/poll script.

---

## §7 Flag conventions

### §7.1 Short aliases — for things you type often

Setter flags on `create`/`update` commands take short aliases. Conventional letter assignments (from jtk's GUARDRAILS §4.1 — the family precedent):

| Short | Long |
|---|---|
| `-n` | `--name` (setter) |
| `-d` | `--description` |
| `-t` | `--type` |
| `-k` | `--key` |
| `-l` | `--lead` |
| `-a` | `--assignee` |
| `-b` | `--body` is the canonical `-b` binding; `--board` may take `-b` in scoping context (e.g., `boards`, `sprints` commands) where `--body` is absent. If a single command ever needs both, `--body` keeps `-b` and `--board` becomes long-only. |
| `-V` | `--value` |
| `-c` | `--context` |
| `-f` | `--field` (repeatable `key=value`) |
| `-F` | `--file` (payload path) |
| `-o` | `--output` (write to path) |
| `-p` | `--project` |
| `-s` | `--sprint` |
| `-m` | `--max` (pagination — see `output-and-rendering.md` §6) |

The lowercase/uppercase distinction between `-f` (`--field`) and `-F` (`--file`) is meaningful and reserved. Reuse this map in any new CLI where the letter binding makes sense; pick a free letter if the binding doesn't apply to your domain.

### §7.2 Long-only — for things you toggle once or rarely

- Output-shape flags (`--id`, `--extended`, `--fulltext`, `--fields`) — see `output-and-rendering.md` §3.
- Pagination cursor (`--next-page-token`).
- Safety flags (`--force` on any risky mutation; `--overwrite` on credential writes only per `working-with-secrets.md` §1.5.1; `--non-interactive` on `init` only per `scriptability.md` §1.2 — NOT a global flag).
- Boolean toggles (`--no-wait`, `--no-browser`, `--no-color`, `--no-verify`, `--show-components`, `--custom-fields`).
- Enum filters (`--state`, `--auth-method`).
- One-off operation knobs (`--to-project`, `--to-type`, `--cloud-id`).
- List filters (`--name`, `--query`).

The principle: things you type many times a day get a short; things you toggle once per setup or once per debugging session don't. Short aliases are precious; spend them on the high-frequency surface.

### §7.3 Reserved filter flag names

- **`--name <substring>`** — case-insensitive substring match against the resource's name field. Use for any name-only filter. Always long-only.
- **`--query <string>`** — reserved for future multi-field full-text search. Do not use it for name-only filters.
- **`--search`** — banned. Verbs do not make good flag names; the verb is `search` (a subcommand), not a flag.

### §7.4 Principled exceptions

When a short alias would collide (e.g., `fields options ... --option` would want `-o`, but `-o` is `--output`), leave the flag long-only rather than invent a non-mnemonic letter. Mnemonic-or-nothing is the rule.

---

## §8 Naming hygiene

### §8.1 Reuse before invent

Before naming a new flag, grep this doc and the consuming CLI's command tree for an existing flag that does the same job. Reuse the name. New synonyms (`--search` for what is already `--name`; `--query` for what is already `--name`) are how the surface gets messy faster than any other single thing.

### §8.2 Command aliases

Top-level commands MAY have short, conventional aliases (jtk: `projects` → `project`, `proj`, `p`). New top-level commands should follow the pattern: full name, singular form, and a one- or two-letter abbreviation if available without conflict.

Subcommands use `ls` for `list` and `rm` for `delete` where established. Do not invent new subcommand aliases beyond these two — proliferating aliases (`del`, `lst`, `find`, etc.) hurt discoverability without helping typing.

---

## §9 Current divergences

The new docs are forward-looking. The following current divergences from this standard are called out here so a future audit knows what to fix, and so a new CLI does not cargo-cult the divergence. Filing migration tickets for these is a separate workstream — out of scope for this doc.

- **`jtk init --token <value>`** (`atlassian-cli/tools/jtk/internal/cmd/initcmd/initcmd.go:64`) — flag-passed plaintext secret, violates `working-with-secrets.md` §1.5.1. The sanctioned ingress is `jtk set-credential --stdin` or `--from-env`.
- **`jtk init` always runs the huh form** (`atlassian-cli/tools/jtk/internal/cmd/initcmd/initcmd.go:219`) — the wizard is not flag-skippable; violates §4.1's scriptable-skip rule.
- **`cfl init` has no `--token-stdin` / `--token-from-env`** (`atlassian-cli/tools/cfl/internal/cmd/init/init.go:161`) — only the huh form ingests the token; non-interactive ingress requires the separate `cfl set-credential` command, so the wizard itself is not flag-skippable for the token field. Violates §4.1's scriptable-skip rule.
- **`sfdc` has no `set-credential` command** — only secret ingress is the interactive OAuth flow inside `sfdc init`. Violates `working-with-secrets.md` §1.5.2 and §4.1's scriptable-skip rule.

Output-side divergences (color, JSON scope, stream discipline) are catalogued in `output-and-rendering.md` §10. Scriptability divergences (missing `--non-interactive`, `me` not exiting non-zero on auth failure) are catalogued in `scriptability.md` §9.
