# Working with Scriptability

The Open CLI Collective's CLIs are designed for two consumers in roughly equal measure: a human running them by hand, and a deployment script — PowerShell on Windows, bash on a Mac, Python on a fleet of machines — wiring them into installer/MDM/CI flows. **Every CLI must work from a script with the same fluency it works at a prompt.** This doc is the family-wide standard for what that means: how interactive prompts gate themselves, how secrets and non-secrets enter the system non-interactively, what exit codes and streams scripts can rely on, and what flag-name space is reserved for multi-tenant futures.

This is **normative for new CLIs.**

Companion pillars:
- `command-surface.md` — verbs, positional-vs-flag, two prompt classes (setup wizards vs safety confirmations).
- `output-and-rendering.md` — output shape, stdout/stderr discipline, color stance.
- `working-with-secrets.md` — secret ingress (stdin / `--from-env`), `set-credential`, `--overwrite`. **This doc cross-refs the secrets doc rather than restating its rules.**
- `working-with-state.md` — config + cache, the `refresh` command, migration-on-first-run.

**When this doc appears to conflict with `working-with-secrets.md` or `working-with-state.md`, those win.** See `docs/README.md` for the full conflict-resolution order across all five docs.

---

## §1 Init wizards and scripted setup

`<tool> init` is the canonical first-time-configuration command across the family. By convention it is the only place a setup wizard (huh-based or hand-rolled — see `command-surface.md` §4.1) lives. The wizard MUST have a complete scriptable bypass.

### §1.1 Wizard ↔ flag parity

Every wizard input MUST have a non-interactive equivalent. The shape of the equivalent depends on whether the input is a secret:

- **Non-secret values** (URLs, account IDs, regions, auth method, audience selectors): the flag carries the value. `--url <value>`, `--region <US|EU>`, `--auth-method <basic|bearer>`.
- **Secrets** (API tokens, OAuth tokens): the flag specifies **ingress**, never the value, per `working-with-secrets.md` §1.5.1. `--<key>-stdin` reads exactly one secret from stdin. `--<key>-from-env <ENV_VAR>` reads one secret from a named env var. **`--<key>=<literal>` is intentionally not supported** (process-listing, shell-history, transcript leakage).
- **Choices that gate side-effects** (whether to open the browser, whether to test the connection): each gets a flag. `--no-browser`, `--no-verify`, `--auth-code-stdin`, `--credentials-file <path>`.

A wizard that asks a question with no equivalent flag is non-conformant — there will be a script that needs that answer.

### §1.2 Non-interactive mode

Every `init` MUST support a non-interactive mode that prompts for nothing. The contract:

- Triggered by either `--non-interactive` (registered on `init` only — see scope note below) OR a non-TTY stdin.
- When triggered, no prompts run, ever — meaning no setup-wizard prompts. Safety confirmations on risky mutations are a separate concern; see `command-surface.md` §4.2 and §3.1.
- If a required input is missing, fail with a message that names the equivalent flag.
- Never silently skip a step that would have been prompted for.

**Scope.** `--non-interactive` is an `init`-only flag. Mutation safety on non-init commands is gated by `--force` (`command-surface.md` §3.1), and the equivalent of "no TTY → no prompt" for safety confirmations is "fail with a `--force` hint" per `command-surface.md` §4.2. Do NOT register `--non-interactive` on any non-`init` command.

Reference implementation (the gold standard): nrq's init combines `--non-interactive` (`newrelic-cli/internal/cmd/initcmd/init.go:77`) with `isTerminal(opts.Stdin)` (`:336-339`) into a `wantPrompt` gate (`:108`); missing input fails with a flag hint (`:156-159`). New CLIs should follow this shape.

### §1.3 Non-secret env-bridge flags

An optional ergonomic mirror of the secret-ingress pattern: `--<key>-from-env <ENV_VAR>` for non-secret values. Useful inside `op run --`–style invocations where the env scope is bounded.

