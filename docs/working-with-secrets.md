# Working with Secrets

Two parts:

1. **The standard** — how CLIs in the Open CLI Collective handle secrets and credentials. Source of truth for new CLIs, and the target state for existing ones.
2. **The migration manifest** — concrete per-CLI work to bring existing CLIs (`jtk`, `cfl`, `gro`, `nrq`, `slck`) into compliance.

**Part 1 is normative. Part 2 is derived from it.** Every item in Part 2 exists because Part 1 requires it. When Part 2 appears to conflict with Part 1 — because a repo's reality turned out to be messier than the manifest anticipated, or because a section here was written with incomplete information — **Part 1 wins.** Update Part 2 (or this document) to match, don't reinterpret Part 1 to fit a Part 2 item.

The "Deriving work from this standard" bridge between the two parts spells out how to translate the standard into work for any CLI, new or existing. Agents picking up Part 2 work should read it before touching code, so the per-CLI sections are read as *instances* of a general pattern rather than as a disconnected checklist.

---

# Part 1 — The Open CLI Collective Secret-Handling Standard

## Goals

- **Runtime secrets live in the OS credential store.** Once a CLI is operating — reading config and making API calls — the only place it looks for a secret is the platform keyring. Never plaintext on disk, never a config-file field, never an env var the CLI reads as a primary source.
- **Deployment-time secrets can live anywhere.** Getting a secret *into* the keyring is an install-time concern. The source can be a 1Password vault read by `op`, an MDM-pushed file, an installer prompt, an interactive `init`, a Vault lookup, an env var the installer reads — anything. The CLI does not care; once `init` (or an equivalent setup step) finishes, the secret is in the keyring and the upstream source is irrelevant.
- **Config files are commitable to private / org-controlled stores.** Everything in a CLI's config file is safe for an org to version in a private repo, ship via MDM, or template across machines. This is *not* a guarantee that the file is safe to publish on the public internet — deployment material (§1.2) is org-internal even though it isn't an access secret.
- **Multi-tenant ready.** A user can hold credentials for more than one tenant of the same service (two Jira orgs, two Slack workspaces) without collision.
- **Org deployment friendly.** An organization can pre-stage keychain entries via MDM / 1Password / its installer of choice, ship a config file pointing at them, and have the CLI Just Work without re-prompting the user for secrets.
- **Consistent across platforms.** macOS, Windows, and Linux behave the same way from the user's perspective, with explicit, documented fallback when the OS keyring is unavailable.

## Non-goals

- Native runtime integration with 1Password / Vault / SSM. Those are *installer-time* secret sources — the installer reads from them and writes to the OS keyring once. The CLI itself only knows about the OS keyring.
- Encrypting the config file. Config files don't contain secrets.

## Threat model

What this standard defends against:

- **Accidental disclosure via version control.** Committing a dotfiles repo with a config dir in it; pushing a screenshot of a config file; a logging system that captures the home directory.
- **Plaintext-on-disk casual inspection.** Coworker glancing at the screen; a backup tool that surfaces files; a corporate-laptop incident response team scanning home directories.
- **Process-listing and shell-history leakage.** A token passed via `--token=<value>` flag ending up in `ps`, in `history`, in a transcript pasted into a Slack channel, in a `script(1)` recording. This is why env-var ingress is preferred over flag ingress for secrets.
- **Inconsistent OS behavior.** A user on macOS getting strong protection while the same workflow on Windows or Linux silently downgrades to a plaintext file.
- **Operator misclassification.** Putting a shared org token in a "deployment material" file because both are "org-wide."

What this standard does **not** defend against:

- **A malicious process running as the same user.** Anything that user can run can read the keyring. The OS keyring isn't a sandbox; it's a place that isn't disk.
- **Root / Administrator compromise.** Once root or SYSTEM is in play, all bets are off.
- **A compromised OS keyring backend.** If Keychain / Credential Manager / Secret Service itself is owned, this standard provides no additional defense.
- **A malicious binary masquerading as a Collective CLI.** TouchID / Hello prompts may not adequately distinguish; out of scope.
- **Network-level attacks** on the API endpoints the CLIs talk to. Orthogonal concern.

The standard is a *better default*, not a security boundary. It removes the most common accidental-disclosure paths and makes the remaining paths visible.

## §1.1 Library

