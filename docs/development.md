# Development guide — cli-common

Repo-local facts for working in this repository. Family-wide policy lives in
[`README.md`](README.md) (the standards index) and the standards docs beside
it — do not copy policy prose here.

## What this repo is

The shared-library and standards home for the Open CLI Collective. No binary
ships from here; consumers depend on it as a Go module
(`github.com/open-cli-collective/cli-common`). It follows the **library-repo
profile** in [`repo-layout.md`](repo-layout.md) §2.1.

## Packages

| Package | Implements |
|---|---|
| `credstore` | credential store abstraction — [`working-with-secrets.md`](working-with-secrets.md) §1.3–§1.5, §1.8, §1.12, §2.1 |
| `statedir` | config/cache/data path resolver — [`working-with-state.md`](working-with-state.md) §6a |
| `statedirtest` | hermetic test helper (8-var env override) — [`working-with-state.md`](working-with-state.md) §3.1 / §5.3 |
| `cache` | tier-1 cache core: envelope, atomic write, freshness — [`working-with-state.md`](working-with-state.md) §6b |

## Build / test

```sh
make check   # tidy + lint + test + build — mirrors CI
```

Requires Go per the `go` directive in `go.mod` (the single version source,
`repo-layout.md` §3) and golangci-lint v2.

Tests MUST be hermetic: use the in-memory credstore backend
(`Options.Backend = BackendMemory`) and `statedirtest.Hermetic` (not
`t.Parallel`-safe — use sequentially). Tests never touch the developer's real
OS keychain or home directories; that class of leak is exactly what
`statedirtest` exists to prevent.

## Releases

No auto-release, no `version.txt` (library profile, `repo-layout.md` §2.1).
Semver tags are cut manually. Any exported API **or behavior** change in any
exported package (`credstore`, `statedir`, `statedirtest`, `cache`) is either
purely additive or rides the coordinated consumer release train in
[`working-with-state.md`](working-with-state.md) §6 — no tag until every
ported consumer is green against the candidate SHA.

## Keyring opt-out tags

`byteness/keyring` (≥ v1.11.0) supports per-backend opt-out build tags.
Consumer CLIs that expose 1Password build with `-tags keyring_nopassage`.
Consumer CLIs that intentionally skip 1Password support build with
`-tags keyring_no1password,keyring_nopassage`. CI here tests both sets — see
[`working-with-secrets.md`](working-with-secrets.md) §1.10 for the dependency
trade-off and why `keyring_nofile` / `keyring_nopass` are excluded.