```bash
op run --env-file=installer.env -- \
  <tool> init \
    --url-from-env TOOL_URL \
    --account-id-from-env TOOL_ACCOUNT \
    --token-stdin <<<"$(op read 'op://Vault/Item/token')"
```

Optional, not normative. nrq has this for `--account-id-from-env` (`newrelic-cli/internal/cmd/initcmd/init.go:74`). The cost is doubling the flag count; weigh against actual installer-script needs.

---

## §2 Secret ingress at config time

Refer to `working-with-secrets.md` §1.5 — that doc is the source of truth. Summary for navigation only:

- `<tool> init` ingests secrets via `--<key>-stdin` or `--<key>-from-env <ENV_VAR>`. `--<key>=<literal>` is banned.
- `<tool> set-credential` is the low-level, single-secret, scriptable ingress. `--stdin` or `--from-env`. `--value <literal>` is banned. `--overwrite` to replace.
- No clipboard for secrets. The existing standard is stdin + env; clipboard for secrets is intentionally out of scope.

New CLIs MUST implement both `init` (for first-time setup of multiple values at once) and `set-credential` (for credential rotation, `op run`–driven setup, MDM installers, and the multi-secret-stdin avoidance case). `sfdc` is missing `set-credential` today — that is the canonical divergence.

`set-credential` MUST also ship `--json` from the start per `output-and-rendering.md` §2 (the secrets standard target). Cross-ref: `working-with-secrets.md` §1.5.2 specifies the JSON envelope shape (`{"ref":..., "key":..., "backend":..., "written":true}`) and exit-code-per-failure-class contract.

---

## §3 Exit codes

### §3.1 Normative now

- **`<tool> me` MUST exit non-zero on auth failure or unreachable upstream.** This is the scripted health-check contract. nrq is the only CLI that currently enforces this (`newrelic-cli/internal/cmd/me/me.go:80-89`); slck `me` returns nil even with no tokens configured (`slack-chat-api/internal/cmd/me/me.go:101-105`) — divergence.
- **`<tool> set-credential` exits 0 on success and non-zero per failure class** — the specific failure classes are enumerated in `working-with-secrets.md` §1.5.2 (existing key + no `--overwrite`, disallowed key, keyring write error, locked keyring per §1.4). New CLIs SHOULD map these to the §3.2 taxonomy where applicable (existing-without-overwrite ≈ 1/generic, disallowed key ≈ 2/usage error, keyring write/locked ≈ 3/auth-config), but the precise code-per-class mapping is advisory until §3.2 becomes normative; the binary success/failure contract is what scripts can rely on today.

### §3.2 Recommended target (advisory)

A small fixed taxonomy for new CLIs to aim at, so installer scripts can dispatch on more than a binary success/fail:

| Exit | Class |
|---|---|
| 0 | success |
| 1 | generic failure |
| 2 | usage error (bad flag, missing arg) |
| 3 | auth / config error |
| 4 | not found |
| 5 | upstream / network error |

This taxonomy is **advisory until cli-common ships a typed error/classifier layer.** Today, most CLI roots collapse `Execute()` errors to exit 1; adopting the taxonomy family-wide requires a shared error type with `ExitCode() int` semantics. New CLIs SHOULD design with this layout in mind even if they collapse to 0/1 in their first release — the classifier can be backfilled later without breaking scripts that only branch on 0/non-0.

When the typed-error layer lands, this section becomes normative.

---

## §4 `me` as scripted health check

`<tool> me` is the canonical self-test command across the family. It exists in jtk, cfl, slck, gro, and nrq; sfdc has none (the closest equivalent is `sfdc config test`).

The contract (combining §3.1 with the output of `me`):