All Collective CLIs use **`github.com/byteness/keyring`** as the credential-store abstraction. (Migrated from `github.com/99designs/keyring` in #23 — ByteNess is an active fork that picks up CVE fixes and ongoing maintenance.)

- macOS → Keychain (Security framework, no shelling out).
- Windows → Credential Manager (`wincred`).
- Linux → Secret Service (D-Bus), then file fallback (see §1.4).
- A shared internal package, `cli-common/credstore`, wraps the library so every CLI uses the same backend priority, error messages, and config layout. CLIs do not depend on `byteness/keyring` directly.
- For surfacing a `--backend` flag and `keyring.backend` config key in a CLI, use `credstore.BackendFlagName` / `credstore.BackendFlagUsage()` and `credstore.BindBackendFlag` — see the package doc on `credstore/flag.go`.

## §1.2 What goes where

**Terminology.** This standard uses **access secret** for any value that, possessed alone, grants API access. The name was chosen over "user secret" because the latter invites readers to assume "per-user" — which then trips them into misclassifying shared org-wide tokens. Class membership is determined by *capability*, not by who holds it: a Slack bot token shared by 200 engineers is an access secret because anyone holding it can post as the bot. A desktop OAuth client JSON shared by the same 200 engineers is *not* an access secret because the user's consent step is still required for it to do anything.

**Guideline.** The config file holds values that are *stable* (change rarely, on the order of an org-wide reconfiguration) and *non-sensitive* (safe to read aloud in a meeting or commit to a private dotfiles repo). The keyring holds access secrets. If a value is short-lived but non-sensitive — like a cache — it belongs in a cache directory, not in either of those places.

**Config file (yaml; mode 0600 on POSIX where the file may contain hostnames or identifiers an operator considers sensitive; otherwise platform-appropriate config-file permissions — this is non-secret data, not a credential):**
- Instance URLs, hostnames, regions
- User identifiers (email, account-id, workspace)
- Default project / space / channel / board
- Auth method, cloud-id, output format
- Cache TTLs and other tuning knobs (e.g. `gro`'s Drive metadata TTL; `jtk`'s multi-layer cache windows for issue metadata vs sprint state — slowly-changing vs fast-changing data classes)
- `credential_ref` (see §1.3)
- *Deployment material* — see below

**Deployment material.** (Previously called "deployment credentials" — renamed because "credential" misleads agents into classifying shared-but-access-granting tokens into this bucket. Deployment material does *not* grant access on its own.) A separate class from access secrets. These are values that meet **all four** of:

1. Identical for every user in the org (not user-specific).
2. Distributed to every user by design (lives on every install).
3. **Grants nothing on its own.** Possessing the value without a further user-specific step yields no API access.
4. Stable over time.

The canonical example is a desktop-app OAuth client JSON (Google Cloud, Microsoft Identity, etc.). Google's own documentation explicitly notes that the "client secret" in this context is not treated as a secret, because it's embedded in distributed binaries by design. The thing that actually grants access — the user's OAuth token — is per-user and stays in the keyring.

**Negative examples — these are NOT deployment material, they are access secrets, even if org-wide.** The "grants nothing on its own" criterion is what most often gets misapplied. If a single value, possessed alone, lets the bearer make authenticated API calls, it is an access secret. Class membership is determined by capability, not by who holds it.

- A shared Slack bot token (`xoxb-…`) used by every employee → access secret. Possession alone lets you post as the bot.
- A shared New Relic API key for a team account → access secret. Possession alone lets you query NRQL.
- A service-account private key (Google, AWS IAM, etc.) used by an org-wide automation → access secret. Possession alone authenticates.
- A SaaS API token rotated quarterly and distributed to all engineers → access secret.

These all live in the keyring under a `credential_ref`, *even when* the installer pre-stages them via `op`. "Shared across the org" is not a license to put a token in a plain file.

Deployment material lives in the config directory as plain files (or inline in the config yaml when small). Recommended layout for `gro`: `~/.config/google-readonly/oauth_client.json` with platform-appropriate file permissions for non-secret org-internal data (on POSIX, 0644 is fine; tighter is also fine; the file is not a credential), and an `oauth_client_path` field in `config.yml` (default: that path). Installers ship this file as part of the deployment; no `op` round-trip, no keyring write, no 1Password secret-notes encoding pitfalls.

Deployment material is not safe to *publish* (don't put it in a public repo), but it is safe to *distribute internally* (commit to a private dotfiles repo, push via MDM, ship alongside `installer-config.json`).

**Cache directory (`$XDG_CACHE_HOME/<service>` or platform equivalent):**
- Anything the CLI computed from API responses and can recompute. Never authoritative; safe to delete at any time. Out of scope for this standard; called out so it doesn't drift into the config file.

**OS credential store (access secrets):**
- Per-user API tokens
- Shared org-wide tokens that grant access on their own (per the negative examples above)
- OAuth refresh/access tokens (the per-user ones — not the org-wide client JSON, which is deployment material)
- Anything that grants access on its own

A config file that contains an *access secret* is non-compliant. A config file (or sibling file) that contains *deployment material* is fine.

## §1.3 The `credential_ref` field

Every Collective CLI's config file carries a `credential_ref` field that names the credential bundle in the OS keyring. The CLI uses this ref — never an implicit hardcoded service name — to fetch its secrets.

Format: `<service>/<profile>`. For example:
- `atlassian-cli/default`
- `slack-chat-api/work-account`
- `newrelic-cli/staging`

The `service` segment is the keychain service name. The `profile` segment is an opaque identifier scoped to that service; the CLI does not parse it.

Within a bundle, individual secrets are addressed by well-known keys defined per CLI:
- `atlassian-cli/default` → keys: `api_token`
- `slack-chat-api/work-account` → keys: `bot_token`, `user_token`
- `newrelic-cli/staging` → keys: `api_key`
- `google-readonly/default` → keys: `oauth_token` (the OAuth client JSON is deployment material per §1.2 and lives on disk, not here)

**One key per logical credential.** A bundle may contain multiple keys only when those keys represent distinct logical credentials defined by the CLI's Part 2 section. A CLI MUST NOT define multiple keys for the same logical credential with resolver precedence between them unless that CLI's Part 2 section explicitly grants the exception, names the full key set, defines the precedence order, and requires tests proving write/resolve consistency for every write target that could otherwise be shadowed.

**Concrete mapping to `byteness/keyring`.** The library exposes `Config.ServiceName` (a single string) and `Item.Key` (a single string per stored item) with no native sub-namespace concept. The shared `cli-common/credstore` wrapper maps the standard's three-segment addressing (`<service>/<profile>/<key>`) onto the library's two as follows:

- `ServiceName` ← the `service` segment of the ref.
- `Item.Key` ← `<profile>/<key>`, joined with a literal `/`.

So `slack-chat-api/work-account` with secret `bot_token` becomes `ServiceName="slack-chat-api"`, `Item.Key="work-account/bot_token"`. This makes `Keyring.Keys()` enumerable per-service for `config show` and `config clear`, and gives a deterministic, inspectable layout when a user opens Keychain Access / Credential Manager / `secret-tool`.

Because `/` is structural in this mapping, **`/` is forbidden inside any segment.** Allowed characters within `service`, `profile`, and `key` are `[A-Za-z0-9_-]`. The shared package rejects anything else at write time with a clear error. CLIs that need a richer identifier (e.g. an email address as a profile) must escape it; the shared package provides helpers.

**Multi-key-per-ref is supported by all three platform backends.** A `Keyring` opened against one `ServiceName` holds any number of `Item`s with distinct `Item.Key` values. On macOS this is one Keychain service with multiple account names; on Windows, one target-name prefix with multiple targets; on Linux Secret Service, one collection with multiple items distinguished by attributes. So `slack-chat-api/work-account` legitimately holds both `work-account/bot_token` and `work-account/user_token` as separate, independently-readable entries.

Multiple profiles let a user keep credentials for multiple tenants in the same OS keyring. A user with two Jira orgs has two config files (e.g. via `--config` or profile selection — out of scope here), each with a distinct `credential_ref`, each pointing at its own keychain bundle.

**Why explicit, not by convention.** An org's deployment tooling can:
- Pre-create the keyring entries (via MDM, `op`, a setup script) with any naming scheme it likes.
- Stamp the ref into a config file shipped to the user.
- Let the user run the CLI without any `init` prompt for the secret.

A CLI that derives its service/account names from a hardcoded convention can't do this cleanly.

## §1.4 Backend selection and ordering

The shared credstore package selects backends in this fixed order:

1. **macOS** — Keychain. No fallback (Keychain is always available on macOS).
2. **Windows** — Credential Manager. No fallback.
3. **Linux** — Secret Service over D-Bus. Fallback rules are non-trivial; see below.

**Linux fallback — fail closed on a working-but-denied keyring.** Distinguish two cases:

- **Secret Service is unavailable.** No D-Bus session bus (headless server, WSL without configured keyring, container without daemon). The CLI may fall back to the encrypted-file backend at `~/.local/share/<service>/keyring` (byteness/keyring's `file` backend). The user's environment doesn't support OS-keyring storage at all; the file is the best we can do.
- **Secret Service is present but locked, returns an auth failure, or otherwise rejects the request.** **Fail closed** with an actionable error that names the backend, the operation that failed, and how to unlock or grant access (`gnome-keyring-daemon`, `seahorse`, `kwalletmanager`, etc.). Do *not* silently fall back to the file backend. A user with a working desktop keyring that happens to be locked must not have their secrets silently downgraded to an encrypted file in a different location — that is a stealth security regression and a likely source of "where did my credentials go?" support tickets.
- **Ambiguous failure → fail closed.** If the wrapper cannot confidently distinguish "unavailable" from "denied/locked" — D-Bus answered but returned an opaque error, the backend timed out, the response shape was unexpected — treat the case as denied/locked and fail closed. The fallback path is opt-in only when we are sure no working keyring is present.
- **Explicit user opt-in to the file backend** is supported via a config flag (working name `keyring.backend: file` in `config.yml`) or an env var (`<SERVICE>_KEYRING_BACKEND=file`). With that set, the CLI uses the file backend unconditionally and never attempts Secret Service. This is the supported path for users who genuinely prefer the file backend. The backend-selector env var is non-secret runtime configuration (it controls *where* the CLI looks, not *what* it finds); it is not the runtime-env-var exception described below.

The file backend is encrypted with a passphrase. For org-friendly headless use (CI, WSL with no daemon), the passphrase can be supplied via a **per-service** env var named `<SERVICE>_KEYRING_PASSPHRASE`, where `SERVICE` is the upper-snake-cased service segment of the `credential_ref`:

- `ATLASSIAN_CLI_KEYRING_PASSPHRASE`
- `SLACK_CHAT_API_KEYRING_PASSPHRASE`
- `GOOGLE_READONLY_KEYRING_PASSPHRASE`
- `NEWRELIC_CLI_KEYRING_PASSPHRASE`

Per-service (not a single global passphrase) so that a leak of one env var compromises only one CLI's secrets, and so that org tooling injecting credentials for one CLI doesn't need access to every CLI's keyring. Document the trade-off in `config show`: when the file backend is in use, the entry indicates whether it's passphrase-prompted or env-var-supplied, so the user understands the security posture of their setup.

**Env-var exception, named explicitly.** This standard otherwise bans env vars as a runtime source of *secret material or unlocking material* (a CLI must not read `FOO_TOKEN` and use it as the API credential at call time — §1.11 acceptance item 2). `<SERVICE>_KEYRING_PASSPHRASE` is the **one allowed runtime-env-var exception for secret material / unlocking material**, and even it is not a service token: it unlocks the encrypted-file backend so the CLI can then fetch the actual service token from it. The distinction matters because the standard's threat model treats env vars as a leaky channel (process lists, parent-process inheritance, transcripts); the passphrase being there is a deliberate trade-off scoped to the headless-Linux use case, not a general escape hatch. Non-secret runtime env vars (the backend selector above; future runtime knobs that don't carry secret material) are not exceptions because they don't fall under the ban in the first place.

## §1.5 Credential ingress

Two commands accept *new* secret material from the user or environment: `init` (interactive or scripted first-time setup) and `set-credential` (low-level, single-secret, scripted). No other command accepts secret material from the user or environment. There are no other ingress paths.

This is distinct from *runtime token refresh*. A CLI making normal API calls may legitimately receive a refreshed OAuth access token (or rotated bearer) from the upstream service and persist it back to the keyring under the active `credential_ref`. That's a write, but the new material came from the upstream service the CLI is already authenticated to, not from the user or environment. Token refresh is the only sanctioned non-ingress write to the keyring. It must update an *existing* entry under the active ref; it must not create new keys not declared in the CLI's allowed-key set; it must not silently change the `credential_ref` itself.

### §1.5.1 `init`

`<cli> init` (in addition to manifest §1 contract):
- Writes non-secret values to the config file.
- Writes secrets to the keyring under the `credential_ref` derived from flags / config / a default.
- If `--credential-ref` is not supplied, defaults to `<service>/default`.
- **Pre-write check (per-bundle, not per-key).** Before writing, the CLI lists existing keys under the target ref (`Keyring.Keys()` for the service, filtered by the `<profile>/` prefix). If *any* expected key for this CLI is already present, `init` fails by default with an actionable message naming the existing ref, the existing keys, and the OS keyring tool the user can inspect them with (`Keychain Access` on macOS, `cmdkey /list` on Windows, `secret-tool search` on Linux). Remediation in the error: re-run with `--overwrite`, pick a different ref with `--credential-ref`, or run `<cli> config clear` first.
- **Atomicity for multi-key bundles.** `init` for a CLI that writes more than one key under the same ref (e.g. `slck` writes `bot_token` and `user_token`) writes all keys or none. The underlying library doesn't expose a transaction primitive, so the wrapper achieves practical atomicity by: (1) validating all inputs and checking pre-write state before any write begins; (2) **when `--overwrite` is in play, reading existing values for every expected key into an in-memory snapshot before any write**, so rollback can restore prior values rather than merely deleting newly-written keys; (3) on a write failure mid-bundle, restoring from snapshot (for `--overwrite` cases) or removing newly-written keys (for first-write cases); (4) reporting a clear error naming what was written, restored, or deleted. The snapshot lives only for the duration of the call and is zeroed before the function returns. A failure to roll back is itself surfaced; the user is never left wondering what is now in the keyring.

- **Secret-ingress flags for scripted `init`.** When `init` is invoked non-interactively and needs to ingest secrets, it uses one of two patterns, never `--<key>=<literal>`:
  - **Per-key env vars:** `--<key>-from-env <ENV_VAR>` for each expected secret. Scales cleanly to multi-secret CLIs (e.g. `slck init --bot-token-from-env BOT_TOKEN --user-token-from-env USER_TOKEN`). Subject to the env-var caveats in the threat model (process inheritance, transcripts) but acceptable inside an `op run --` invocation where the env scope is bounded.
  - **Single-secret stdin:** `--<key>-stdin` reads exactly one secret value from stdin (e.g. `<cli> init --token-stdin`). Available only when the CLI has exactly one expected secret, or when only one secret is being supplied this way (others must come from env). Stdin has one stream; the standard does not endorse delimited multi-secret stdin payloads.
  - **For multi-secret automation, prefer `set-credential` per secret over `init`.** It's purpose-built for the one-secret-per-invocation case and avoids the stdin-multiplexing problem entirely (§1.10).

  `init --token=<literal>` and `init --bot-token=<literal>` are **intentionally not supported** for secret-bearing flags, for the same process-listing / shell-history / transcript reasons that apply to `set-credential`. Non-secret values (URLs, account-ids, regions) remain flag-supplied; only secrets are constrained to stdin/env.

**On `--overwrite` (preferred name; `--force` is the legacy alias).** Single, narrowly-scoped meaning: "the keyring entries I'm about to write may already exist; replace them instead of failing." It does not suppress confirmation prompts elsewhere, does not lower verification strictness, does not affect file-overwrite behavior outside the keyring write. Naming it `--overwrite` rather than `--force` makes the scope obvious and avoids the historical baggage of `--force` meaning "ignore all safety rails." If we keep `--force` as an alias for ergonomic familiarity, document it as exactly equivalent to `--overwrite` and nothing more.

### §1.5.2 `set-credential`

`<cli> set-credential` is the low-level, single-secret, scriptable ingress path. Distinct from `init`: it writes one key, takes no config-file values, performs no verification or API smoke test, and is intended for automation (installer scripts, credential rotations, `op run`-driven setup).

**Flags:**
- `--ref <credential_ref>` — required. Defaults to the active config's `credential_ref` only if a config file already exists.
- `--key <key>` — required. Must be one of the allowed keys for this CLI (e.g. for `slck`: `bot_token` or `user_token`). The CLI rejects any other key with a clear error listing the allowed set. No free-form keys; this prevents typos like `--key bot-token` from silently creating an unused entry.
- `--stdin` (preferred) or `--from-env <ENV_VAR>` — exactly one of these supplies the secret value. **`--value <literal>` is intentionally not supported**, because flag-passed secrets appear in process listings, shell history, and transcripts (§ Threat model). If a user genuinely wants to pass a value inline, they can `echo "$value" | <cli> set-credential ... --stdin`.
- `--overwrite` — same semantics as in `init`. Without it, an existing entry at `<ref>/<key>` causes failure.

**Behavior:**
- Never echoes the value to stdout or stderr, never logs it, never includes it in the `set-credential` invocation in shell history (the value comes from stdin or env).
- On success, prints one line to stderr: `wrote <key> to <ref> via <backend>`. Exits 0.
- On failure (existing key + no `--overwrite`, disallowed key, keyring write error, locked keyring per §1.4), exits nonzero with a distinct code per failure class.
- With `--json`: emits `{"ref": "...", "key": "...", "backend": "...", "written": true}` (or `"written": false` with an `"error"` field on failure). Never emits the value.

## §1.6 What `config show` reports

`config show` (and `config show --json`) reports:
- The contents of the config file (which by definition contain no access secrets — §1.2).
- For each access secret expected by the CLI: whether it is present, never the value.
- Which backend the keyring is using on this machine (`keychain`, `wincred`, `secret-service`, or `file`).
- The full `credential_ref` so users can locate the entry in Keychain Access / Credential Manager / `secret-tool`.
- **Deployment material is reported by path, presence, and content fingerprint — not inlined.** OAuth client JSONs and similar deployment-material files can be sizable and are org-internal; dumping them into `config show` clutters output and risks copy-paste leakage into chats/tickets. The reported fingerprint is a stable hash prefix (e.g. SHA-256 truncated to 12 hex chars) so operators can verify the file matches what the installer shipped without reading the contents. `config show --verbose` may inline small deployment-material files; the default does not.

## §1.7 `config clear`

Two scopes — narrow (default) and total (`--all`). Both are scoped to the **active** `credential_ref` so a user with multiple profiles never accidentally wipes another profile.

### §1.7.1 `config clear` (default scope)

- Removes the keyring entries under the active `credential_ref` (every key in the bundle: `api_token`, `bot_token`, `user_token`, `oauth_token`, etc.).
- Leaves the config file in place. The user can re-run `<cli> init` and re-authenticate without retyping the URL / email / workspace / region.
- Leaves caches in place.
- This is the everyday "I rotated my token, let me re-auth" command. Reversible by re-running `init`.

### §1.7.2 `config clear --all` (factory reset of the active profile)

- Everything in §1.7.1, **plus**:
- The active config file (`~/.config/<service>/config.yml` and any per-tool legacy files the CLI still recognizes for *this* profile).
- Cache directories the CLI owns.
- Empty parent directories left behind after removal.
- **Scope is the active profile, not the whole CLI.** For a single-profile user this is indistinguishable from "the machine never having seen the CLI" — which is the common case. For a multi-profile user, other profiles' configs and keyring entries remain untouched. This is the deliberate, safe default: a user with two Jira tenants who runs `config clear --all` on the active one does not lose access to the other.
- **Whole-service purging is intentionally not provided** by this command. If we ever need it (an org-wide uninstall, a security incident response tool), it gets its own explicit command with its own flag, not silent expansion of `--all`'s scope.
- Implies the no-prompt behavior described in the deployment manifest §1.5 (scriptable, no confirmation, idempotent).

### §1.7.3 Scope rules common to both

- Never touches keyring entries for inactive `credential_ref`s (multi-profile safety).
- Never touches files outside the CLI's own config / cache directories.
- Reports what was removed (file paths, keyring ref + keys, cache paths). With `--dry-run`, reports without removing.

## §1.8 Migration from legacy formats

When a CLI reads a config file containing a legacy plaintext secret field, **and** no value for that secret already exists in the keyring under the configured ref:
- Move the secret into the keyring under the configured (or default) `credential_ref`.
- Rewrite the config file without the secret field, adding `credential_ref` if missing.
- Print one line to stderr: `migrated <field> to keyring at <ref>; this is a one-time operation`.
- The user may be prompted by the OS (Touch ID / Windows Hello / Secret Service unlock) — this is the OS, not the CLI.

**Conflict resolution — legacy plaintext value differs from existing keyring value.** This happens when a user has run a newer version of the CLI (writing to the keyring) and then runs an older one (writing back to the legacy plaintext), or has manually edited the legacy file. The CLI **does not silently pick a winner.** It fails with a clear error that names both locations and states that the values differ — and **never prints either value, masked or unmasked.** Masked prefixes/suffixes are still secret material (§1.12); the standard does not endorse "first four characters of the token" displays. The error message offers three options:

- `<cli> config clear` then re-run → keeps the legacy plaintext value, removes the keyring entry, lets the migration proceed.
- Manually delete the plaintext field from the legacy file → keeps the keyring value, removes the conflict.
- Re-run with `--overwrite` → forces the legacy plaintext into the keyring, replacing the existing entry.

This is the same posture as §1.5's overwrite rule: when two sources of truth exist, the user is the only entity that can authoritatively pick.

**Multiple legacy sources for one credential.** When more than one legacy source can contain the same logical access secret, migration collects every non-empty candidate value. If the candidates contain more than one distinct value, migration fails as a conflict that names every source location and prints no secret material. The CLI does not use source precedence to choose among divergent access secrets. If all candidates are equal, the CLI migrates one value to the keyring and scrubs every plaintext copy. (Source precedence may still resolve divergent *non-secret* config fields; it never picks a secret winner.)

**Ingress after migration.** When `init` or another ingress command triggers migration in the same invocation, the post-migration keyring state is authoritative for whether a secret is already present and for any in-memory secret prefill. The command MUST NOT reread a plaintext credential source it may have just scrubbed.

**Machine-readable migration signal.** When migration occurs, JSON output paths emit a `_migration` field at the top level of the response object:

```json
{
  "_migration": {
    "version": 1,
    "changes": [
      {"field": "api_token", "from": "config:legacy_plaintext", "to": "keyring:atlassian-cli/default/api_token"}
    ]
  },
  ...rest of the response...
}
```

`version: 1` lets consumers detect the schema; `changes` is an array (multiple fields may migrate in one run). `from` and `to` are descriptive opaque strings — never include the value. The field appears exactly once, on the run where the migration occurred. Subsequent runs find nothing to migrate and the field is absent.

The CLI never performs this migration without surfacing it in the appropriate output path — stderr line for human runs, `_migration` block for JSON runs. Silent state changes during automation are non-compliant.

## §1.9 Org deployment model

An organization deploying a Collective CLI to its users can:

1. **Pre-stage secrets out-of-band.** Use MDM / `op run` / a custom installer to write keyring entries on user machines before the CLI ever runs.
2. **Ship a config file** under the CLI's config dir, containing the `credential_ref` pointing at those pre-staged entries.
3. **Skip `init`.** The CLI runs and reads from the keyring directly. `<cli> me` works on first invocation.

This is the path Monit's `claude-desktop-mcp` installer should take. The CLI's interactive `init` becomes the fallback for users without org deployment.

**Keyring entries are user-scoped.** Every backend this standard supports (macOS Keychain login keychain, Windows Credential Manager per-user vault, Linux Secret Service per-user collection, file backend in `~/.local/share/...`) stores entries against the *operating-system user account*, not against the machine. MDM-driven and installer-driven prestaging must therefore run **in the target user's context** — either as that user (sudo -u, `runas`), or by having the installer invoke the CLI (`<cli> set-credential` / `<cli> init`) as a step that the target user runs themselves on first login. An installer running as `root` or `SYSTEM` that writes to its own keyring has accomplished nothing useful for the end user.

## §1.10 Note on 1Password / external secret managers

1Password (and Vault, AWS Secrets Manager, etc.) are *installer-time* sources. **`set-credential` is the preferred automation primitive** for translating from an external secret manager into the OS keyring; it's purpose-built for this case (single-secret, stdin/env ingress, no API smoke test, scriptable exit codes — §1.5.2):

```
op read 'op://Vault/Item/field' | <cli> set-credential --ref <ref> --key <key> --stdin
```

For full first-time setup that includes both non-secret config and a secret, `init` can ingest the secret via the same stdin/env pattern (§1.5.1) — for example:

```
op read 'op://Vault/Item/token' | <cli> init --url <url> --email <email> --token-stdin
```

In automation, prefer `set-credential` per-secret over `init` for everything: it has a smaller surface, no verification round-trip to fail on, and one secret per invocation maps cleanly onto one `op read`.

**Default path: the CLI does not invoke 1Password at runtime.** Adding an `op` resolver to the CLI's runtime would entangle every CLI with `op`'s availability, version compatibility, and account configuration. The canonical boundary is installer-time: installers translate from external secret managers into the OS keyring, and the CLI reads from the OS keyring. `set-credential` is the preferred automation primitive for that translation.

A note on what credstore exposes: as of #23, `credstore` exposes only the five backends `keychain`, `wincred`, `secret-service`, `file`, `memory`. Surfacing external secret managers (1Password / KeePassXC / `pass`) as additional *native* keyring backends — which would make them runtime-visible to the CLI through credstore — is tracked in #24. If #24 lands, the "default path" above stays the recommendation for most users; the new native backends are an opt-in alternative for users who specifically want runtime resolution and accept the per-backend availability/version coupling.

## §1.11 Compliance criteria

A CLI is compliant with this standard when all of the following are true at runtime, observable from the user's perspective:

1. **`init` writes no access secret to the config file or to any plaintext file under the CLI's config dir.** Access secrets land in the keyring only. Deployment material (§1.2) may be written to plain files; it is not an access secret.
2. **Normal API commands resolve access secrets from the keyring, not from environment variables or config files.** Env vars and flags carry new secret material into the CLI only via `init` / `set-credential` (§1.5). A normal API command may write a *refreshed* token back to the active ref (§1.5 intro) — that is not ingress of new material from the user/environment, it's persistence of material the upstream service just issued. The one runtime-env-var exception for secret/unlocking material is the file-backend passphrase (§1.4), which is not itself a service token.
3. **`config show` (and `--json`) reports presence, backend, and ref for every secret — never values.** A user can confirm setup without seeing any secret material.
4. **`config clear` (default scope) removes only the keys under the active `credential_ref`.** Other profiles, other CLIs' keyring entries, and the config file are untouched.
5. **`config clear --all` returns the active profile to a pre-install state** — config file, keyring entries under the active ref, caches, and empty parent directories. Inactive profiles are not touched (§1.7.2).
6. **Legacy plaintext config migrates once on first read, then the plaintext field is removed from disk.** The migration prints one line to stderr and emits `_migration` in JSON output paths (§1.8). Re-running the CLI after migration finds no plaintext fields to migrate. Conflicts between plaintext and existing keyring values fail loudly per §1.8, never resolve silently. The migration test invokes the real CLI entrypoint on the migrating invocation and asserts the required migration signal is emitted on the relevant output path: stderr for human output and `_migration` for JSON output. At least one test covers migration followed by a non-zero command exit; the migration signal must still be emitted before process exit.
7. **The standard's contracts hold identically on macOS, Windows, and Linux.** Where the Linux file backend is in use, `config show` says so. A locked Linux Secret Service fails closed (§1.4); the CLI never silently downgrades a working desktop install to the file backend.
8. **No secret material appears in any output the CLI emits** — see §1.12.
9. **`set-credential` accepts only allowed keys via stdin or env, never via flag value.** Disallowed keys fail loudly (§1.5.2).
10. **The bundle key set conforms to the CLI's Part 2 section.** Every secret key the CLI can write, migrate, resolve, show, or clear under the active `credential_ref` is listed in that CLI's §2.x section, with required/optional status. Tests enumerate the active bundle after `init`, `set-credential`, migration, and clear flows and assert exact equality with the keys expected for that scenario. No unlisted key may appear.
11. **No intra-credential shadowing exists unless explicitly sanctioned.** A logical access credential resolves from one key. If a CLI's §2.x section explicitly permits multiple keys for the same logical credential, tests cover every write target and every higher-precedence key that could shadow it: after ingress or migration, normal resolution returns the value just written, and lower-precedence writes clear or update any higher-precedence keys that would otherwise shadow them.

These are the *minimum* acceptance criteria for any compliance work. CLI-specific criteria (e.g. `cfl` must drop the `/wiki` suffix from stored URLs; `slck` must store the workspace identifier) are listed in the relevant Part 2 §2.x section.

## §1.12 Logging, display, and telemetry

Storage is only half the problem. A CLI that puts secrets in the keyring but spills them through some other channel is non-compliant. The following are all banned:

- **Stdout / stderr output.** No secret in `--verbose`, `--debug`, `--trace`, normal info logs, or error messages. Error messages may name the keyring ref, the key, the backend, and the operation — never the value. "Authentication failed for ref `atlassian-cli/default`" is fine. "Authentication failed with token abcd1234" is not.
- **HTTP traces / wire logs.** When the CLI exposes a request-dumping mode (`--http-debug` or similar), `Authorization`, `Cookie`, `Set-Cookie`, and any custom auth headers are redacted to a length-only placeholder (`Authorization: Bearer <redacted, len=84>`). Request bodies that contain credentials (OAuth code-exchange POSTs, login forms) are redacted by field.
- **Panic output / stack traces.** A panic must not include secret-bearing variables. Recover handlers scrub `error.Error()` strings that contain known-secret substrings before display.
- **`--help` and shell examples.** Documentation shows `--stdin` and env-var patterns. Examples never inline a token value, even an obviously-fake one — fake tokens get cargo-culted into real configs. Use `<your token>` or `$TOKEN_FROM_OP` placeholders.
- **Telemetry / crash reporting.** If the CLI ever emits telemetry (currently none do; reserved for future), event payloads include no secret material, no full URLs that might embed tokens in query strings, no environment variable dumps. The telemetry schema is allowlist-based, not blocklist-based.
- **Shell completion scripts.** Completion handlers that read partial command lines (for argument completion) never log the input, because users do tab-complete `--token=xoxb-` and watch it get captured.

**Test obligation.** The acceptance test suite for each CLI includes a "no-leak" test: run every command class with a known-distinctive token value loaded into the keyring, capture all output channels (stdout, stderr, any log files the CLI writes), and grep for the token. Any hit fails the build.

---

# Deriving Work From This Standard

Bridge between Part 1 (the rules) and Part 2 (one CLI's worth of work). When adding a new CLI or revisiting an existing one, work through these steps in order. Part 2's per-CLI sections are concrete instances of this loop.

**For each CLI:**

1. **Classify every piece of state the CLI handles** into four buckets per §1.2:
   - Access secrets → keyring.
   - Deployment material → plain files in the config dir.
   - Non-secret config → `config.yml`.
   - Caches → cache directory.
   If a value seems to straddle buckets, the classification criteria in §1.2 decide. Don't invent a fifth bucket.

2. **Define the CLI's keyring coordinates** per §1.3:
   - Canonical `service` segment (matches the CLI's repo / module name, e.g. `slack-chat-api`).
   - Default `credential_ref` (`<service>/default`).
   - Well-known key names within the bundle (`api_token`, `bot_token`, `oauth_token`, …).

3. **Route all runtime credential resolution through `cli-common/credstore`.** No `os.Getenv("FOO_TOKEN")` reads in API client code. No reading the config file for a token field. The keyring is the only authoritative source at runtime.

4. **Constrain where secrets can enter the system to `init` and `set-credential` only.** Env vars, flags, stdin, clipboard — all of these are *ingress* mechanisms valid during setup. None are valid during normal operation. Audit existing command implementations for env-var reads that bypass `init`; these are the cargo-culted legacy paths the standard is replacing.

5. **Implement the `config show` and `config clear` surfaces per §1.6 and §1.7.** Acceptance items 3, 4, 5 from §1.11 are the test.

6. **Implement legacy migration per §1.8.** The migration runs once on next read of the legacy config, writes to the new location, removes the legacy field. Subsequent runs find nothing to migrate.

7. **Write tests that assert the §1.11 acceptance criteria.** Specifically: a test that runs `init`, then inspects the config file and asserts it contains no user-secret fields. A test that runs a normal API command with the keyring populated but no env vars and no secret-bearing config, and asserts it works. A test that runs `config clear` and asserts only the active ref's keys were removed.

If a step turns up something Part 1 didn't anticipate (a new credential class, a CLI that genuinely needs runtime env-var resolution for an unusual reason, a backend that doesn't behave like the others), **stop and update Part 1.** Don't quietly diverge.

---

# Part 2 — Migration Manifest

> **Agent guardrail.** Existing CLIs in this manifest contain code that treats env vars, config-file fields, and ad-hoc plaintext files as first-class runtime credential sources. **Do not preserve these as runtime sources for convenience.** They are exactly what this migration removes. Env vars and flags remain valid as `init` / `set-credential` *ingress* paths (per derivation step 4 above). Anything else is a legacy path to be migrated or deprecated, not copied forward into the new design. If a legacy behavior seems load-bearing, surface it as a compatibility exception in this document before implementing it — do not infer one.

Cross-CLI prerequisite work, then per-CLI items. Should be sequenced before `cli-deployment-manifest.md` (the installer / `config clear --all` work depends on this).

## §2.1 Cross-CLI prerequisite — `cli-common/credstore`

The shared `cli-common/credstore` package is the foundation everything else depends on. Its surface needs to be wide enough to support every Part 1 contract; a thin wrapper that exposes only `Get`/`Set` will force each CLI to re-derive pre-write checks, atomicity, validation, and migration logic, and the implementations will drift.

**Hosting.** A separate `open-cli-collective/cli-common` repo with semver tags, consumed by each CLI as a normal Go module dependency (`require github.com/open-cli-collective/cli-common vX.Y.Z` in `go.mod`). Lets non-Collective CLIs adopt it too. Whether any individual CLI also commits a `vendor/` directory for hermetic builds is a per-repo choice unrelated to this standard.

**API surface (working names; agent picks final naming):**

The package centers on a service-scoped `Store` returned by `Open`. Backend selection depends on service (env vars are service-derived: `<SERVICE>_KEYRING_BACKEND`, `<SERVICE>_KEYRING_PASSPHRASE` — §1.4), so the service must be in scope before any operation. Operations take `(profile, key)` against the opened store; refs are joined and split by package-level helpers when callers need to display or parse the full `<service>/<profile>` string.

- **Open / lifecycle.**
  - `credstore.Open(service string, opts *Options) (*Store, error)` — opens a service-scoped store, resolves backend selection at this point (including reading `<SERVICE>_KEYRING_BACKEND` and `<SERVICE>_KEYRING_PASSPHRASE`), applies the §1.4 Linux classification rules.
  - `(*Store).Close() error` — releases backend resources; safe to call multiple times.
  - `(*Store).Backend() (backend, source Source)` — `backend` is one of `keychain` / `wincred` / `secret-service` / `file`; `source` describes how it was selected (`auto`, `env`, `config`). No error return — the store is already open and the backend is known. Required for `config show` (§1.6).
  - **Allowed-key validation is CLI-provided.** The shared package validates ref / profile / key *syntax* (the `[A-Za-z0-9_-]` character set, no `/` inside segments — §1.3) but cannot know which key names a given CLI considers valid (`bot_token` for slck, `api_token` for atlassian-cli, etc.). Each CLI passes its own allowed-key allowlist when opening the store: `Options.AllowedKeys []string`. The store then enforces it: `Set` / `SetBundle` / `Delete` reject any key not in the allowlist with a clear error listing the allowed set (per §1.5.2). CLIs that omit `AllowedKeys` get syntax-only validation, useful for tooling like `cli-common`'s own tests; production CLI code always supplies the allowlist.
- **Ref handling (package-level, no store needed).** `ParseRef(string) (service, profile, error)`; inverse `FormatRef(service, profile)`. Enforce the `[A-Za-z0-9_-]` character set per §1.3, reject `/` inside any segment, return a typed error so callers can produce actionable messages. `EscapeRefSegment(raw)` helper for CLIs that need to derive a profile from a richer identifier (e.g. a user email).
- **Single-key operations on a `Store`.** `Get(profile, key) (value, error)`, `Set(profile, key, value string, opts ...SetOpt) error`, `Delete(profile, key) error`, `Exists(profile, key) (bool, error)`. `SetOpt` carries `Overwrite` (per §1.5).
- **Bundle operations on a `Store`.** `ListBundle(profile) ([]key, error)` — required for `config show`, pre-write checks (§1.5.1), `config clear` (§1.7), and migration conflict detection (§1.8). `DeleteBundle(profile) error` — used by `config clear`.
- **Atomic-ish multi-key write.** `SetBundle(profile string, kv map[string]string, opts ...SetOpt) (Result, error)` implementing the §1.5.1 contract: validate all inputs, snapshot existing values for keys present in the map when `Overwrite` is set, write all, restore from snapshot on partial failure, zero the snapshot before return. `Result` reports which keys were written, which were restored, which were left untouched.
- **Linux backend classification.** Internal to `Open`'s backend-resolution logic. Distinguishes "unavailable" from "denied/locked" per §1.4, with the "fail closed on ambiguous" rule baked in. Exposed as test seams so each CLI's no-leak / fail-closed tests can drive both paths.
- **File-backend opt-in plumbing.** `Open` honors `<SERVICE>_KEYRING_BACKEND=file` (per §1.4) and the `keyring.backend: file` config-file knob (passed via `Options`); passes through `<SERVICE>_KEYRING_PASSPHRASE` (also §1.4). The package does not hardcode service names.
- **In-memory backend.** A `Memory` backend implementation used by tests (selected via `Options.Backend = BackendMemory`), identical behavior contract to the real backends, **no disk side effects**. Every CLI's no-leak and atomicity tests run against this. Critical for CI on machines that don't have a usable keyring.
- **Redaction helpers (package-level).** Redaction needs to know which strings are secrets — a `Redact(s string) string` taking only the input couldn't do anything useful. The shape:
  - `NewRedactor(secrets ...string) *Redactor` — constructs a redactor pre-loaded with the secret values to scrub.
  - `(*Redactor).Add(secret string)` — for secrets discovered after construction (e.g. a refreshed token).
  - `(*Redactor).Redact(s string) string` — scrubs every loaded secret from `s` (including substring matches; replaces with `<redacted, len=N>` where N is the original length).
  - `(*Redactor).RedactHeaders(http.Header)` — for HTTP wire logs; redacts `Authorization`, `Cookie`, `Set-Cookie`, and any header whose value contains a loaded secret.
  - `(*Redactor).RedactWriter(io.Writer) io.Writer` — wrapping helper so a CLI's debug-log writer auto-scrubs without each call site remembering.
  - `NoLeakAssertion(output []byte, secrets ...string) error` — test helper. Returns a non-nil error naming the secret (but not its value) if any secret appears in `output`, else nil.

  Every CLI builds a `Redactor` populated with the secrets it just loaded from the keyring (and any obtained at runtime via refresh) and uses it for `--http-debug` / `--verbose` paths per §1.12.
- **Migration helpers (package-level).** `EmitMigrationStderr(field, ref string)` and `MigrationJSONEntry(field, from, to string)` produce the standard's one-line stderr message and `_migration` JSON object shape (§1.8). Avoids each CLI re-inventing the format.

Sketch of a typical call site:

```go
s, err := credstore.Open("atlassian-cli", &credstore.Options{
    AllowedKeys: []string{"api_token"},
})
if err != nil { return err }
defer s.Close()

token, err := s.Get("default", "api_token")
// ...

backend, source := s.Backend()
fmt.Fprintf(os.Stderr, "using %s backend (selected via %s)\n", backend, source)
```

**Other §2.1 deliverables:**

- `credential_ref` default format: `<service>/default`. Codified in the package.
- Migration log line format: one line to stderr, `migrated <field> to keyring at <ref>; this is a one-time operation`. JSON output paths emit a `_migration` field per §1.8.
- Conflict-resolution helper that implements §1.8's plaintext-vs-keyring decision tree (detect, emit error, never print values).
- README in `cli-common` linking back to this document. The package's godoc references the §-numbers it implements.

**Tests in `cli-common` (not optional — the rest of the manifest treats credstore as load-bearing):**

- Round-trip tests against macOS Keychain, Windows Credential Manager, Linux Secret Service (under D-Bus), and the file backend.
- Unit tests against the in-memory backend for `SetBundle` atomicity (including induced mid-bundle failures and snapshot restoration).
- Linux fail-closed tests (a mocked Secret Service that returns `Locked`, `Denied`, ambiguous errors).
- Ref-parsing fuzz/property tests (no `/`-containing input survives without an error; valid input round-trips).
- Redaction tests (assertion: no known secret appears in any output channel).

**Per-CLI deliverables that Part 1 implies but Part 2 must spell out explicitly** — every CLI in §2.2–§2.5 gets all of these, regardless of starting state:

1. Dependency on `cli-common/credstore` at a pinned version.
2. `init` reworked: secret ingress via `--<key>-stdin` and `--<key>-from-env <ENV>` only; no `--<key>=<literal>` for secret-bearing flags; calls `SetBundle` with `Overwrite` semantics per §1.5.1.
3. `set-credential` subcommand added (§1.5.2) with the full flag and behavior contract.
4. `config show` and `config show --json` reporting per §1.6, including deployment-material reporting by path/presence/fingerprint where applicable.
5. `config clear` and `config clear --all` per §1.7.1 / §1.7.2 (active-profile scope), plus `--dry-run` per §1.7.3.
6. Legacy migration per §1.8 (one-time, stderr line, `_migration` JSON field, plaintext-vs-keyring conflict failure).
7. `<cli> me` post-init smoke-test command (or equivalent), used by installers for verification.
8. No-leak test suite per §1.12 using `cli-common`'s `NoLeakAssertion` helper.
9. `--help`, README, and shell-example sweep: remove every `--token=<literal>`, `SLACK_API_TOKEN=...` env-runtime, etc. that conflicts with Part 1. Replace with `--token-stdin` / `op read | <cli> set-credential` patterns.
10. CI matrix: macOS + Linux at minimum, Windows where the CLI is distributed there. The in-memory backend covers the gap when CI doesn't have a real keyring.
11. Hermetic secret-store tests: unit tests use a non-OS backend or explicit in-memory fake and must never read the developer's real OS keychain. Pure planning/reconciliation helpers take already-resolved values as parameters and perform no keyring I/O.

## §2.2 `atlassian-cli` (`jtk` + `cfl`) — biggest lift

**Standard mapping:** Exercises §1.2 (access secret moves out of plaintext config), §1.3 (introduces a single top-level `credential_ref` shared by both binaries), §1.7 (both clear scopes), §1.8 (auto-migration of plaintext `api_token` field). CLI-specific item not in the general acceptance set: storing the Confluence base URL without the `/wiki` suffix.

This CLI today stores the API token in plaintext yaml/json across multiple files. The shared `~/.config/atlassian-cli/config.yml` has a `default` section plus optional per-tool `jtk` / `cfl` sections that may override any field, **including `api_token`** — i.e. a user can today have one token for `jtk` and a different one for `cfl`. **This per-tool-token capability is being dropped.** A user who needs different credentials for different Atlassian contexts uses the standard's multi-tenancy model — separate `credential_ref` profiles at the *Atlassian-tenant* level (e.g. `atlassian-cli/work` and `atlassian-cli/personal`), each holding one `api_token` used by both `jtk` and `cfl`. Multi-tenancy at the tool level was a vestige of separate codebases; it has no semantic justification once jtk and cfl share a config.

**`credential_ref` design — single top-level ref.**

```yaml
url: https://signalft.atlassian.net
email: rstockbower@example.com
auth_method: basic                          # basic | bearer
cloud_id: 11111111-2222-3333-4444-555555555555   # required for bearer auth; unused for basic
credential_ref: atlassian-cli/default       # one token, shared by jtk and cfl

jtk:
  default_project: MON                      # tool-specific non-secret config still allowed

cfl:
  default_space: ENG                        # tool-specific non-secret config still allowed
```

`credential_ref` lives at the top level of `config.yml`. Per-tool sections may still hold tool-specific non-secret fields (default project, default space, output format), but **may not override `credential_ref`, `url`, `email`, `auth_method`, or `cloud_id`** — the new schema enforces this. The ref's bundle holds the single key `api_token` and is shared by both tools.

This is the atlassian-cli application of §1.3's one-key-per-logical-credential rule and §1.11's bundle-key conformance criteria (§1.11.10–§1.11.11). Current implementation deviation to remediate: MON-5326 / atlassian-cli#367 tracks removal of per-tool API-token keys such as `cfl_api_token` and `jtk_api_token`; they are not a sanctioned exception.

**Migration flattens the legacy `default:` section into the top level.** The current shared config nests connection fields under a `default:` block. Post-migration, those fields live at the top level (as shown above); only the per-tool `jtk:` / `cfl:` sub-sections remain as keyed blocks. Migration reads the old `default.*` values and writes them flat.

Multi-tenancy (per-tenant profile UX — `--profile work` vs `--profile personal`) remains the open §2.7 item; it applies to the whole `atlassian-cli` consistently when it lands.

**Work:**

- Move `api_token` from the shared yaml (and all legacy locations) into the keyring under the resolved `credential_ref`.
- Add `credential_ref` field handling per the design above. Default `atlassian-cli/default`.
- **Drop the legacy per-tool files.** Auto-migrate on first read, print one-line notice.
  - **Required first step: inventory actual current and historical paths before implementing migration.** Read every config-loading path in both `jtk` and `cfl` (current main branches plus any released tags still in user hands), and enumerate every directory each binary has *ever* written to on Linux, macOS, and Windows. The known candidates listed below are starting points, not a closed set; agents must verify against the actual codebases.
  - Known candidates to verify and likely include:
    - `~/.config/jira-ticket-cli/config.json` (Linux/XDG path)
    - **`~/Library/Application Support/jira-ticket-cli/config.json`** (macOS — current `jtk` code uses `os.UserConfigDir()`, which on macOS returns the Library path, not `~/.config`)
    - Windows: `%AppData%\jira-ticket-cli\config.json` (the Windows return of `os.UserConfigDir()`)
    - `~/.config/cfl/config.yml`
    - macOS and Windows equivalents for `cfl` if it ever used `os.UserConfigDir()`
  - Migration must read every candidate that exists for the current user, feed all discovered tokens into the multi-source conflict-resolution rule below, then remove the legacy files on success.
- Fix the `/wiki` suffix: store base URL only (`https://org.atlassian.net`). Append `/wiki` at Confluence API call time. Auto-migrate existing configs that have `/wiki` baked in.
- Update `jtk init` / `cfl init` to write the token to the keyring (not the yaml), using stdin/env ingress per §1.5.1. The `--token=<literal>` flag, if it exists, becomes a hard error pointing at the new flags.
- Update `config show` to report keyring backend and the (single, shared) ref per §1.6. `jtk config show` and `cfl config show` show identical token-status output since they share the bundle.
- Update `config clear` per §1.7. Default scope clears the shared ref's `api_token`, which deauthenticates both `jtk` and `cfl` simultaneously — desired and consistent with the "one Atlassian credential" design. `config clear --all` is **active-profile scope** per §1.7.2 (config + caches + active ref's keyring entry); other Atlassian-tenant profiles are not touched.
- **Migration conflict handling — multiple plaintext token sources.** A user's pre-migration state can include several distinct plaintext `api_token` sources. The path-inventory requirement above may surface additional historical files; the list below is the *known* set, not closed:
  1. `default.api_token` in the shared `~/.config/atlassian-cli/config.yml`
  2. `jtk.api_token` override in the same shared file
  3. `cfl.api_token` override in the same shared file
  4. `api_token` in the legacy `~/.config/jira-ticket-cli/config.json` (or its macOS `~/Library/Application Support/...` / Windows `%AppData%\...` path)
  5. `api_token` in the legacy `~/.config/cfl/config.yml` (or its macOS / Windows equivalents if applicable)

  Plus any additional plaintext sources surfaced by the path-inventory step.

  **Migration rule:** collect every discovered plaintext token value across all known and inventoried sources. If all discovered values are byte-identical, migrate the single value into the keyring under the resolved ref. If two or more distinct values exist, **fail loudly per §1.8**: name every source where a token was found, state that values differ, do not print any value (masked or otherwise), and point the user at remediation — install the desired token under `atlassian-cli/default` with `set-credential`, then re-run; or, when multi-tenancy ships, install distinct tokens under distinct profiles. Silent picking is prohibited.
- Confirm Windows behavior: today neither tool uses Credential Manager on Windows. Post-migration, they will.
- Cross-tool sibling-detection logic in `init` (the "use values from cfl/jtk you've already configured" prompt) becomes a `--inherit-sibling` flag for the deployment manifest, and is non-interactive when invoked with it. Since the credential is now shared, "inherit sibling" is essentially the default for everything except a brand-new install.

## §2.3 `gro` (Google read-only)

**Standard mapping:** Exercises §1.1 (replace `security` / `secret-tool` shell-outs with the shared lib), §1.2 (the OAuth client JSON is deployment material; the per-user OAuth token remains an access secret), §1.3 (`credential_ref`), §1.4 (Windows backend support), §1.6 (deployment-material reporting by fingerprint), §1.8 (migration of *two* legacy files: `credentials.json` and `token.json`).

Already keychain-backed for the user OAuth token on macOS/Linux but uses shell-outs to `security` and `secret-tool`, keeps the OAuth client JSON in plaintext on disk, and falls back to a plaintext `token.json` when the keychain backend isn't available. There are therefore **two distinct on-disk artifacts** to address, not one.

**Config file structure (`~/.config/google-readonly/config.yml`):**

```yaml
credential_ref: google-readonly/default
oauth_client_path: ~/.config/google-readonly/oauth_client.json
cache_ttl_hours: 24
granted_scopes:                # tracked for stale-token detection per current behavior
  - https://www.googleapis.com/auth/gmail.readonly
  - ...
```

The `granted_scopes` field is preserved from current behavior (used to detect when a token's scopes have drifted from what `init` granted). If a future cleanup pass removes that detection, the field can drop with it.

**Work:**

- Replace `security` / `secret-tool` shell-outs with `cli-common/credstore`.
- Add `credential_ref` to `~/.config/google-readonly/config.yml` (default `google-readonly/default`). Migrate `config.json` → `config.yml` for format consistency with the other CLIs.
- **OAuth client JSON** (deployment material per §1.2). Path: `~/.config/google-readonly/oauth_client.json` with platform-appropriate file permissions for non-secret org-internal data (on POSIX, 0644 is fine; tighter is also fine; the file is not a credential). `oauth_client_path` field in `config.yml` lets an org override the location (default: that path). Installers ship this file as part of the deployment; no keyring round-trip, no `op` integration for this file, no 1Password secret-notes mangling.
- **Auto-migrate legacy `credentials.json`** (deployment material): copy to `oauth_client.json` (or wherever `oauth_client_path` points), update config, leave a one-line notice. Source location: `~/.config/google-readonly/credentials.json`.
- **Auto-migrate legacy `token.json`** (access secret — this is the bigger lift). `token.json` was the file-fallback location for the per-user OAuth token when the keychain backend wasn't available. Migrate its contents into the keyring under `google-readonly/default/oauth_token`, then **remove the plaintext file**. Conflict rules from §1.8 apply: if a token already exists in the keyring and `token.json` differs, fail loudly, don't pick a winner.
- **OAuth user token** post-migration: keyring only, under the ref, key `oauth_token`. This is what `config clear` rotates.
- Add Windows Credential Manager support for the user token (today `gro` on Windows likely falls through to a plaintext file token — verify and fix as part of the `token.json` migration).
- `config show`: backend, ref, presence of `oauth_token`. For the OAuth client JSON, report **path + presence + content fingerprint** per §1.6; do not inline the JSON contents by default. `--verbose` may inline.
- **Move the cache out of the config dir.** Today `gro`'s Drive metadata cache lives under the config directory (alongside `config.json` / `credentials.json` / `token.json`), which §1.2 classifies as the wrong place. Move it to the platform cache location: `$XDG_CACHE_HOME/google-readonly` on Linux (default `~/.cache/google-readonly`), `~/Library/Caches/google-readonly` on macOS, `%LOCALAPPDATA%\google-readonly\Cache` on Windows. Auto-migrate by recreating the cache at the new location on first run; the old location can be deleted as part of the same migration since cache contents are recomputable. `config clear --all` (§1.7.2) cleans the new cache location; for one deprecation cycle it also cleans any leftover cache files under the old location.
- Two-phase install UX (browser round-trip) documented as an explicit installer step; `gro init --no-browser` + an `--auth-code-stdin` (or similar) lets the installer pause cleanly between phases. This is a deployment-manifest item, but flagged here because it's the only `init` that intrinsically can't be one-shot.

## §2.4 `slck` (Slack)

**Standard mapping:** Exercises §1.1 (replace shell-out), §1.2 (introduce config file for non-secret values — workspace identifier — that today live nowhere), §1.3 (multi-key bundle: `bot_token` + `user_token` under one ref), §1.4 (Windows backend), §1.8 (keyring key-name and possibly service-name migration), §1.11 (drop `SLACK_API_TOKEN` as a runtime env source).

Already keychain-backed on macOS (the current code may use service name `slack-chat-api` and account name `api_token` for the bot token, or in older releases the service `slck`; the migration must cover both). Non-macOS code (Linux **and** Windows) falls back to a plaintext credentials file. Most heavily used of the remaining three; sequence ahead of `nrq`. Work:

- Replace `security` shell-out with `cli-common/credstore`.
- Introduce a config file at `~/.config/slack-chat-api/config.yml`. Fields: `credential_ref` (default `slack-chat-api/default`) and `workspace` (the workspace identifier — captured at `init` time, used for verification by `slck me`; not strictly needed for API calls but valuable in `config show` and for org deployment scripts to assert against).
- Keys under the new keyring bundle: `bot_token`, `user_token`.
- **Legacy keyring locations to migrate from.** The migration reads any of the following that exist, writes to the new layout, removes the originals:
  - `service=slck, account=api_token` → new: `slack-chat-api/default/bot_token`
  - `service=slck, account=user_token` → new: `slack-chat-api/default/user_token`
  - `service=slack-chat-api, account=api_token` → new: `slack-chat-api/default/bot_token`
  - `service=slack-chat-api, account=user_token` → new: `slack-chat-api/default/user_token`
  Note the rename from `api_token` → `bot_token` (the new key name better reflects what the token actually is). Where multiple discovered values for the same logical key are byte-identical, migrate the single value. Where they differ, §1.8 conflict rules apply (fail loudly, name all sources, never print values).
- **Legacy plaintext-file fallback to migrate from.** Current non-macOS code (Linux and Windows both) writes tokens to a plaintext file at `~/.config/slack-chat-api/credentials` (`%APPDATA%\slack-chat-api\credentials` on Windows; verify exact path). Migration reads `api_token` and `user_token` from this file, writes them to the keyring under `slack-chat-api/default/bot_token` and `slack-chat-api/default/user_token` respectively, then **deletes the plaintext file**. Same conflict rules as above if the file's value differs from a value already in the keyring.
- Add Windows Credential Manager support so post-migration Windows users no longer use the plaintext file (today Windows users get the file fallback).
- **Hard-deprecate `config set-token <literal>`.** It violates §1.5 in two ways: positional secret ingress is even worse than flag-passed (no `=` to grep for in shell history, no obvious hint the argument is sensitive). The new behavior: invoking `slck config set-token` with a positional argument **exits nonzero immediately** with a message naming the migration path (`slck set-credential --ref slack-chat-api/default --key bot_token --stdin`, with `op read | ...` example). The command does not accept the value via any path — not flag, not positional, not stdin under the old name. The new ingress lives at `slck set-credential` per §1.5.2.
- **Drop `SLACK_API_TOKEN` and `SLACK_USER_TOKEN` as runtime env sources** per §1.11 acceptance item 2. Today these env vars are read by the running CLI as primary credential sources; post-migration they are accepted only as ingress via `init --bot-token-from-env SLACK_API_TOKEN` (etc.). Audit every API client read path and remove direct `os.Getenv` calls. Update README and `--help` examples to use `op read | slck set-credential ... --stdin` and `init --bot-token-from-env`.
- Multi-tenancy is out of scope for this migration. The standard's `credential_ref` mechanism supports it (a user could in principle hold two profiles), but no CLI surface for picking between profiles is added now.
- `config show` reports backend, ref, and which keys are present.

## §2.5 `nrq` (New Relic)

**Standard mapping:** Exercises §1.1 (replace shell-out), §1.2 (move `account_id` and `region` out of credential storage and into a new config file — they are non-secret config, not access secrets), §1.3 (`credential_ref`), §1.4 (Windows backend), §1.8 (migration from macOS keychain entries and the Linux credentials file), §1.11 (drop `NEWRELIC_API_KEY` as a runtime env source).

Already keychain-backed via `security` shell-out on macOS. Non-macOS code (Linux **and** Windows) falls back to a plaintext credentials file. Work:

- Replace `security` shell-out with `cli-common/credstore`.
- Introduce a config file at `~/.config/newrelic-cli/config.yml`. Fields: `account_id`, `region`, `credential_ref` (default `newrelic-cli/default`). `account_id` and `region` move out of the keyring — they're not secrets, and putting them in the config file makes org deployment much easier (installer stamps them in once, never re-prompts).
- Keys under the keyring bundle: `api_key`.
- Add Windows Credential Manager support (today Windows users get the file fallback).
- **Auto-migrate from the legacy plaintext-file fallback.** Current non-macOS code (Linux and Windows both) writes credentials to `~/.config/newrelic-cli/credentials` (`%APPDATA%\newrelic-cli\credentials` on Windows; verify exact path). The file is a flat `key=value` format (not an INI; no `[sections]`), one line each for `api_key`, `account_id`, `region`. Read all three; `api_key` → keyring; `account_id` and `region` → new `config.yml`. After successful migration, delete the file. Same §1.8 conflict rules if values disagree with anything already in the keyring or new config.
- **Auto-migrate from current macOS keychain entries.** Three separate accounts under service `newrelic-cli`: `api_key`, `account_id`, `region`. `api_key` → new keyring location under the ref; `account_id` and `region` → new `config.yml`; delete the old keychain entries.
- **Drop `NEWRELIC_API_KEY` as a runtime env source** per §1.11. Today the running CLI reads `os.Getenv("NEWRELIC_API_KEY")` as a primary credential source. Post-migration, that env var is accepted only as ingress via `init --api-key-from-env NEWRELIC_API_KEY` or `set-credential --from-env NEWRELIC_API_KEY`. Audit and remove direct `os.Getenv` calls in API client code.
- **`NEWRELIC_ACCOUNT_ID` and `NEWRELIC_REGION` are kept as non-secret runtime env overrides.** Decision: they're non-secret runtime knobs, not credentials, and don't fall under §1.11 acceptance item 2's ban (which is scoped to secret/unlocking material — §1.4). Precedence is **env > config-file**. Useful for multi-account scripting (`NEWRELIC_ACCOUNT_ID=12345 nrq query ...`) without needing a separate config or profile. Document the precedence in `--help`; show the resolved source in `config show`.
- `config show` reports backend, ref, presence of `api_key`, the resolved values for `account_id` / `region`, **and the source of each non-secret value** (env-override vs config-file).
- **Hard-deprecate `config set-api-key <literal>`** per §1.5 — positional secret ingress is banned alongside flag-passed. Invoking it with a positional argument exits nonzero with a message naming the migration path (`nrq set-credential --ref newrelic-cli/default --key api_key --stdin`, with `op read | ...` example). The command does not accept the value via any path under the old name.
- Collapse `config set-account-id` and `config set-region` into `nrq config set --account-id ... --region ...` writing to `config.yml`. These are non-secret and may continue to accept positional or flag arguments; only secret-bearing subcommands are bound by §1.5's ingress rules. Keep the old subcommands for one deprecation cycle as thin aliases.

## §2.6 Sequencing — actual execution and remaining work

This section originally posed sequencing as a choice between two options, recommending engineering-risk-minimization (foundation → one warmup CLI → atlassian → gro, with the remaining CLI in parallel). The plan evolved during Phase B execution; the recorded option-A/option-B framing is preserved at the bottom of this section for historical context but is no longer authoritative. The `credstore-phase-b-plan` memory is the source of truth for what was actually done.

**Phase B — executed order.**

- **B0** — `cli-common/credstore` foundation (§2.1). Established the shared package at `github.com/open-cli-collective/cli-common` with the in-memory backend, ref parsing, redaction helpers, atomic bundle writes, and the Linux backend-classification rules. Tagged and consumable as a Go module dependency.
- **B1** — `slck` pilot (§2.4). First real consumer; chosen to exercise the multi-key bundle (`bot_token` + `user_token`) and the service-name migration (`slck` → `slack-chat-api`) against a young credstore. Pilot template merged at `d9f88fd`.
- **B2** — `gro` (§2.3). Second consumer; validates the pilot template on another low-risk single-binary CLI before atlassian.
- **B3** — `atlassian-cli` (§2.2), tracked as INT-440 and atlassian-cli#365. One PR migrating both `jtk` and `cfl` via the shared `atlassian-go` module — the trickiest migration of the set (two tools sharing one module, multi-section legacy config, cross-platform legacy file inventory).

**How this differs from the original Option A.** Option A as written put atlassian *third* (foundation → one warmup CLI → atlassian → gro). The executed order slotted `gro` in as a second low-risk warmup *before* atlassian, making atlassian fourth. The reasoning matches Option A's spirit — exercise credstore on simpler consumers first — but with two warmup CLIs instead of one, on the judgement that the gain from a second template validation outweighed the cost of one more cycle before atlassian.

**Remaining work, in approximate order:**

1. **`nrq`** (§2.5). The one CLI of the five not yet migrated. Can run in parallel with anything below it.
2. **No-leak test landed in each per-CLI PR** (§1.12). Already the practice for B1–B3; mentioned here so it doesn't get dropped for `nrq`.
3. **Installer / `config clear --all` rollout** in `cli-deployment-manifest.md` §1.5 and §3.
4. **Profile selection / multi-tenant UX** (§2.7). Independent of everything above; revisit when there's a concrete need.

---

**Historical: original options as written.** Preserved verbatim from the pre-execution version of this section so the original reasoning is recoverable. Not authoritative.

> **Option A — Engineering risk minimizing (recommended).** Atlassian is the biggest single lift (two binaries, shared multi-section config, per-tool ref fallback, `/wiki` URL fix, multiple legacy file paths across two OSes). Doing it second-after-credstore puts the most complex consumer in front of a young shared library, with predictable consequences. Instead:
>
> 1. **`cli-common/credstore`** (§2.1) with the full test matrix per the §2.1 deliverables. Do not let this skip CI on the platforms it will eventually serve.
> 2. **One reference migration to harden credstore: `nrq` or `slck`.** Pick `nrq` if you'd rather exercise the env-var-runtime-source removal first (smaller blast radius if you get it wrong; fewer downstream users). Pick `slck` if you'd rather hit the multi-key bundle (`bot_token` + `user_token`) and the service-name migration (`slck` → `slack-chat-api`) early, since both are credstore-stressing.
> 3. **`atlassian-cli`** (§2.2). At this point credstore has been through one round of real consumer feedback and its rough edges are filed down.
> 4. **`gro`** (§2.3). The two-phase OAuth dance is novel enough that doing it last lets you learn from the other three first.
> 5. **In parallel with #3 and #4:** the other of `nrq` / `slck` that wasn't picked for #2.
> 6. **Then:** installer / `config clear --all` work in `cli-deployment-manifest.md` §1.5 and §3.
>
> **Option B — User-visible impact maximizing.** Keep `atlassian-cli` second-after-credstore (it has the most users and the most acute pain — plaintext token on disk). Accept that credstore will go through teething pains on its hardest consumer first. **Only viable if §2.1's test suite is genuinely thorough** (in-memory backend, all four backends in CI, atomicity tests, fail-closed tests). Without that, the debugging happens in `atlassian-cli`'s PR review and slows everything down.
>
> 1. `cli-common/credstore` (§2.1) — with rigorous tests.
> 2. `atlassian-cli` (§2.2).
> 3. In parallel after #1: `slck` (§2.4), `nrq` (§2.5), `gro` (§2.3).
> 4. Then: installer work.
>
> **Original recommendation: Option A.** The user-visible impact gap between "atlassian second" and "atlassian third" is one or two weeks; the engineering risk of debugging credstore through atlassian's complexity is materially worse.

## §2.7 Open questions

Decisions captured above (per-service env vars; `account_id` and `region` moved to nrq's config file; slck workspace stored in config; multi-tenancy out of scope; gro OAuth client JSON treated as deployment material in a plain file; `_migration` JSON shape pinned in §1.8). One item remains:

1. **Profile selection / multi-tenant UX.** The standard supports it (multiple `credential_ref`s per user), but the CLI surface for picking between profiles — `--profile foo`, `SLCK_PROFILE=foo`, separate config files, `~/.config/<service>/profiles/<name>.yml` — needs its own design pass. Not blocking; the migration is designed so this can be retrofitted without breaking existing single-profile users.
