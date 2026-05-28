# TL;DR — working-with-state.md needs a fourth pillar

> **⚠️ Superseded by Round 6 (2026-05-28).** The fourth pillar landed in
> `working-with-state.md` §5; this primer is preserved as the "how the
> decision was reached" companion but its specifics are now stale. Final
> deltas from what's written below:
>
> - **Backing:** Linux `XDG_STATE_HOME` (not `XDG_DATA_HOME`); Windows
>   `%LOCALAPPDATA%` (not `%APPDATA%` — Roaming would sync the data dir
>   over the network). macOS `~/Library/Application Support/<dir>/data/`
>   unchanged. Rationale: working-state use case matches XDG STATE's
>   spec, not XDG DATA's.
> - **`config clear --all`:** strict separation, *no* `--purge-data`
>   flag. Data has its own verb subtree (suggested `<tool> data purge` /
>   `<tool> data prune`).
> - **Hermetic test helper:** grew from 7 to 8 vars (`XDG_STATE_HOME`
>   added) — `cli-common/statedirtest`.
> - **Naming rule:** data is per-binary (matches cache §4.1, not config
>   §3); the "deferred until first shared-credential CLI" hedge is gone.
> - **Retention:** SHOULD declare a policy when the dir can grow
>   unboundedly; concrete shapes (size/age/count caps) in §5.6.
> - **`--dry-run`:** both nuclear and maintenance verbs support it.
>
> Read this for context on the architectural debate; read
> `working-with-state.md` §5 for the actual policy.


## Context

We ship a family of Go CLIs (`jtk`, `cfl`, `gro`, `nrq`, `slck`, `sfdc`) on shared standards docs in `cli-common/docs/`. State has three pillars today:

| Pillar | Where | Rule |
|---|---|---|
| **Secrets** | OS keyring (`cli-common/credstore`) | Access secrets only |
| **Config** | `os.UserConfigDir()/<tool>` | Authored, durable, non-secret |
| **Cache** | `os.UserCacheDir()/<tool>` | Derived, **safe to delete at any moment** |

`config clear --all` wipes config + cache for the active profile. That's correct for everything we ship today.

## The gap

We're designing a new CLI (`codereview` / `cr`) — AI-driven PR reviewer. It needs to persist:

- A **run ledger** (SQLite): every review run, agents used, token cost, duration, model, findings
- **Run artifacts** kept past the run: findings JSON, log streams, agent outputs

This is **derived but not safe to delete**:
- Re-derivable in theory (re-run every review for $$$), not in practice
- Token-cost history, "which agent earns its keep," resume metadata — *real* user value
- `config clear --all` blowing it away violates user expectation

Cache doesn't fit. Config doesn't fit. There isn't a slot.

## Proposal sketch (not committed)

Add a fourth pillar — working name **data** — mirroring XDG semantics:

- **Linux:** `$XDG_DATA_HOME` / `~/.local/share/<tool>`
- **macOS / Windows:** XDG data and config collapse to one root, so use a `data/` subdir under that root (`~/Library/Application Support/<tool>/data`, `%APPDATA%\<tool>\data`)
- Survives `config clear --all`; removed only by a dedicated command (`cr data prune` / `--purge-data` opt-in flag for full uninstall)
- CLI-specific retention policy lives in config (not standards-mandated)
- Hermetic test helper must extend the existing 7-var list

Maps cleanly to existing patterns: tier-1 cache `Envelope[T]` / atomic write / `Locator` indirection from INT-310 transfer directly.

## Things to pressure-test with the architect

1. **Is "data" really a fourth pillar, or a strict-mode cache?** Could we instead change cache's contract to "safe to delete, but may be expensive to recompute" and let CLIs declare retention on cache sub-dirs? Probably worse — muddies the existing "cache = disposable" invariant the other CLIs depend on.
2. **macOS/Windows path collision.** `os.UserConfigDir()` and the natural data dir resolve to the same root. `data/` subdir under the tool dir is the obvious answer; any cleaner option?
3. **Test isolation env-var set.** Adding data resolution changes which env vars need overriding in hermetic tests. Worth folding into `cli-common/statedirtest` in lockstep.
4. **`config clear` scope semantics.** Default narrow scope leaves data alone (already correct). `--all` is the question: does `--all` still mean "active-profile factory reset" but exclude data? Or do we introduce `--purge-data` as an explicit opt-in?
5. **Should the standard mandate retention policy shape?** Or leave it per-CLI? Other CLIs may eventually grow data state too; uniformity could matter.
6. **Conformance status across existing CLIs.** None hold data today, but the §6 rollout matrix and §2 conformance table should at least name the new pillar so future audits don't miss it.

The standards-doc family converged INT-310 via Codex pressure-test (5 rounds, blockers→0). Same process is the right fit here: draft → architect review → Codex convergence → tag in `cli-common`.