- **Exits 0 iff** the CLI is fully configured AND the upstream is reachable AND the configured credential authorizes against the upstream identity endpoint.
- **Exits non-zero** on any of: no credential, credential invalid, upstream unreachable, account/scope mismatch.
- **Output** is the configured identity at the upstream (account ID, display name, email — whatever the upstream returns). On failure, a short error to stderr naming the failure class.
- **Never echoes the secret.** Not in success output, not in error output.

`config test` is a sibling command focused specifically on the connection (rather than the identity); both are conformant. A new CLI MUST ship `me` (so the §3.1 contract has a named target) and MAY additionally ship `config test`. `me` is preferred as the scripted health-check entry point because it answers "who am I?" at the same time as "am I working?"

---

## §5 Browser-open pattern

CLIs that do OAuth (or any flow that wants to hand the user a URL) follow a shared pattern. Reference implementation: `gro init` at `google-readonly/internal/cmd/initcmd/init.go:338-353`.

The shape:

- **Always print the URL to stderr.** Regardless of any flags. The URL is the fallback path; printing it is never wrong.
- **Optionally open the user's default browser**, gated on a confirmation. The confirmation can be interactive (huh form) or implicit (the URL is just printed and the user clicks it).
- **`--no-browser`** suppresses the open-the-browser attempt. The URL is still printed.
- **`--auth-code-stdin`** reads the redirect URL or auth code from stdin instead of running a callback listener. Implies `--no-browser`. Used for two-phase MDM-style installs where the human is at a different machine from the install runner.

```go
authURL := auth.GetAuthURL(oauthCfg)
if !opts.authCodeStdin && !opts.noBrowser {
    open, err := d.Prompter.ConfirmOpenBrowser()
    if err != nil {
        return fmt.Errorf("confirm browser open: %w", err)
    }
    if open {
        if err := d.OpenBrowser(authURL); err != nil {
            fmt.Fprintf(stderr, "Could not open browser automatically (%v).\n", err)
        }
    }
}
// The URL is a side-channel hint to the human, NOT primary command data —
// route to stderr so a `RESULT=$(... init ...)` capture stays clean.
fmt.Fprintln(stderr, "If your browser didn't open, paste this URL into it:")
fmt.Fprintln(stderr)
fmt.Fprintln(stderr, "  "+authURL)
```

This is the only sanctioned mechanism for a CLI to invoke the user's GUI. CLIs should NOT use the same mechanism to open arbitrary resource URLs by default. cfl's `page view --web` (`atlassian-cli/tools/cfl/internal/cmd/page/view.go:104-111`) is an exception — opt-in via flag, never the default rendering path.

---

## §6 `--profile` reservation

Some current CLIs (atlassian-cli's multi-tenant story, the planned code-review CLI's "Claude-at-work-on-bitbucket / Codex-at-home-on-github" use case) need to talk to multiple upstream instances per user. The forward-compatible flag name for this is **`--profile`**.

This doc reserves `--profile` across the family:

- New CLIs MUST NOT use `--profile` for any other purpose. The name is held back even when no CLI implements multi-tenant yet.
- When a CLI does implement profiles, the value space is opaque per-CLI; the standard does not prescribe a profile naming convention.
- The relationship between `--profile` and `credential_ref` (`working-with-secrets.md` §1.3) is "profiles select; `credential_ref` resolves." A future profile-aware CLI looks up the active profile's `credential_ref` and the secret resolves from there.

No CLI ships `--profile` today. Reserving the name now prevents painting ourselves into a corner.

---

## §7 The `refresh` command

`<tool> refresh [resources...]` is the canonical cache-invalidation command, fully specified in `working-with-state.md` §4.6. Summary for navigation only:

- `<tool> refresh` (no args) refreshes all caches.
- `<tool> refresh <resource>` refreshes one named cache (e.g., `jtk refresh users`).
- `<tool> refresh --status` reports freshness + age with no network calls.

