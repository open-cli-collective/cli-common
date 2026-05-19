# working-with-state.md — decisions locked; Codex-converged (5 rounds)

> Status: **§7 decisions resolved 2026-05-19; Codex architecture
> pressure-test CONVERGED at round 5 (`blockers=0 majors=0 minors=0
> nits=0`) — full disposition in §7.** Companion pillar to
> `working-with-secrets.md` (which also moves into `cli-common/docs/` so both
> pillars co-version). This doc is the source of truth for **non-secret
> on-disk state** across the Open CLI Collective Go CLI family. Homed here
> (`cli-common/docs/`), versioned with the `cli-common` state components
> (path/dir resolver + cache), pinned per-CLI like the credstore API
> (tag-before-close, INT-310).

---

## 1. Scope & the three pillars

A CLI puts exactly three kinds of state on disk/keyring. Each has one owner:

| Pillar | Kind of state | Where | Owning doc |
|--------|---------------|-------|------------|
| **Secrets** | access credentials | OS keyring (`cli-common/credstore`) | `working-with-secrets.md` |
| **Config** | durable, authored, non-secret | `os.UserConfigDir()/<tool>` | **this doc §3** |
| **Cache** | disposable, derived, regenerable | `os.UserCacheDir()/<tool>` | **this doc §4** |

Secrets are out of scope here — see `working-with-secrets.md`. The defining
distinction this doc rests on: **config is authored and must survive; cache is
derived and must be safe to delete at any instant.** A value that the user set
is config. A value fetched from an API to avoid re-fetching is cache. They
never share a directory.

The one-line standard:

> Config → `os.UserConfigDir()/<tool>` · Cache → `os.UserCacheDir()/<tool>` · Secrets → OS keyring
>
> **Use the Go stdlib helper. No hand-rolled *current/target* path
> resolution** (a CLI's bespoke *legacy-source* probing is exempt — §3/§5a).
> The helpers honor `$XDG_*` on Linux and return the OS-native dir on
> macOS/Windows — that *is* the standard. Decided 2026-05-19 (§7).

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
probing stays exempt — §3/§5a). On Linux the helpers honor `$XDG_*` (so power
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
per §2/§6 it is, across the units: (1) one-time silent B2a/B2b-style config
relocation on macOS/Windows for the hand-rolling stores; (2) the same
adoption in `atlassian-cli/shared/credstore` (**not** `cli-common/credstore`
— see §6.5), as one combined cfl+jtk unit; (3) config writes → atomic + dirs
`0700`/files `0600` wherever not already (slck/sfdc/gro + legacy cfl pkg);
(4) the jtk cache re-migration; and (5) gro cache → atomic writes, hard-coded
TTL, and removal of `gro config cache ttl|show|clear`. Rollout in §6;
decisions in §7.

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
> resolver unit (§6.4), the legacy packages are per-tool. jtk's legacy pkg
> already uses `os.UserConfigDir()`. **Secrets** (keyring) are
> location-independent and out of scope.

---

## 3. Config (durable state)

- **Resolution:** `os.UserConfigDir()/<dir>`, obtained from the shared
  `cli-common` state resolver (§5a). The resolver owns the *base-dir + naming
  policy + create/no-create split*; it is **not** a blanket "no file may ever
  call `os.User*Dir()`" ban — a CLI's bespoke legacy-source detection (e.g.
  probing an old `~/.config` path that the helper would never return)
  legitimately still computes its own paths. The hand-rolled non-secret
  stores (`slack-chat-api/internal/config`, `salesforce-cli/internal/config`,
  `google-readonly/internal/config`) and the shared
  `atlassian-cli/shared/credstore/credstore.go:72` config.yml path are the
  **anti-pattern to replace** for the *current* path; legacy detection is a
  separate, intentionally per-CLI concern.
- **`<dir>` naming rule (DECIDED §7):** keyed to **credential scope, not the
  binary**. A repo whose binaries share one credential bundle shares one
  config dir: atlassian-cli ⇒ `os.UserConfigDir()/atlassian-cli` (one dir, one
  `config.yml`, one keyring bundle — matches the B3 design). Single-binary
  repos ⇒ the tool name. (Cache differs — per-binary, see §4.1.)
