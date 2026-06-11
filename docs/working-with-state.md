# working-with-state.md — four pillars; Codex-converged on all four

> Status: **All eight decisions log sections resolved; Codex architecture
> pressure-test CONVERGED on the original three pillars at round 5
> (`blockers=0 majors=0 minors=0 nits=0`, 2026-05-19) and on the Data
> pillar (§5) at round 7 (`blockers=0 majors=0 minors=0` after applying
> 2 minors, 2026-05-28). Full per-round disposition in §8 — rounds 1–5
> covered the original three pillars; rounds 6, 6-cleanup, and 7 covered
> Data.** Companion pillar to `working-with-secrets.md` (which also
> moves into `cli-common/docs/` so both pillars co-version). This doc is
> the source of truth for **non-secret on-disk state** across the Open
> CLI Collective Go CLI family. Homed here (`cli-common/docs/`),
> versioned with the `cli-common` state components (path/dir resolver +
> cache), pinned per-CLI like the credstore API (tag-before-close,
> INT-310).

---

## 1. Scope & the four pillars

A CLI puts exactly four kinds of state on disk/keyring. Each has one owner:

| Pillar | Kind of state | Where | Owning doc |
|--------|---------------|-------|------------|
| **Secrets** | access credentials | OS keyring (`cli-common/credstore`) | `working-with-secrets.md` |
| **Config** | durable, authored, non-secret | `os.UserConfigDir()/<tool>` | **this doc §3** |
| **Cache** | disposable, derived, regenerable | `os.UserCacheDir()/<tool>` | **this doc §4** |
| **Data** | program-managed working state | `XDG_STATE_HOME` (Linux) / `%LOCALAPPDATA%` + `data\` subdir (Windows) / Application Support + `data/` subdir (macOS) | **this doc §5** |

Secrets are out of scope here — see `working-with-secrets.md`. The defining
distinctions this doc rests on:

- **Config is user-facing** — authored, edited, sometimes templated by an org;
  the user puts it there, and `config clear --all` resets it.
- **Cache is derived and safe to delete at any instant** — a value fetched to
  avoid re-fetching belongs here; loss is tolerated by definition.
- **Data is program-facing** — bytes the program writes and reads while it
  runs; the user shouldn't poke at it directly. Not config (user didn't author
  it), not cache (loss is not tolerated). Run ledgers, persisted artifacts,
  local indexes — see §5.

They never share a directory. **Tiebreaker for the fuzzy cache/data line:
default to cache.** Loss-tolerated is the safer error than disk-cruft
accretion.

The one-line standard:

> Config → `os.UserConfigDir()/<tool>` · Cache → `os.UserCacheDir()/<tool>` · Data → `XDG_STATE_HOME/<tool>` (Linux) / `%LOCALAPPDATA%\<tool>\data` (Windows) / `~/Library/Application Support/<tool>/data/` (macOS) · Secrets → OS keyring
>
> **Use the Go stdlib helper where one exists. No hand-rolled
> *current/target* path resolution** (a CLI's bespoke *legacy-source*
> probing is exempt — §3/§6a). The helpers honor `$XDG_*` on Linux and
> return the OS-native dir on macOS/Windows — that *is* the standard. Data
> has no Go helper; the `cli-common` resolver derives it (§5.2). Decided
> 2026-05-19 (§8); data added 2026-05-28.

### 1.1 Platform mapping (Go stdlib)

Go ships only three of these helpers — there is **no** `UserDataDir` /
`UserStateDir`:

| XDG var | Go helper | Linux | macOS | Windows |
|---|---|---|---|---|
| `XDG_CONFIG_HOME` | `os.UserConfigDir()` | `$XDG_CONFIG_HOME` ⇒ `~/.config` | `~/Library/Application Support` | `%APPDATA%` (Roaming) |
| `XDG_CACHE_HOME` | `os.UserCacheDir()` | `$XDG_CACHE_HOME` ⇒ `~/.cache` | `~/Library/Caches` | `%LOCALAPPDATA%` |
| `XDG_DATA_HOME` | — *(none)* | `~/.local/share` | `~/Library/Application Support` † | `%APPDATA%` † |
| `XDG_STATE_HOME` | — *(none)* | `~/.local/state` | — † | `%LOCALAPPDATA%` † |
| *(home)* | `os.UserHomeDir()` | `~` | `~` | `%USERPROFILE%` |

† convention only — no Go helper; derive it yourself.

**The standard is: call the Go helper, take whatever it returns.** Native per
OS, no hand-rolled *current/target* resolution (bespoke *legacy-source*
probing stays exempt — §3/§6a). On Linux the helpers honor `$XDG_*` (so power
users' overrides still work); on macOS/Windows they return the OS-native dir.

This *changes current behavior for config* — but the starting point is **not
uniform**, so the claim is scoped precisely:

- **Current non-secret config stores in `slck`, `nrq`, `gro`, and `sfdc`**
  hand-roll `$XDG_CONFIG_HOME else ~/.config/<tool>` on all OSes (the
  deliberate "no `%APPDATA%` branch").
- **The shared Atlassian config.yml path** (`atlassian-cli/shared/credstore/
  credstore.go:72`) also hand-rolls `$XDG_CONFIG_HOME else ~/.config/
  atlassian-cli`.
- **jtk's *legacy* per-tool config** already uses `os.UserConfigDir()`
  (`atlassian-cli/tools/jtk/internal/config/config.go:71`) — it is *not* a
  hand-roller; the migration must treat it as a distinct legacy source, not
  assume a `~/.config` origin.

Under this standard, the hand-rolling stores adopt `os.UserConfigDir()`:

- **Linux:** effectively no change (`os.UserConfigDir()` ≡ the hand-rolled
  result) — **except** a *relative* `$XDG_CONFIG_HOME`: the hand-rolls accept
  it, `os.UserConfigDir()` returns an error. This is an **intentional
  tightening** (relative XDG is non-conformant per the XDG spec); document it,
  don't paper over it.
- **macOS:** config moves `~/.config/<tool>` → `~/Library/Application Support/<tool>`.
- **Windows:** config moves `~/.config/<tool>` → `%APPDATA%\<tool>`.

**Cache is *not* uniformly conformant** — the accurate status: `gro`'s cache
root already calls `os.UserCacheDir()` (conformant location, though its writes
are non-atomic and its TTL is still user-configurable — §4); `slck`/`nrq`/
`sfdc`/`cfl` have **no disk cache**; `jtk` is the **only existing disk-cache
outlier** (`~/.jtk/cache`). Remaining work is broader than just relocation —
per §2/§7 it is, across the units: (1) one-time silent B2a/B2b-style config
relocation on macOS/Windows for the hand-rolling stores; (2) the same
adoption in `atlassian-cli/shared/credstore` (**not** `cli-common/credstore`
— see §7.5), as one combined cfl+jtk unit; (3) config writes → atomic + dirs
`0700`/files `0600` wherever not already (slck/sfdc/gro + legacy cfl pkg);
(4) the jtk cache re-migration; and (5) gro cache → atomic writes, hard-coded
TTL, and removal of `gro config cache ttl|show|clear`. Rollout in §7;
decisions in §8.

**Data resolution (the fourth pillar)** is not provided by a Go stdlib
helper — the `cli-common` resolver derives it per platform (§5.2):

- **Linux:** `$XDG_STATE_HOME` if set (absolute), else `~/.local/state/<dir>`.
  Not `XDG_DATA_HOME`: the use case is working/running state, which matches
  XDG STATE's spec (high-churn, not backup-targeted), not XDG DATA's
  (portable user data).
- **macOS:** `~/Library/Application Support/<dir>/data/` — Apple's
  Application Support is the catch-all root for both config and data; the
  `data/` subdir disambiguates pillars within the tool's subtree.
- **Windows:** `%LOCALAPPDATA%\<dir>\data` — explicitly **not** `%APPDATA%`
  (Roaming). Roaming would sync the data dir (SQLite, logs, agent outputs)
  over the network for users on roaming profiles; LocalAppData is the
  machine-local, non-roaming home that fits the working-state use case. The
  `data\` subdir disambiguates data from cache inside the LocalAppData tool
  subtree.

No existing CLI holds data today — the pillar is greenfield, additive to
the rollout (§7).

---

## 2. Conformance status (current vs target)

No CLI is fully conformant. Per-surface, with the concrete gap:

| CLI | Config dir | Config write | Config perms | Disk cache | Net action |
|-----|-----------|--------------|--------------|-----------|-----------|
| slck | hand-rolled `~/.config` | plain `os.WriteFile` | check | none | resolver + atomic + perms |
| nrq | hand-rolled `~/.config` | atomic (✅) | check | none | resolver |
| sfdc | hand-rolled `~/.config` (`config.json`¹) | plain `os.WriteFile` | check | none | resolver + atomic + perms *(parked)* |
| cfl | **2 surfaces:** shared `~/.config/atlassian-cli` (shared credstore) **+** legacy cfl pkg `~/.config/cfl` | shared: **atomic ✅**; legacy cfl pkg: plain `os.WriteFile` | shared: check; legacy cfl pkg dirs **`0750`→`0700`** | none | shared-config resolver (w/ jtk) + legacy-cfl-pkg atomic+perms |
| jtk | **2 surfaces:** legacy jtk pkg already `os.UserConfigDir()` **+** shared `~/.config/atlassian-cli` (shared credstore, atomic ✅) | shared: atomic ✅; legacy jtk pkg: check | check | ⚠️ `~/.jtk/cache/<instance>/` | shared-config resolver (w/ cfl) + **cache re-migrate (independent, first)** |
| gro | hand-rolled `~/.config/google-readonly` | plain `os.WriteFile` | check | ✅ loc `os.UserCacheDir()` but **non-atomic + user-TTL + `config cache` cmds** | resolver + atomic config + cache: atomic + drop TTL/`config cache *` |

¹ sfdc still `config.json` only because it is the parked Phase-B unit.
"check" = verify against the §3 `0700`/`0600` rule during the port; do not
assume conformant.

**Data:** no existing CLI holds program-managed data per §5; the table
omits a data column for brevity. Add one when the first port (e.g. `cr`)
introduces a data dir.

> **Corrections vs. the earlier draft (Codex-verified):** (1) gro is **not**
> "no action — B2b was correct": only its cache *location* is conformant; its
> config still hand-rolls, its cache writes are non-atomic, its TTL is still
> user-configurable, and `gro config cache ttl|show|clear` must be removed.
> (2) "resolver switch only" for cache-less CLIs is wrong — adopting the
> resolver is the trigger to also bring config writes to atomic + dirs to
> `0700` where they aren't already (slck/sfdc/gro and the *legacy cfl
> config package* use plain `os.WriteFile`; legacy cfl dirs `0750`). (3) cfl
> and jtk each have **two config surfaces**: the *shared* atlassian-cli
> config.yml (written atomically by the shared credstore) and a *legacy
> per-tool config package*; the shared surface is one combined cfl+jtk
> resolver unit (§7.4), the legacy packages are per-tool. jtk's legacy pkg
> already uses `os.UserConfigDir()`. **Secrets** (keyring) are
> location-independent and out of scope.

---

## 3. Config (durable state)

- **Resolution:** `os.UserConfigDir()/<dir>`, obtained from the shared
  `cli-common` state resolver (§6a). The resolver owns the *base-dir + naming
  policy + create/no-create split*; it is **not** a blanket "no file may ever
  call `os.User*Dir()`" ban — a CLI's bespoke legacy-source detection (e.g.
  probing an old `~/.config` path that the helper would never return)
  legitimately still computes its own paths. The hand-rolled non-secret
  stores (`slack-chat-api/internal/config`, `salesforce-cli/internal/config`,
  `google-readonly/internal/config`) and the shared
  `atlassian-cli/shared/credstore/credstore.go:72` config.yml path are the
  **anti-pattern to replace** for the *current* path; legacy detection is a
  separate, intentionally per-CLI concern.
- **`<dir>` naming rule (DECIDED §8):** keyed to **credential scope, not the
  binary**. A repo whose binaries share one credential bundle shares one
  config dir: atlassian-cli ⇒ `os.UserConfigDir()/atlassian-cli` (one dir, one
  `config.yml`, one keyring bundle — matches the B3 design). Single-binary
  repos ⇒ the tool name. (Cache differs — per-binary, see §4.1.)
- **macOS/Windows migration:** adopting the helper relocates the config dir on
  those OSes. One-time, silent, non-fatal, **bespoke per unit / credential
  scope** (§7.4 — matched to that scope's *actual* current on-disk reality,
  which is **not** uniformly `~/.config`: jtk legacy is already
  `os.UserConfigDir()`, cfl legacy is `~/.config/cfl`, shared Atlassian is
  `~/.config/atlassian-cli`), invisible to the user; fail loud only on a
  genuine read/decode conflict, never
  precedence-pick. **No port merges without satisfying §3.2.**
- **File:** `config.yml` (sfdc is legacy `config.json`, parked). Non-secret;
  safe for an org to commit a template.
- **Permissions:** dir `0700`, file `0600`.
- **Atomic write:** temp-file-in-same-dir + rename. Reference:
  `newrelic-cli/internal/config/config.go` Save().
- **Legacy migration:** read old format transparently once, promote, leave a
  one-line notice. Fail loud (never precedence-pick) if multiple legacy sources
  diverge. Cross-ref `working-with-secrets.md` §1.8 for the secret half.
- **What lives here:** `credential_ref`, non-secret connection fields, per-tool
  defaults. **Never** a TTL, never derived data, never a secret.

### 3.1 Test isolation (load-bearing — not cleanup)

Switching to `os.UserConfigDir()` **breaks the family's current test
isolation on macOS.** Today tests isolate by setting `XDG_CONFIG_HOME`. But
`os.UserConfigDir()` derives from `$HOME/Library/Application Support` on macOS
and `%AppData%` on Windows — **it does not read `XDG_CONFIG_HOME` there.**
After the switch, any test that does not override `HOME` (and Windows
`AppData`/`LocalAppData`) reads/writes the developer's *real* config dir. This
is the exact non-hermetic-test class that leaked a real credential during B3.

**`HOME` alone is insufficient — especially on Windows.** Go's
`os.UserConfigDir()` reads `%AppData%` and `os.UserCacheDir()` reads
`%LocalAppData%`; those are **not** derived from `%USERPROFILE%`/`HOME` (the
OS sources them from the registry). A `HOME`-only override leaves a real-dir
leak on Windows. The shared hermetic helper MUST point **all** of these at
the test temp dir:

`HOME`, `USERPROFILE`, `AppData`, `LocalAppData`, `XDG_CONFIG_HOME`,
`XDG_CACHE_HOME`, `XDG_DATA_HOME`, and `XDG_STATE_HOME`

(XDG vars included so a developer's exported `$XDG_*` can't bleed into a
Linux test run either). The set grew to 8 vars when the data pillar (§5)
landed with Path A backing on `XDG_STATE_HOME`; both XDG_DATA_HOME and
XDG_STATE_HOME are pinned so either variant in a dev's env can't leak.
Existing per-CLI helpers are incomplete — e.g.
`google-readonly/internal/credtest/credtest.go:29` sets `LOCALAPPDATA` but
not `AppData`. This helper ships **once** in `cli-common` alongside the
resolver (§6a); no CLI re-derives the env-var list.

### 3.2 Migration acceptance matrix (per-port merge gate)

"Bespoke silent migration" is not a verification check. **Before
implementation**, each port must *declare in its PR description* its
durable-data policy — there is no family-wide default because the legacy
layouts differ:

- **copy vs. move** of the old config (does the old path survive?);
- **second-run / re-invocation** behavior (idempotent; never re-migrates a
  user who has since edited the new path);
- **downgrade / fork** behavior (an older binary, or the sibling tool,
  still reading the old path — esp. shared Atlassian config).

Then the port PR must include tests proving each case below against *that
CLI's* real legacy source(s):

| Case | Expected behavior | Verified by |
|------|-------------------|-------------|
| old-only present | migrated per the PR's declared copy/move policy | test |
| new-only present | untouched, no migration attempted | test |
| same value both | idempotent no-op, no error | test |
| **conflicting** old vs new | **fail loud**, name both paths, mutate nothing | test |
| malformed old | fail loud, do not silently discard | test |
| malformed new | fail loud, do not overwrite with old | test |
| neither present | path **resolved, not created**; dir created only on first write/init (per the §6a no-create split) | test |
| **no real-dir writes** | hermetic: test never touches the dev's real dirs | §3.1 helper |

A port that cannot demonstrate all eight rows **and** declare the three
policy points is not merge-ready. jtk's matrix additionally includes "legacy
already `os.UserConfigDir()`" as an old-source variant (not a `~/.config`
origin).

---

## 4. Cache (disposable state)

### 4.1 Location
`os.UserCacheDir()/<tool>` via the shared `cli-common` resolver (Linux
`$XDG_CACHE_HOME`/`~/.cache`, macOS `~/Library/Caches`, Windows
`%LOCALAPPDATA%`). Append only `<tool>` — no platform suffix. Reference:
`google-readonly/internal/config/config.go` `CacheDirPath()` (the B2b
implementation — the conformant shape to lift into `cli-common`).

**Multi-binary repos: cache is PER-BINARY (DECIDED §8).** Unlike config
(credential-scoped, one shared dir), cache is per-tool derived data — jtk
caches Jira resources, cfl would cache Confluence resources; they never share.
So atlassian-cli ⇒ `os.UserCacheDir()/jtk` and `os.UserCacheDir()/cfl`
*separately*, **not** a shared `os.UserCacheDir()/atlassian-cli/{jtk,cfl}`.

### 4.2 On-disk envelope
Every cached resource is a self-describing JSON envelope. Reference:
`atlassian-cli/tools/jtk/internal/cache/envelope.go`.

```
Envelope[T]{ Resource, Instance, FetchedAt, TTL, Version, Data T }
```

- `Version` mismatch ⇒ treated as a cache miss (schema bumps self-heal).
- Reads never check freshness; freshness is a separate concern (§4.4).

### 4.3 Atomic write (mandatory)
Temp file in the **same dir** → write → chmod `0600` → rename. Dir created
`0700`. Reference: `envelope.go` `atomicWriteEnvelope`. **gro currently uses
plain `os.WriteFile`** — adopting this is a real robustness win, not cosmetic.

### 4.4 Freshness & TTL
- TTL is **hard-coded per resource, NOT user-configurable.** Rationale: the
  jtk decision — a TTL knob is config surface nobody tunes correctly; correct
  default + an explicit `refresh` escape hatch beats a setting. gro's
  `config cache ttl` is **removed** under this standard.
- `Classify(fetchedAt, ttl, now) → Fresh|Stale|Uninitialized|Manual|Unavailable`.
  `manual` sentinel = never auto-expire. Reference:
  `atlassian-cli/tools/jtk/internal/cache/freshness.go`.

### 4.5 Invalidation
- `Touch(names...)` — zero `FetchedAt` to mark stale, keep the data bytes.
- `AppendOnCreate` / `RemoveOnDelete` — surgical in-place envelope edits so a
  mutation doesn't force a full refetch.
- Reference: `atlassian-cli/tools/jtk/internal/cache/invalidate.go`.

### 4.6 Command surface (the only user-facing cache controls)
- `<tool> refresh [resources...]` — populate/update; auto-expands declared
  dependencies in order.
- `<tool> refresh --status` — freshness + age, no network.
- Clearing folds into `<tool> config clear --all` (no dedicated cache cmd, no
  `config cache show`). Reference: jtk `internal/cmd/refresh/refresh.go`.

---

## 5. Data (program-managed state)

The fourth pillar — bytes the program owns the lifecycle of. The lifecycle
invariants are:

- **The dir as a whole survives `config clear --all`.** Pillars have
  separate lifecycles; a config-scoped reset never reaches into data.
- **Whole-dir nuke is explicit and user-invoked** (the `purge` verb;
  §5.5). Not triggered by uninstall, not folded into other resets.
- **Individual records / artifacts inside the dir may be removed by the
  program** under a documented retention policy (§5.6). Automatic
  enforcement at write-time is preferred over user-driven prune.

Config is *user*-facing (authored, edited, sometimes templated); data is
*program*-facing — the program writes, reads, mutates; the user shouldn't
poke at it directly.

### 5.1 What belongs here

Examples: a run ledger (SQLite of every invocation), persisted artifacts
kept past a run (findings JSON, log streams, agent outputs), build
histories, local indexes, downloaded large assets the user benefits from
re-using across runs. Anything that:

- the program reads and writes during normal operation, AND
- the user did not author and would not edit by hand, AND
- loss on `config clear --all` would surprise the user.

Negative definition: **not config** (user didn't author it), **not secret**
(no credential), **not cache** (not safe to delete).

**Tiebreaker: when a maintainer is genuinely on the fence between cache and
data, default to cache.** Loss-tolerated is the safer error than
accreted-on-disk-forever. Drift toward data is the cost we accept for
having a fourth pillar; the tiebreaker minimizes it.

**XDG DATA + STATE one-pillar collapse — but backed by STATE.** XDG
splits `XDG_DATA_HOME` (portable user data, backup-targeted) from
`XDG_STATE_HOME` (logs / runtime / recently-used / high-churn working
state). macOS doesn't distinguish at all (Application Support is the
catch-all); Windows distinguishes by roaming-vs-local (`%APPDATA%` vs
`%LOCALAPPDATA%`) but not by data-vs-state. We honor the OS-level
collapse where it exists and pick the spec-correct half where it doesn't:
on Linux we back the data pillar with `XDG_STATE_HOME` (not
`XDG_DATA_HOME`) because the use case — run ledgers, persisted artifacts,
logs, working state — is XDG STATE's spec, not XDG DATA's. No fifth pillar.

### 5.2 Location

Go's stdlib provides no `UserDataDir` helper — derive it via the shared
`cli-common` resolver (§6a):

- **Linux:** `$XDG_STATE_HOME/<dir>` if set; else `~/.local/state/<dir>`.
  **A relative `$XDG_STATE_HOME` is non-conformant per the XDG spec and
  returns an error** — same intentional tightening as `XDG_CONFIG_HOME` /
  `XDG_CACHE_HOME` (§1.1). No silent fallback on relative values: the
  resolver mirrors the stdlib helpers' behavior so a misconfigured dev env
  surfaces loudly. *Not* `XDG_DATA_HOME` — see §5.1 for the rationale
  (working state matches STATE's spec, not DATA's).
- **macOS:** `~/Library/Application Support/<dir>/data/`. Application
  Support is the catch-all root for both config and data on macOS; the
  `data/` subdir disambiguates pillars within the tool's subtree. (macOS
  has no STATE analog; Application Support is the pragmatic home.)
- **Windows:** `%LOCALAPPDATA%\<dir>\data`. Explicitly **not** `%APPDATA%`
  (Roaming). Roaming would sync the data dir over the network for users
  on roaming profiles — disastrous for SQLite, logs, agent outputs,
  large artifacts. `%LOCALAPPDATA%` is the machine-local, non-roaming
  home that fits the working-state use case. The `data\` subdir is needed
  because cache also resolves under LocalAppData.

**Naming rule: data is per-binary** — same as cache (§4.1), not config.
Derived program-managed state has program-specific lifecycle; jtk's run
ledger ≠ cfl's run ledger even if they share credentials. So a
shared-credential family like atlassian-cli would have separate `…/jtk`
and `…/cfl` data dirs, not a shared `…/atlassian-cli/{jtk,cfl}`. The
config rule (credential-scoped) is the wrong analog because data isn't
owned by the credential bundle.

The resolver owns the create-vs-no-create split the same way it does for
config and cache: resolution is mkdir-free; the dir is created lazily on
first write.

### 5.3 Test isolation

The Path A backing shift (Linux → `XDG_STATE_HOME`) **grows the
`statedirtest.Hermetic` helper from 7 to 8 env vars**: `XDG_STATE_HOME`
joins the existing set (`HOME`, `USERPROFILE`, `AppData`, `LocalAppData`,
`XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`). `XDG_DATA_HOME`
stays in the set so an XDG-aware dev env that exports either variant
can't bleed into a Linux test run. macOS/Windows roots chain through
`HOME`/`USERPROFILE`/`AppData`/`LocalAppData` — already overridden, no
change needed there. **Env coverage is ready ahead of the resolver API:**
the helper pins the vars `Data()` will read (§7 rollout step 7), so once
the resolver lands its data path it's hermetic from day one — the helper
itself is not gated on the resolver method existing.

### 5.4 Format invariants

- **Permissions:** dir `0700`, file `0600` — same as config and cache.
- **Atomic write: NOT mandated.** Data is open-ended in shape — SQLite
  (with its own WAL/journal durability), append-only logs, streamed
  artifacts, many small files — and a blanket temp-rename rule fits none
  of them well. Reach for atomic writes when the format is a single
  self-contained artifact that must transition atomically; do not force
  it on the database engine or on stream writers.
- **Schema migration: fail loud + migrate, never silent self-heal.**
  Cache's "version mismatch = miss" trick (§4.2) works because cache loss
  is tolerated; data loss is not. A self-updating application is on the
  hook to migrate its on-disk data forward at startup — this is
  industry-norm for any program that owns durable state, but it bears
  saying once here so the contrast with cache is explicit. Store the
  schema version, detect mismatch, migrate or refuse.

### 5.5 Command surface

The user MUST have a discoverable path to remove data. Without one, the
pillar accretes orphaned MB on the user's disk three years after uninstall.
Two operations the CLI exposes:

**Nuclear (required):** wipe the data dir entirely; drop any database
artifacts. Idempotent. **Must not depend on the data being readable** — if
the SQLite is corrupt or schema-incompatible, nuclear must still scrub.
This is the §7.6 cleanup-command recovery contract (MON-5372 / MON-5373)
applied to data: the cleanup verb is the user's escape hatch *from* the
broken state it exists to wipe; it cannot itself require `Load`-grade
health.

**Maintenance (optional, per-CLI shape):** selective trim — `--older-than`,
`--keep-last N`, project-scoped pruning, etc. The doc does not standardize
the shape; each CLI knows what its own state looks like and how a user
would want to trim it.

**Suggested verbs (not mandated):**

- **Nuclear: `<tool> data purge`.** Alternatives: `destroy` (Terraform-
  native), `wipe`, `drop` (SQL-native if storage is a database). Avoid
  `reset` — implies recoverable, wrong signal for nuclear.
- **Maintenance: `<tool> data prune`.** Alternatives: `trim`, `compact`,
  `vacuum` (SQLite-native), `gc` (programmer-y). Avoid `clean` —
  ambiguous between trim and nuke.

`purge` + `prune` is the recommended pair: Unix-ecosystem-native (`apt
purge`, `git prune`, `docker prune`), severity encoded in the verb itself,
easy to remember together.

**Severity belongs in the verb, not a flag.** `config clear --all` is the
precedent the family inherits for the config pillar, but new data subtrees
should not repeat the pattern. `purge` vs. `prune` reads more clearly than
`clear --all` vs. `clear` and resists accidental nuclear invocation.

**Sub-conventions:**

- Nuclear prompts for confirmation by default; `--force` opts out
  (long-only) — the family-wide safety-confirmation skip per
  `command-surface.md` §3.1. `--yes`/`-y` are not used (amended
  2026-06-11: the terraform/kubectl-style `--yes` was considered and
  rejected — one skip spelling across the family beats matching
  external precedent).
- **Both nuclear and maintenance verbs support `--dry-run`.** Nuclear's
  dry-run reports the exact paths that would be scrubbed (data dir,
  database artifacts) without removing anything; maintenance's dry-run
  reports the selection that would be acted on. Nuclear is the
  highest-risk verb in this pillar — preview is not optional.

**Scope separation from `config clear --all`:** config verbs are
config-scope only. `config clear --all` MUST NOT reach into data — no
`--purge-data` flag, no opt-in cross-pillar coupling. The pillars have
separate verbs because they have separate lifecycles; users can compose
them at the shell if they want a full reset.

**Nuclear is user-invoked, not uninstall-triggered.** A package-manager
uninstall does not call nuclear; the user must invoke it explicitly.
Otherwise we recreate the cache failure mode in reverse — losing things by
accident.

### 5.6 Retention (guidance)

The data pillar can grow unboundedly if a CLI persists one row per run, one
file per artifact, or log streams kept past the run. Codex flagged this
during the data-pillar pressure-test: without retention, agent outputs
and log streams become a disk-and-privacy problem. The doc cannot enforce
a single retention shape — what counts as "old" is per-CLI — but it can
hand authors the menu and a tiebreaker.

**Shapes of retention to consider** (compose freely; one of these is
usually enough):

- **Size cap.** "Keep the data dir under N MB; evict oldest on overflow."
  Best for blob-style artifacts where total bytes is the user's pain.
- **Age cap.** "Drop rows / files older than D days." Best for ledgers
  where staleness is the right axis.
- **Count cap.** "Keep the last N runs." Best for ledgers with a natural
  unit of work (a run, a build, a review).

**Enforcement timing** — automatic-at-write beats manual-prune; users do
not manually prune. If the CLI ships a `prune` verb, the automatic enforcer
is still doing 95% of the work and `prune` is the user's explicit override.

**Defaults matter.** Generous-but-finite is better than generous-and-
unbounded. A user who wants to keep more state can raise the cap; a user
who never runs `prune` is silently protected.

This is guidance, not a mandate — the CLI developer is on the hook to
actually implement it. But "we didn't think about retention" is the same
failure mode as "we didn't think about TTL" was for cache (§4.4), and the
fix for cache was the same shape: pick a sensible default, hard-code it,
escape hatch for explicit override. Don't ship a CLI whose data dir grows
forever without thinking about how it stops.

### 5.7 What lives here vs. what doesn't

Lives here: run ledgers, persisted run artifacts (kept past the run), local
indexes, build histories, downloaded large assets the user benefits from
re-using across runs.

Does NOT live here:

- credentials / tokens → secrets (keyring)
- user-edited connection fields, defaults, templates → config
- per-resource freshness-bounded fetches → cache
- anything safe to delete on next run → cache
- transient runtime files that don't outlive the process → `os.MkdirTemp`
  or `t.TempDir`-equivalent, not the data pillar

---

## 6. The `cli-common` state components

`cli-common` gains the **state components** (exact package layout is an
impl detail for the Codex pass; principle-level here):

**(a) Path/dir resolver.** A thin `os.UserConfigDir()+Join` wrapper would
**not** justify a shared component (Codex-flagged: that would be coupling
without payoff). It earns its place only by owning the parts that are
genuinely common policy and easy to get subtly wrong per-CLI:

- the **credential-scope naming rule** (§3) and **per-binary cache rule**
  (§4.1) — one place, not re-derived 6×;
- the **data-dir derivation** (§5.2) — no Go stdlib helper exists, so the
  resolver computes the platform-specific path (`XDG_STATE_HOME` on Linux;
  `%LOCALAPPDATA%` + `data\` subdir on Windows; Application Support +
  `data/` subdir on macOS); one place, not re-derived per-CLI;
- the **create vs. no-create split** (a resolver that mkdirs is wrong for
  dry-run / `config clear --all` paths — gro already learned this in B2b);
- the **§3.1 hermetic test helper** (the full 8-var env set after the Data
  pillar's Path A backing — the highest leak-risk item, must not be
  re-derived per CLI; covers all four pillars — §5.3);
- a documented **migration-source enumeration** seam so each CLI's bespoke
  legacy detection plugs in *without* the resolver itself trying to be
  generic about legacy layouts.

It does **not** ban a CLI from calling `os.User*Dir()` for its own
legacy-probe paths (§3). Scope = *the current/target path + policy + test
isolation*, not *all path computation everywhere*.

**(b) Cache library — two tiers.** Extract tier 1 now; **tier 2 is deferred,
not designed, until cfl forces the question (rule of three).**

- **Tier 1 — universal core (extract now):** `Envelope[T]` +
  `ReadResource[T]`/`WriteResource[T]`; atomic temp-file-rename write;
  version-mismatch-as-miss; freshness `Classify`/`Age`/`Status`; cache-path
  resolution from an **injected** `Locator{ Root, InstanceKey }` (the resolver
  in (a) supplies `Root`; `InstanceKey` = jtk hostname / gro constant /
  single-instance `"default"`). The cache lib is directory-agnostic — it
  receives `Root`, never derives it.
- **Tier 2 — domain layer (deferred):** resource registry, dependency DAG,
  fetchers, `refresh` cobra wiring, instance-key derivation. jtk has all of
  it; gro needs ~none. Extracting from one consumer would just relocate jtk's
  code. Let it crystallize across jtk → gro → cfl; then decide *shared lib vs.
  documented copied pattern*. **Tier-2 promotion criteria (record now):** ≥2
  consumers need the same registry/DAG shape AND the API has been stable
  across one full port cycle without per-consumer special-casing.
  **INT-310 close-out (MON-5375, 2026-05-20): continue deferral.** Only jtk
  needs the tier-2 registry/DAG/fetcher/refresh shape today — gro uses
  tier-1 primitives only, and cfl has no cache. Promoting from a single
  consumer would just relocate jtk code without surfacing a shared
  abstraction. Re-evaluate when cfl gains a cache, or any second
  tier-2-shape consumer (registry/DAG/fetcher/refresh) appears.

> **The commons API co-evolves during the ports — under a hard guardrail.**
> Each port may surface a constraint that generalizes (a) or tier 1; the jtk
> retrofit is the *first generalization driver*, not a one-shot gate. But
> "not frozen" must not silently break an already-ported CLI. **Rule
> (Codex-required):** after the first CLI is ported, any change to an
> exported resolver/cache symbol is **either** purely additive (no existing
> caller changes behavior) **or** rides a **coordinated release train** — a
> tracked set of consumer PRs (one per already-ported CLI), each green
> against the *candidate `cli-common` SHA*, merged together with the repin.
> cli-common and the CLIs are separate repos/modules, so a literal single
> PR is impossible — *the train, not one PR, is the unit.* `go.mod` pins
> prevent *silent* breakage only until the next repin, so the cli-common
> semver tag (INT-310) **MUST NOT be cut until that whole consumer matrix is
> green against the candidate SHA.** No tag on a co-evolving API without it.

---

## 7. Rollout — LOCKED (decided 2026-05-19)

**Model: commons-first, then iterative port one *unit* at a time with a
bespoke invisible migration; the commons generalizes as constraints
surface.** A **unit is a credential scope, not a binary** (§7.4): it may be
one single-binary CLI, one shared-credential scope spanning multiple
binaries (the Atlassian shared config = cfl+jtk together), or one cache-only
surface (the jtk cache re-migration, independent). Within a unit the
resolver switch, config atomic/perms, and any cache adoption are the *same
act* — done together (not two horizontal sweeps).

1. **Build the `cli-common` state components first** (§6a resolver + §6a
   hermetic test helper + §6b tier-1 cache core). Nothing ports until this
   exists. **DELIVERED 2026-05-19 (MON-5364):** `cli-common/statedir`
   (`Scope`/`Cache` resolver, create-vs-no-create split, `LegacySource`
   seam), `cli-common/statedirtest` (the `Hermetic` helper — 7-var at
   delivery; grew to 8 on 2026-05-28 when the Data pillar's Path A backing
   added `XDG_STATE_HOME`), and
   `cli-common/cache` (directory-agnostic `Envelope[T]`,
   `ReadResource[T]`/`WriteResource[T]`, atomic write,
   version-mismatch-as-miss,
   `Classify`/`Age`/`Status`, injected `Locator`). No CLI ported yet; no
   INT-310 tag cut (the §6 release-train guardrail is unaffected).
2. **Port one unit at a time** (unit per §7.4 = a CLI / a credential scope /
   a cache-only surface). A unit is *one PR* but **decomposed into separate,
   independently-reviewable commits, each with its own acceptance
   checklist** — because the surfaces are unrelated and "bundled" otherwise
   hides scope (Codex-flagged: gro's "cache unchanged" actually masks a
   config relocation **+** cache-envelope rewrite **+** atomic-write change
   **+** removal of `gro config cache ttl|show|clear`). Per-surface commits:
   (i) resolver adoption + §3.2 migration matrix; (ii) config write → atomic
   + perms `0700`/`0600`; (iii) cache core adoption (if it caches) + cache
   re-migration; (iv) command-surface removals (e.g. `config cache *`). Any
   surface whose diff is too large to review beside the others **splits to
   its own PR**. Reviewability is not traded for the migration-safety win —
   both are required.
3. **Generalize the commons as you go — under the §6 guardrail** (additive,
   or a coordinated release train with every ported consumer green against
   the candidate SHA; no tag without that matrix). Don't special-case a CLI
   to dodge a real API gap.
4. **Order — a "unit" is a credential scope, not a binary** (Codex Blocker):
   - **jtk cache re-migration** can go **first and independently** — it
     touches only `~/.jtk/cache` → `os.UserCacheDir()/jtk`, not the shared
     Atlassian config path; it is also the first generalization driver.
   - **The Atlassian shared-config resolver adoption is a single combined
     cfl+jtk unit — it CANNOT be jtk-only.** `atlassian-cli/shared/credstore`
     `DefaultPath()` is called by *both* jtk
     (`tools/jtk/internal/config/config.go:24`) and cfl
     (`tools/cfl/internal/config/config.go:230`, `init.go:98`). Switching it
     for jtk silently relocates cfl's config too — so cfl would migrate
     **without its §3.2 matrix**. The shared resolver port carries *both*
     binaries' §3.2 matrices in one unit.
   - **gro**, **slck**, **nrq**, **sfdc** are each independent single-scope
     units (own config dir).
   - Suggested sequence: jtk-cache → Atlassian-shared-config(cfl+jtk) → gro
     → slck → nrq → sfdc; cfl greenfield cache is the third *cache* consumer
     / tier-2 decision point whenever cfl gains a cache.
   - **No unit is "resolver only"**: every unit also brings that scope's
     config writes to atomic + dirs to `0700`/files `0600`
     (slck/sfdc/gro and the legacy cfl config package use plain
     `os.WriteFile`; legacy cfl dirs `0750`). nrq already writes atomically
     and the shared Atlassian credstore write is already atomic — *verify
     per surface, don't assume* (see §2).
5. **`atlassian-cli/shared/credstore` adopts the resolver — NOT
   `cli-common/credstore`.** Correction (Codex Blocker): `cli-common/
   credstore` does **not** own a `config.yml` path; it receives
   config-derived backend values from the caller (`credstore/store.go:63`).
   The hand-rolled shared `config.yml` path lives in
   `atlassian-cli/shared/credstore/credstore.go:72`. The new resolver is an
   **additive, opt-in `cli-common` package** — adding it is not a behavior
   change to `cli-common/credstore`. atlassian-cli's shared wrapper adopts it
   as part of the cfl/jtk port. The INT-310 tag-before-close must cover the
   new `cli-common` state package + this doc (credstore itself is unchanged
   here); repin consumers per the §6 matrix rule.
6. Finalize this doc from what survived; make the tier-2 call.
   **DONE 2026-05-20 (MON-5375):** see §7.6 below for the retrospective and §6b for the tier-2 call.
7. **Data pillar (§5) is additive / greenfield (2026-05-28).** No existing
   CLI holds data state, so the data pillar contributes zero port-units to
   the rollout above. Net-new CLIs (`cr` is the driver) adopt §5 from
   inception alongside §3/§4/§6. **Resolver support DELIVERED 2026-05-30
   (`a9a6987`):** the `statedir.Data` type with `DataDir()` /
   `DataDirEnsured()` methods implements the §5.2 derivation, shipped ahead
   of the first data-holding CLI. The addition was additive (no existing
   caller changed behavior), so no consumer-matrix repin was required —
   consistent with the §6 release-train guardrail.

### 7.6 INT-310 close-out retrospective (MON-5375, 2026-05-20)

The state workstream shipped 5 of 6 planned port units against the
candidate cli-common SHA `e67b2fc81f9d7072679d8cd77098121ed6f15f47` (no
breaking changes required). With the matrix green per §6, **cli-common
`v0.1.0` is the INT-310 state baseline tag** — first stable for
`statedir` + `statedirtest` + `cache` (tier 1), alongside the
already-shipped `credstore` and both standards docs.

**Ports shipped (all status Deployed):**
| Unit | Repo SHA | Ticket | PR |
|------|----------|--------|-----|
| jtk cache re-migration | atlassian-cli@`59a2fb9` | MON-5369 | #373 |
| Atlassian shared config (cfl+jtk combined) | atlassian-cli@`b867c0e` | MON-5370 | #374 |
| gro (config→statedir + cache→cli-common/cache + drop user-TTL) | google-readonly@`1f472eb` | MON-5371 | #134 |
| slck (resolver + atomic + perms) | slack-chat-api@`2619ba3` | MON-5372 | #165 |
| nrq (resolver + dual-probe credentials file) | newrelic-cli@`128c84d` | MON-5373 | #101 |

**Parked:** MON-5374 sfdc (salesforce-cli#42). sfdc has no cli-common
dependency today, so it does not ride the v0.1.0 train; it will adopt
the tag when un-parked. This is the explicit "resolve or exclude"
disposition the GH issue called for.

**Commons API additions during ports (both additive — Codex-cleared per §6):**
- `cache.WriteEnvelope` (cli-common#20, SHA `2c4a5b8`) — verbatim atomic writer; jtk-facade required it to relocate envelopes byte-for-byte from `~/.jtk/cache`.
- Underscore allowed in `cache` component regex `^[A-Za-z0-9][A-Za-z0-9._\-]*$` (cli-common#21, SHA `e67b2fc`) — jtk's pre-port instance keys contain underscores.

No exported symbol changed behavior across the 5 ports; the API was
stable enough that no train-coordinated repin was needed mid-rollout.

**Reusable port patterns (codified here so future state-touching work
inherits them):**

1. **Mutation-free `DetectXxxRelocation` + gated `ApplyXxxRelocation`**:
   detection runs anywhere (init AND runtime); copy runs only at init's
   pre-write gate. Mixing the two surfaces was the MON-5371 plan-r1
   blocker — kept separate ever since.

2. **`Load` / `LoadForRuntime` split with `cfg != nil` soft-degrade
   contract** (MON-5371): mutating callers (init post-gate, `config
   set`, migrating `keychain.Open`) use strict `Load`; pure readers
   (`config show`, `OpenNoMigrate`, runtime resolution) use
   `LoadForRuntime` which warns-and-soft-degrades ONLY when canonical
   was readable. Malformed canonical under conflict MUST hard-fail —
   warning + defaulting would silently swap `CredentialRef` back to
   the default and mask the corruption.

3. **Unexported `xxxFromNewDir` testable seam** (MON-5372 trap-fix):
   production AND tests call the same path. A parallel test helper
   that re-implements the public seam lets production regress while
   tests still pass — the unexported `loadFromNewDir(newDir)` /
   `loadForRuntimeFromNewDir(newDir)` pair makes that impossible.

4. **Old-only-malformed fails loud BEFORE `CopyNeeded`** (MON-5371):
   if the OLD path is unparseable, `ApplyConfigRelocation` would
   propagate corrupt bytes to the new dir. Validate-then-mark-copy.

5. **Companion-secret-file dual-probe** (MON-5371 gro, MON-5373 nrq):
   when the resolver switch also relocates a secret-bearing companion
   file (gro: `token.json`; nrq: `credentials`), the migrator MUST
   probe BOTH old and new locations — old/new candidate enumeration
   with path-identity dedup. **If `clear --all` (or equivalent cleanup)
   also scrubs that companion file, migrator and cleanup must share
   the SAME candidate-enumeration helper so they cannot drift** —
   nrq's `CredentialFileCandidates()` is the exemplar (consumed by
   both `internal/keychain/migrate.go` discover() and
   `internal/cmd/configcmd/config.go` clear). gro's case is the other
   shape: the token dual-probe lives in the keychain migrator via
   `GetTokenPath()` + `OldHandRolledTokenPath()`, and `config clear
   --all` does NOT consume that helper because gro's `--all` file-scrub
   scope is config + cache only; OAuth token cleanup happens through
   the keychain path, not the legacy `token.json` candidate
   enumeration (token.json is also explicitly excluded from
   `ApplyConfigRelocation` — token lives entirely in the keychain
   layer). Equality model is **format-dependent**: text-key=value
   files (nrq's `credentials`) compare on parsed/effective projection
   (api_key / account_id / region — harmless ordering or trailing-
   newline differences must NOT false-conflict); opaque blobs (gro's
   `token.json`) compare on the trimmed raw serialized value
   (different bytes = different token = conflict). Pick the model
   that matches the file format, not a single family rule.

6. **Full-env-set `statedirtest.Hermetic`** mandatory (7-var at INT-310
   delivery; 8-var after Path A added `XDG_STATE_HOME`): HOME/XDG-only
   test isolation leaks the developer's real `~/Library/Application
   Support` / `%APPDATA%` paths on macOS/Windows. Caught a real-dir
   leak in MON-5370 (cfl+jtk shared test was reading the dev's actual
   bearer config). `t.Parallel`-unsafe — use sequentially.

7. **Cleanup-command recovery contract** (MON-5372/5373): `clear
   --all` (and equivalents) is the user's primary recovery path —
   it must not itself be blocked by the broken state it exists to
   wipe. Resolve canonical + old paths up front WITHOUT calling
   `Load`; store-open is best-effort under `--all` so an unparseable
   canonical / invalid `credential_ref` / invalid `keyring.backend`
   does not stall the file scrub. Pinned by
   `TestConfigClear_All_MalformedConfig_StillScrubsFiles` (nrq).

8. **Init-gate ordering proof** (MON-5372/5373): a gate-vs-migration
   ordering test seeds a legacy artifact (credentials file / token)
   AND asserts the error message contains the gate-specific wrapper.
   Either alone is insufficient — the artifact-untouched check passes
   even if a downstream strict-`Load` rejected; the message-only check
   passes if the gate ran but didn't run BEFORE migration. Both
   assertions together prove ordering.

9. **Material equality compares the user-meaningful default-applied
   projection**: apply defaults on both sides BEFORE comparing (so an
   omitted field that semantically defaults to X compares equal to an
   explicit X). Use `reflect.DeepEqual` on the full default-applied
   struct ONLY when every field is directly comparable AND semantically
   user-meaningful after defaults (slck / nrq). When a field needs
   semantic normalization (gro: `OAuthClientPath` default-old vs
   default-new are equivalent paths; `GrantedScopes` sort order is
   meaningless), build an explicit projection so legitimate
   default-path relocations don't false-conflict. Either way, future
   fields force a deliberate choice; silence is what hides divergence.

10. **Path-identity dedup**: Linux collapses old≡new (statedir ≡
    `$XDG_CONFIG_HOME`); operations on both paths must dedupe so
    they don't double-act. The dedup happens at the candidate-list
    level (`CredentialFileCandidates`, `configPathsForClear`) so
    every consumer naturally inherits it.

These ten patterns are the durable INT-310 deposits. Future
state-touching work (sfdc un-parking, cfl future cache, any new CLI
adopting the resolver) should inherit them by reading this section —
not by rediscovering them per port.

---

## 8. Decisions log

- [x] **OS-philosophy split (§1.1): native everywhere.** The *current/target*
      config & cache path comes from the shared `cli-common` resolver over
      `os.UserConfigDir()`/`os.UserCacheDir()` — no hand-rolled *current-path*
      resolution. (A CLI's bespoke *legacy-source* probing is explicitly
      exempt — §3/§6a.)
- [x] **Path/dir resolution is a `cli-common` component (§6a)** for the
      current/target path + naming policy + create/no-create split + the
      hermetic test helper (7-var at this decision; grew to 8 on 2026-05-28
      — see Data pillar additions below) — *not* a thin `os.User*Dir()` ban.
      Per-CLI legacy probes still compute their own paths (Codex M3: a thin
      wrapper would be coupling without payoff; the component earns its
      place via the policy surface, not by banning stdlib calls).
- [x] **Config dir name (§3): credential-scoped.** Shared-credential repos
      share one dir (atlassian-cli ⇒ `os.UserConfigDir()/atlassian-cli`);
      single-binary repos ⇒ tool name.
- [x] **Multi-binary cache dir (§4.1): per-binary.** `…/jtk` & `…/cfl`
      separately; never a shared `…/atlassian-cli/{jtk,cfl}`.
- [x] **Rollout (§7): commons-first, port one *unit* at a time** (unit =
      a CLI / a credential scope / a cache-only surface, §7.4), commons
      co-evolves under the §6 guardrail. **jtk-cache first** (independent);
      **Atlassian shared config = one combined cfl+jtk unit**. A unit is one
      PR **decomposed into per-surface commits with separate acceptance
      checklists** (split to multiple PRs if review surface demands) — not an
      opaque "bundle". No unit is "resolver only" — each also brings that
      scope's config writes to atomic + perms where not already.
- [x] **Credstore correction (§7.5):** the resolver is adopted by
      `atlassian-cli/shared/credstore`, **not** `cli-common/credstore` (the
      latter owns no config.yml path). New resolver = additive opt-in
      `cli-common` package; INT-310 tag covers it + this doc.
- [x] **Test isolation (§3.1):** shared hermetic helper overrides the full
      7-var set (`HOME`, `USERPROFILE`, `AppData`, `LocalAppData`,
      `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`) — `HOME`-only is a
      Windows real-dir leak. Load-bearing; ships once in `cli-common`.
      *(Grew to 8 vars on 2026-05-28 when the Data pillar's Path A backing
      added `XDG_STATE_HOME` — see Data pillar additions below.)*
- [x] **Tier-2 cache extraction (§6b): deferred** to post-cfl; promotion
      criteria recorded in §6b. **Reaffirmed 2026-05-20 (MON-5375):**
      tier-2 shape has only one consumer (jtk); promotion criteria
      explicitly unsatisfied. Re-evaluate when cfl gains a cache.
- [x] **Doc home:** `cli-common/docs/`, versioned + pinned with the code.
- [x] **Cache location:** `os.UserCacheDir()/<tool>`; jtk re-migrates; gro
      (B2b) stays.
- [x] **Per-port migration acceptance matrix (§3.2):** every port PR proves
      all 8 cases (old/new/same/conflict/malformed×2/neither/no-real-writes)
      against that CLI's real legacy source(s). Gate, not aspiration.
- [x] **`working-with-secrets.md` co-versioning:** YES — move the single
      copy at `~/dev/working-with-secrets.md` into `cli-common/docs/`
      alongside this doc; update the bare-filename reference in
      `cli-common/README.md`. (Verified: only one copy exists; clean move,
      no diverged-copy merge.)

#### Data pillar additions (2026-05-28, Codex Round 6 applied)

The following decisions land §5 Data as a fourth pillar. Round 6 Codex
pressure-test ran 2026-05-28 (1 blocker / 3 majors / 2 minors); all
findings applied. Round 7 confirmation pass recommended before tagging
convergence on §5. Disposition table at the end of this section.

- [x] **Fourth pillar — Data (§5): added.** Program-managed working state;
      the program owns lifecycle, the user shouldn't poke at it directly.
      Defined negatively ("not config, not secret, not cache") with the
      cache/data tiebreaker "default to cache when on the fence." Resolves
      the gap raised in `data-pillar-primer.md` (since deleted — §5 is the
      sole record). Driver: `cr` (codereview)
      needs a SQLite run ledger + persisted artifacts that survive
      `config clear --all`.
- [x] **Data location (§5.2): Path A — STATE-flavored backing.**
      `XDG_STATE_HOME` (Linux) / `%LOCALAPPDATA%\<dir>\data` (Windows) /
      `~/Library/Application Support/<dir>/data/` (macOS). Explicitly
      **not** `XDG_DATA_HOME` on Linux (use case is working state, matches
      STATE's spec) and explicitly **not** `%APPDATA%` on Windows (avoids
      roaming-profile network-sync of SQLite + logs + agent outputs).
      Resolved Codex Round 6 Blocker B1. No Go stdlib helper; `cli-common`
      resolver derives.
- [x] **Data naming rule (§5.2): per-binary.** Same as cache (§4.1), not
      config. jtk's run ledger ≠ cfl's run ledger even if they share
      credentials — derived program-managed state has program-specific
      lifecycle, not credential-scope ownership. Resolved Codex Round 6
      Major M1 (the earlier "deferred until first shared-credential
      data-holding CLI" was contradictory with the inherited config rule).
- [x] **XDG DATA + STATE collapse (§5.1): honored where the OS does it;
      spec-correct half picked where it doesn't.** macOS/Windows collapse
      at the OS level; on Linux we back with `XDG_STATE_HOME` (not
      `XDG_DATA_HOME`) because the use case matches STATE's spec. No
      fifth pillar.
- [x] **Cache/data tiebreaker (§5.1): default to cache.** Loss-tolerated
      is the safer error than disk-cruft accretion. This is the
      drift-prevention mechanism in lieu of a strict positive
      inclusion-criterion (the strict version was rejected as too
      `cr`-shaped).
- [x] **Data format invariants (§5.4): perms `0700`/`0600`; NO
      atomic-write mandate; schema mismatch fail-loud + migrate.** The
      schema clause is the property that distinguishes data from cache at
      the implementation level — cache uses version-mismatch-as-miss
      (§4.2), data cannot. Atomic writes are open-ended because the
      formats are open-ended (SQLite has its own durability; logs/streams
      don't want temp-rename).
- [x] **Data command surface (§5.5): nuclear required, maintenance
      per-CLI.** Nuclear obeys the §7.6 cleanup-command recovery contract
      (must not require readable state). Suggested verb pair `purge`
      (nuclear) / `prune` (maintenance); severity encoded in the verb,
      not a flag. Confirmation prompt + `--force` opt-out for nuclear
      *(amended 2026-06-11: originally `--yes`; aligned to
      `command-surface.md` §3.1's single safety-skip spelling)*.
      **Both nuclear and maintenance verbs support `--dry-run`** —
      nuclear's dry-run reports paths-that-would-be-scrubbed without
      removing. Resolved Codex Round 6 Minor m1 (nuclear is the
      highest-risk verb; preview is not optional).
- [x] **Retention is guidance, not a mandate (§5.6).** If the data dir
      can grow unboundedly in normal use (one row per run, one file per
      artifact, log streams kept past the run), the CLI SHOULD declare a
      retention/size policy and enforce it automatically. Shapes (size
      cap / age cap / count cap) and exact flags are per-CLI; the doc
      lists the menu and the tiebreakers. Generous-but-finite defaults
      beat unbounded ones. Codex Round 6 Major M2 pushed for a MUST;
      softened to SHOULD per user direction — the CLI developer is on
      the hook to actually implement it.
- [x] **Cross-doc update to `working-with-secrets.md` §1.7.2 (Round 6
      Major M3).** Secrets doc now explicitly notes that the data pillar
      is excluded from `config clear --all`'s scope — pillars have
      separate lifecycles by design. Prevents the older "factory reset"
      framing from leaking into implementers.
- [x] **Config / data verb scope (§5.5): strict separation.** `config
      clear --all` is config-scope only; no `--purge-data` flag, no
      cross-pillar coupling. Pillars have separate lifecycles; users
      compose at the shell for a full reset.
- [x] **Nuclear is user-invoked, not uninstall-triggered (§5.5).**
      Package-manager uninstall does not call nuclear; explicit user
      invocation only. Avoids the cache failure mode in reverse (losing
      things by accident).
- [x] **`statedirtest.Hermetic` grows to 8 vars (§5.3).** Path A's Linux
      backing shift (XDG_STATE_HOME) means the helper needed a new
      override; XDG_DATA_HOME stays in the set so either XDG variant
      that a dev env exports gets pinned. The macOS/Windows roots were
      already covered by `AppData`/`LocalAppData`. Helper doc-string
      updated to "all four pillars." Test renamed
      `TestHermetic_IsolatesAll7Vars` → `IsolatesAll8Vars`.
- [x] **Stale code comments updated (Round 6 Minor m2).**
      `statedir/resolver.go` package doc now references §6a (was §5a) and
      mentions data-dir derivation. `statedirtest/statedirtest.go`
      package doc now references all four pillars and explicitly names
      the data-pillar env vars it pins.
- [x] **Doc home unchanged:** `cli-common/docs/`, versioned + pinned
      with the code (the data pillar rides the existing infra).
- [x] **`data-pillar-primer.md` retention:** originally kept as the
      "how this decision was reached" companion; **deleted 2026-06-11**
      now that §5 is converged and the primer's specifics were stale.
      This decisions log is the surviving record.

### Codex pressure-test — disposition (session 019e3fe7, gpt-5.5 xhigh)

Round 1: `blockers=2 majors=6 minors=2 nits=1`. All accepted (Codex
fact-checked against the sibling repos and caught real errors in the draft):

| Finding | Disposition |
|---------|-------------|
| **B1** `cli-common/credstore` owns no config.yml resolver | §1.1/§3/§7.5 corrected → `atlassian-cli/shared/credstore`; resolver is additive opt-in |
| **B2** config move strands data; no acceptance checks | added §3.2 8-case matrix as a per-port merge gate |
| **M1** Windows test isolation under-specified | §3.1/§8 now enumerate the full 7-var set |
| **M2** "not frozen" lacks guardrail | §6 additive-or-all-consumers-green rule; no tag w/o consumer matrix |
| **M3** §6a unjustified as thin wrapper | §6a reframed around naming/create-split/test-helper/migration seam |
| **M4** bundling hides scope (gro) | §7.2 per-surface commits + checklists; split-PR escape hatch |
| **M5** rollout under-scopes config standard | §2/§7.4 cache-less CLIs also get atomic + `0700`/`0600` |
| **M6** §2 gro "none" false | §2 table rebuilt per-surface; gro action corrected |
| **m1** "every CLI hand-rolls" not traceable | §1.1 narrowed; jtk legacy `os.UserConfigDir()` called out |
| **m2** "cache already conformant" too broad | §1.1 precise: gro loc only / 4 none / jtk outlier |
| **n1** Linux relative `$XDG_CONFIG_HOME` | §1.1 documented as intentional tightening |

Round 2 (revised doc re-reviewed): `blockers=1 majors=3 minors=2 nits=1`.
Codex verified all round-1 corrections landed correctly; new findings were
fallout from those corrections — all accepted:

| Finding | Disposition |
|---------|-------------|
| **B** shared Atlassian config used by cfl *and* jtk → jtk-only port silently ports cfl w/o its §3.2 matrix | §7.4 reworked: a "unit" is a credential scope; shared-config is one combined cfl+jtk unit; jtk *cache* re-migration stays independent/first |
| **M** "same PR" across separate repos not mechanical | §6 guardrail → "coordinated release train; no tag until consumer matrix green vs candidate SHA" |
| **M** §3.2 durable-data policy ambiguous | §3.2 now requires each port to *declare* copy-vs-move / second-run / downgrade-fork before impl |
| **M** §3.2 "neither → created" vs create/no-create split | §3.2 row → "resolved, not created; dir on first write/init only" |
| **m** §8 regressed to absolutist wording | §8 bullets 1–2 reworded to match §3/§6 (legacy probes exempt) |
| **m** §2 cfl row blurs shared vs legacy config | §2 cfl & jtk rows split into the 2 real surfaces (shared atomic vs legacy pkg) |
| **n** §3.2 ordered before §3.1 | reordered: §3.1 test isolation, then §3.2 matrix |

Round 3 (re-reviewed): `blockers=0 majors=1 minors=2 nits=1`. Codex
confirmed §7.4 closes the Atlassian cfl-without-matrix gap. Remaining were
stale-text consistency slips from the round-2 rework — all fixed:

| Finding | Disposition |
|---------|-------------|
| **M** §7 lead-in/step 2 still said "per CLI" — contradicts unit model | reworded to "port one *unit* at a time" (CLI / credential scope / cache-only) |
| **m** one-line standard still absolutist | → "no hand-rolled *current/target* path resolution" |
| **m** §1.1 "remaining work" undercounts | expanded to the full 5-item list (relocation + shared + atomic/perms + jtk cache + gro cache/TTL/cmds) |
| **n** title round counter stale | title now tracks rounds applied (kept in sync each round) |

Round 4 (re-reviewed): `blockers=0 majors=1 minors=2 nits=1`. Codex
confirmed §7 lead-in/§7.2/§7.4/§2 align on the unit model; residual
contradictions were §8/§1.1/§3 spots not propagated in round 3 — all fixed:

| Finding | Disposition |
|---------|-------------|
| **M** §8 rollout bullet still "per-CLI / jtk first / One PR per CLI" | reworded to the unit model (jtk-cache first; Atlassian shared = cfl+jtk unit) |
| **m** §1.1 prose "no hand-rolled resolution" unscoped | → "current/target" (legacy probe exempt) |
| **m** §3 migration "bespoke per CLI" | → "per unit / credential scope" (§7.4) |
| **n** disposition table pinned a stale title string | row de-pinned to "tracks rounds applied" |

Round 5 (re-reviewed): **`blockers=0 majors=0 minors=0 nits=0` — CONVERGED.**
Codex re-read the full doc: §1.1/§2/§3/§6/§7/§8 agree on the unit model
(current/target via shared resolver, legacy probes exempt, migration per
unit/credential scope, jtk-cache independent+first, Atlassian shared config
a combined cfl+jtk unit with both §3.2 matrices). No remaining findings.

Round 6 (data-pillar pressure-test, 2026-05-28): `blockers=1 majors=3
minors=2`. Codex read the data-pillar primer first, then diffed §5 against
HEAD. All accepted; corrections applied in this revision:

| Finding | Disposition |
|---------|-------------|
| **B1** Data root conflates portable data, local state, logs, large artifacts — `%APPDATA%` may roam, `XDG_DATA_HOME` is backup-targeted | §1/§1.1/§5.1/§5.2 backing shifted to Path A — Linux `XDG_STATE_HOME`, Windows `%LOCALAPPDATA%` plus a `data\` subdir, macOS Application Support plus a `data/` subdir. §5.1 collapse note rewritten to honor OS-level collapse + pick spec-correct half where OS doesn't (STATE on Linux). |
| **M1** Shared-family data naming contradictory — "inherits config rule" then "deferred" | §5.2 rewrote naming rule → **per-binary** (matches cache §4.1, not config §3). Derived program-managed state has program-specific lifecycle, not credential-scope ownership. |
| **M2** Retention too optional for a pillar that may persist logs / agent outputs | added §5.6 retention guidance — concrete shapes (size/age/count caps), enforcement-timing recommendations, generous-but-finite defaults. Framed as SHOULD per user direction (not MUST), with the explicit note that "we didn't think about retention" is the same failure mode as "we didn't think about TTL" was for cache. |
| **M3** `working-with-secrets.md` §1.7.2 still framed `config clear --all` as broad factory reset | secrets §1.7.2 + §1.10 cross-doc updated — data pillar explicitly excluded; pointer to `working-with-state.md` §5. |
| **m1** Nuclear data purge should support `--dry-run` | §5.5 sub-conventions extended — both nuclear and maintenance verbs support `--dry-run`. Nuclear's dry-run reports paths-that-would-be-scrubbed. |
| **m2** Code comments stale: `statedir/resolver.go:2` refs §5a; `statedirtest/statedirtest.go:3` says "config/cache" | Both package doc comments rewritten to reference §6a and "all four pillars" respectively; resolver doc now also describes data-dir derivation. |

**Fallout from B1 (not in original Codex findings, surfaced during apply):**
Path A's Linux backing shift (`XDG_STATE_HOME`) means the `statedirtest.Hermetic`
helper grows from 7 to 8 env vars — `XDG_STATE_HOME` joins the set. Helper code,
doc-string, test name, and §3.1/§5.3/§6a doc references all updated.

**Round 7 confirmation recommended.** Round 6 was a single pressure-test pass
with corrections; convergence on §5 should be claimed only after a re-review
pass returns `blockers=0 majors=0 minors=0`. The doc's status block reflects
this — §5 is post-Round-6, pre-convergence.

Round 6 cleanup pass (2026-05-28, post-application re-read): Codex re-read
the working tree after Round 6 corrections landed. No remaining blockers in
the fourth-pillar architecture; 5 cleanup items surfaced as fallout from the
Round 6 apply. All accepted:

| Finding | Disposition |
|---------|-------------|
| Data deletion semantics — §5 lead-in "removed only by an explicit user-invoked verb" contradicts §5.6's automatic-at-write retention | §5 lead-in rewritten as three explicit invariants: dir survives `config clear --all` / whole-dir nuke is explicit / individual records may be retention-pruned by the program |
| `working-with-secrets.md` §1.7.2 heading + "machine never having seen the CLI" sticky framing | §1.7.2 renamed to "config + credentials + cache reset"; "machine never having seen the CLI" reframed as "compose `config clear --all` AND `<tool> data purge` for the historical full reset"; §1.10 row #5 already corrected in Round 6 |
| 8-var propagation incomplete — §7 step 1 / §7.6 step 6 / §8 decision rows / `statedirtest.go:3` still said 7-var | All forward-looking text now reads "8-var" or "full env set"; the two §8 decision rows from 2026-05-19 preserve "7-var" as the historical decision with an explicit "(grew to 8 on 2026-05-28)" annotation; package doc updated |
| `statedir/resolver.go` package doc overclaims data-dir support — no `Data()` API exists yet | Doc rewritten as future-tense: explicit "NOT YET IMPLEMENTED" status, points at §7 rollout step 7 as the implementation gate (when the first data-holding CLI lands). Also `LegacySource` doc-string §5a → §6a. *(Since superseded: `statedir.Data` shipped 2026-05-30 in `a9a6987` and the package doc now describes the implemented behavior — see §7 step 7.)* |
| `data-pillar-primer.md` stale enough to mislead — references old XDG_DATA_HOME / %APPDATA% / --purge-data / 7-var helper | Superseded banner at top with bulleted deltas; primer body preserved as "how the decision was reached" companion (user direction to keep it in place) *(since deleted 2026-06-11 — see the §8 retention decision row)* |

Round 7 (re-reviewed, 2026-05-28): **`blockers=0 majors=0 minors=2` — CONVERGED after applying the two minors.**
Codex re-read the working tree after the cleanup pass landed and found no
remaining architecture issues. Two minors applied:

| Finding | Disposition |
|---------|-------------|
| **m1** §5.2 Linux line `$XDG_STATE_HOME` if set (absolute) — parenthetical hint, not explicit policy | §5.2 rewritten: relative `$XDG_STATE_HOME` returns an error, matching the §1.1 tightening for config/cache. Explicit policy, no silent fallback. |
| **m2** `statedirtest` package doc overclaims "resolution for all four pillars" while `Data()` is unimplemented | Doc reframed: env coverage is ready ahead of the resolver API; the helper pins the vars `Data()` will read, so the helper itself is not gated on the resolver method existing. §5.3 reworded the same way. |

**§5 is now Codex-converged.** The fourth pillar has the same convergence
status as the original three (the existing pillars were `0/0/0/0` at
Round 5; the data pillar is `0/0/0` at Round 7 post-minors).