**Current state:** jtk ships the top-level `refresh` subcommand (`atlassian-cli/tools/jtk/internal/cmd/refresh/refresh.go`); gro ships a cli-common-backed cache (`google-readonly/internal/cache/cache.go`) but exposes invalidation as a per-command `--refresh` flag (e.g., `gro drive drives --refresh` at `google-readonly/internal/cmd/drive/drives.go:106`) rather than a top-level subcommand — divergence from `working-with-state.md` §4.6's signature. slck, sfdc, nrq, cfl have no caches. Codifying the signature here so future CLIs grow caches with the standardized shape.

---

## §8 Stream discipline

stdout = data, stderr = side channel. Full rules at `output-and-rendering.md` §5. Repeated here because scriptability depends on it:

- A script doing `RESULT=$(<tool> ...)` captures stdout. The CLI MUST NOT leak progress, prompts, warnings, or migration notices into that capture.
- A script doing `<tool> ... 2>/dev/null` SHOULD still get the data it wanted on stdout.
- JSON output paths put `_migration` envelopes at the top level of the stdout JSON response, not on stderr (per `working-with-secrets.md` §1.8). Human paths put the migration notice on stderr only.

---

## §9 Current divergences

The new docs are forward-looking. Scriptability divergences:

- **No CLI implements the `--non-interactive` + TTY-detection contract except nrq** (`newrelic-cli/internal/cmd/initcmd/init.go:77,108,156-159,336-339`). jtk, cfl, gro, sfdc all run their setup wizards unconditionally; slck checks stdin TTY implicitly but has no explicit `--non-interactive` flag.
- **`jtk init` accepts `--token <value>` for the API token** (`atlassian-cli/tools/jtk/internal/cmd/initcmd/initcmd.go:64`) — flag-passed plaintext secret, violates §2 and `working-with-secrets.md` §1.5.1.
- **`cfl init` has no secret-ingress flags at all** (`atlassian-cli/tools/cfl/internal/cmd/init/init.go:161`) — non-interactive ingress requires the separate `cfl set-credential` command; the wizard itself is not flag-skippable.
- **`sfdc` has no `set-credential` command** — secret ingress is only available via the interactive OAuth flow inside `sfdc init`. Installer scripts have no scriptable bypass at all.
- **`set-credential` flag surface diverges across the family.** `working-with-secrets.md` §1.5.2 specifies `--ref`, `--key`, `--stdin`/`--from-env`, `--overwrite`, and `--json`. nrq ships all of these except `--json` (`newrelic-cli/internal/cmd/configcmd/config.go:76-80`). jtk/cfl ship ONLY `--from-env` (`atlassian-cli/tools/jtk/internal/cmd/setcredential/setcredential.go:42`, `atlassian-cli/tools/cfl/internal/cmd/setcredential/setcredential.go:42`) — stdin is implicit when `--from-env` is absent, `--ref`/`--key` are not registered (single hardcoded `api_token` in the shared atlassian-cli bundle), no `--overwrite`, no `--json`. New CLIs MUST ship the full §1.5.2 surface.
- **`gro` ships a cli-common cache but no top-level `refresh` subcommand.** Cache invalidation is exposed via per-command `--refresh` flags (e.g., `gro drive drives --refresh` at `google-readonly/internal/cmd/drive/drives.go:106`); divergence from `working-with-state.md` §4.6.
- **`slck me` returns nil with no tokens** (`slack-chat-api/internal/cmd/me/me.go:101-105`) — fails the §3.1 health-check contract; a script doing `slck me || install_credentials` will skip the install.
- **No CLI implements the §3.2 exit-code taxonomy.** Most roots collapse `Execute()` errors to exit 1. Backfilling requires the typed-error layer described in §3.2.
- **No CLI implements `--profile`.** Reserved per §6; no implementation expected until codereview or a multi-tenant atlassian-cli refresh forces the issue.

Command-surface divergences live in `command-surface.md` §9. Output-side divergences live in `output-and-rendering.md` §10.