- **macOS/Windows migration:** adopting the helper relocates the config dir on
  those OSes. One-time, silent, non-fatal, **bespoke per unit / credential
  scope** (§6.4 — matched to that scope's *actual* current on-disk reality,
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
`XDG_CACHE_HOME`, and `XDG_DATA_HOME`

(XDG vars included so a developer's exported `$XDG_*` can't bleed into a
Linux test run either). Existing per-CLI helpers are incomplete — e.g.
`google-readonly/internal/credtest/credtest.go:29` sets `LOCALAPPDATA` but
not `AppData`. This helper ships **once** in `cli-common` alongside the
resolver (§5a); no CLI re-derives the env-var list.

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
| neither present | path **resolved, not created**; dir created only on first write/init (per the §5a no-create split) | test |
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

**Multi-binary repos: cache is PER-BINARY (DECIDED §7).** Unlike config
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

## 5. The `cli-common` state components

`cli-common` gains the **state components** (exact package layout is an
impl detail for the Codex pass; principle-level here):

**(a) Path/dir resolver.** A thin `os.UserConfigDir()+Join` wrapper would
**not** justify a shared component (Codex-flagged: that would be coupling
without payoff). It earns its place only by owning the parts that are
genuinely common policy and easy to get subtly wrong per-CLI:

- the **credential-scope naming rule** (§3) and **per-binary cache rule**
  (§4.1) — one place, not re-derived 6×;
- the **create vs. no-create split** (a resolver that mkdirs is wrong for
  dry-run / `config clear --all` paths — gro already learned this in B2b);
- the **§3.1 hermetic test helper** (the full 7-var env set — the highest
  leak-risk item, must not be re-derived per CLI);
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

## 6. Rollout — LOCKED (decided 2026-05-19)

**Model: commons-first, then iterative port one *unit* at a time with a
bespoke invisible migration; the commons generalizes as constraints
surface.** A **unit is a credential scope, not a binary** (§6.4): it may be
one single-binary CLI, one shared-credential scope spanning multiple
binaries (the Atlassian shared config = cfl+jtk together), or one cache-only
surface (the jtk cache re-migration, independent). Within a unit the
resolver switch, config atomic/perms, and any cache adoption are the *same
act* — done together (not two horizontal sweeps).

1. **Build the `cli-common` state components first** (§5a resolver + §5a
   7-var test helper + §5b tier-1 cache core). Nothing ports until this
   exists. **DELIVERED 2026-05-19 (MON-5364):** `cli-common/statedir`
   (`Scope`/`Cache` resolver, create-vs-no-create split, `LegacySource`
   seam), `cli-common/statedirtest` (the 7-var `Hermetic` helper), and
   `cli-common/cache` (directory-agnostic `Envelope[T]`,
   `Read`/`WriteResource[T]`, atomic write, version-mismatch-as-miss,
   `Classify`/`Age`/`Status`, injected `Locator`). No CLI ported yet; no
   INT-310 tag cut (the §5 release-train guardrail is unaffected).
2. **Port one unit at a time** (unit per §6.4 = a CLI / a credential scope /
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
3. **Generalize the commons as you go — under the §5 guardrail** (additive,
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
   here); repin consumers per the §5 matrix rule.
6. Finalize this doc from what survived; make the tier-2 call.

---

## 7. Decisions log (all resolved 2026-05-19)

- [x] **OS-philosophy split (§1.1): native everywhere.** The *current/target*
      config & cache path comes from the shared `cli-common` resolver over
      `os.UserConfigDir()`/`os.UserCacheDir()` — no hand-rolled *current-path*
      resolution. (A CLI's bespoke *legacy-source* probing is explicitly
      exempt — §3/§5a.)
- [x] **Path/dir resolution is a `cli-common` component (§5a)** for the
      current/target path + naming policy + create/no-create split + the
      7-var test helper — *not* a thin `os.User*Dir()` ban. Per-CLI legacy
      probes still compute their own paths (Codex M3: a thin wrapper would be
      coupling without payoff; the component earns its place via the policy
      surface, not by banning stdlib calls).
- [x] **Config dir name (§3): credential-scoped.** Shared-credential repos
      share one dir (atlassian-cli ⇒ `os.UserConfigDir()/atlassian-cli`);
      single-binary repos ⇒ tool name.
- [x] **Multi-binary cache dir (§4.1): per-binary.** `…/jtk` & `…/cfl`
      separately; never a shared `…/atlassian-cli/{jtk,cfl}`.
- [x] **Rollout (§6): commons-first, port one *unit* at a time** (unit =
      a CLI / a credential scope / a cache-only surface, §6.4), commons
      co-evolves under the §5 guardrail. **jtk-cache first** (independent);
      **Atlassian shared config = one combined cfl+jtk unit**. A unit is one
      PR **decomposed into per-surface commits with separate acceptance
      checklists** (split to multiple PRs if review surface demands) — not an
      opaque "bundle". No unit is "resolver only" — each also brings that
      scope's config writes to atomic + perms where not already.
- [x] **Credstore correction (§6.5):** the resolver is adopted by
      `atlassian-cli/shared/credstore`, **not** `cli-common/credstore` (the
      latter owns no config.yml path). New resolver = additive opt-in
      `cli-common` package; INT-310 tag covers it + this doc.
- [x] **Test isolation (§3.1):** shared hermetic helper overrides the full
      7-var set (`HOME`, `USERPROFILE`, `AppData`, `LocalAppData`,
      `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`) — `HOME`-only is a
      Windows real-dir leak. Load-bearing; ships once in `cli-common`.
- [x] **Tier-2 cache extraction (§5b): deferred** to post-cfl; promotion
      criteria recorded in §5b.
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

### Codex pressure-test — disposition (session 019e3fe7, gpt-5.5 xhigh)

Round 1: `blockers=2 majors=6 minors=2 nits=1`. All accepted (Codex
fact-checked against the sibling repos and caught real errors in the draft):

| Finding | Disposition |
|---------|-------------|
| **B1** `cli-common/credstore` owns no config.yml resolver | §1.1/§3/§6.5 corrected → `atlassian-cli/shared/credstore`; resolver is additive opt-in |
| **B2** config move strands data; no acceptance checks | added §3.2 8-case matrix as a per-port merge gate |
| **M1** Windows test isolation under-specified | §3.1/§7 now enumerate the full 7-var set |
| **M2** "not frozen" lacks guardrail | §5 additive-or-all-consumers-green rule; no tag w/o consumer matrix |
| **M3** §5a unjustified as thin wrapper | §5a reframed around naming/create-split/test-helper/migration seam |
| **M4** bundling hides scope (gro) | §6.2 per-surface commits + checklists; split-PR escape hatch |
| **M5** rollout under-scopes config standard | §2/§6.4 cache-less CLIs also get atomic + `0700`/`0600` |
| **M6** §2 gro "none" false | §2 table rebuilt per-surface; gro action corrected |
| **m1** "every CLI hand-rolls" not traceable | §1.1 narrowed; jtk legacy `os.UserConfigDir()` called out |
| **m2** "cache already conformant" too broad | §1.1 precise: gro loc only / 4 none / jtk outlier |
| **n1** Linux relative `$XDG_CONFIG_HOME` | §1.1 documented as intentional tightening |

Round 2 (revised doc re-reviewed): `blockers=1 majors=3 minors=2 nits=1`.
Codex verified all round-1 corrections landed correctly; new findings were
fallout from those corrections — all accepted:

| Finding | Disposition |
|---------|-------------|
| **B** shared Atlassian config used by cfl *and* jtk → jtk-only port silently ports cfl w/o its §3.2 matrix | §6.4 reworked: a "unit" is a credential scope; shared-config is one combined cfl+jtk unit; jtk *cache* re-migration stays independent/first |
| **M** "same PR" across separate repos not mechanical | §5 guardrail → "coordinated release train; no tag until consumer matrix green vs candidate SHA" |
| **M** §3.2 durable-data policy ambiguous | §3.2 now requires each port to *declare* copy-vs-move / second-run / downgrade-fork before impl |
| **M** §3.2 "neither → created" vs create/no-create split | §3.2 row → "resolved, not created; dir on first write/init only" |
| **m** §7 regressed to absolutist wording | §7 bullets 1–2 reworded to match §3/§5 (legacy probes exempt) |
| **m** §2 cfl row blurs shared vs legacy config | §2 cfl & jtk rows split into the 2 real surfaces (shared atomic vs legacy pkg) |
| **n** §3.2 ordered before §3.1 | reordered: §3.1 test isolation, then §3.2 matrix |

Round 3 (re-reviewed): `blockers=0 majors=1 minors=2 nits=1`. Codex
confirmed §6.4 closes the Atlassian cfl-without-matrix gap. Remaining were
stale-text consistency slips from the round-2 rework — all fixed:

| Finding | Disposition |
|---------|-------------|
| **M** §6 lead-in/step 2 still said "per CLI" — contradicts unit model | reworded to "port one *unit* at a time" (CLI / credential scope / cache-only) |
| **m** one-line standard still absolutist | → "no hand-rolled *current/target* path resolution" |
| **m** §1.1 "remaining work" undercounts | expanded to the full 5-item list (relocation + shared + atomic/perms + jtk cache + gro cache/TTL/cmds) |
| **n** title round counter stale | title now tracks rounds applied (kept in sync each round) |

Round 4 (re-reviewed): `blockers=0 majors=1 minors=2 nits=1`. Codex
confirmed §6 lead-in/§6.2/§6.4/§2 align on the unit model; residual
contradictions were §7/§1.1/§3 spots not propagated in round 3 — all fixed:

| Finding | Disposition |
|---------|-------------|
| **M** §7 rollout bullet still "per-CLI / jtk first / One PR per CLI" | reworded to the unit model (jtk-cache first; Atlassian shared = cfl+jtk unit) |
| **m** §1.1 prose "no hand-rolled resolution" unscoped | → "current/target" (legacy probe exempt) |
| **m** §3 migration "bespoke per CLI" | → "per unit / credential scope" (§6.4) |
| **n** disposition table pinned a stale title string | row de-pinned to "tracks rounds applied" |

Round 5 (re-reviewed): **`blockers=0 majors=0 minors=0 nits=0` — CONVERGED.**
Codex re-read the full doc: §1.1/§2/§3/§5/§6/§7 agree on the unit model
(current/target via shared resolver, legacy probes exempt, migration per
unit/credential scope, jtk-cache independent+first, Atlassian shared config
a combined cfl+jtk unit with both §3.2 matrices). No remaining findings.
