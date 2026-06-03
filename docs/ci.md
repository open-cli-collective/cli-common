# Working with CI

The Open CLI Collective ships a family of Go CLIs whose repositories should
build, test, and lint the same way. This document is the family-wide standard
for **continuous integration** — the pre-merge gate that runs on every push and
pull request. It does **not** cover cutting a release (that is `release.md`) or
publishing artifacts (that is `distribution.md`).

This is **normative for new CLIs.**

Companion pillars (repo axis):
- `repo-layout.md` — the Go-version source, the Makefile targets CI invokes, and
  the `.golangci.yml` it lints against. **CI consumes these primitives; it does
  not redefine them.**
- `release.md` — conventional commits, version source-of-truth, and tag minting.
  CI stops at "the PR is mergeable"; `release.md` begins at "main moved."
- `distribution.md` — goreleaser and the publish channels, triggered post-tag.

**When this doc appears to conflict with `repo-layout.md`, that wins** for the
shared primitives it owns (Go version, Makefile contract, lint config). See
`docs/README.md` for the full conflict-resolution order.

The seam: **ci = pre-merge gate · release = tag minting · distribution = tag →
artifacts → channels.** Keep CI free of any publishing concern.

---

## §1 Workflow identity and triggers

A single workflow file, `.github/workflows/ci.yml`, named `CI`. It triggers on
exactly two events:

- `push` to `main` (catches anything that lands directly or via merge).
- `pull_request` (the primary gate).

No other triggers belong in `ci.yml`. Tag pushes are `release.yml`'s domain;
scheduled or manual runs are not part of the standard CI surface.

A `concurrency` group keyed on the ref with `cancel-in-progress: true` MUST be
set, so a force-push or rapid re-push cancels the superseded run rather than
queueing redundant work.

---

## §2 Jobs — build, test, lint as separate jobs

CI runs **build / test / lint** as distinct jobs, plus a PR-only title check:

| Job | Does | Runs on |
|---|---|---|
| `build-platform` | `make build` on each OS (§4 matrix) — **not** a required check | push + PR |
| `build` | required **aggregate** — passes iff every `build-platform` leg did (§7) | push + PR |
| `test` | `make test` (or `make test-cover`) | push + PR |
| `lint` | `golangci-lint` per `repo-layout.md` §5 | push + PR |
| `identity-check` | assert `packaging/identity.yml` matches `.goreleaser`/winget/choco/nfpm/Homebrew (`distribution.md` §8.2) | push + PR |
| `pr-title` | assert the PR title is a conventional commit (`release.md` §1) | **pull_request only** |

`build`/`test`/`lint` are separate because **branch protection requires them as
independent status checks** (`repo-layout.md` §6). A single combined job exposes
one check and cannot satisfy that contract; the monolithic single-job form some
current CLIs use is a divergence (§8). The `pr-title` job exists because
squash-merge makes the PR title the release-gating commit (`release.md` §1) and a
local `commit-msg` hook never sees it; it runs only on `pull_request` (there is
no PR title to check on a `push`). Because it gates release eligibility,
`pr-title` MUST be a **required** PR status check (`repo-layout.md` §6) — left
optional, the gate is advisory and a non-conventional title can merge and either
mis-trigger or silently skip a release.

**`build` is an aggregate, not the matrix.** A matrix job surfaces one check
*per leg* (`build (ubuntu-latest)`, `build (windows-latest)`, …), and that
shifting set is not a stable name branch protection can require. So the OS matrix
is a **non-required** `build-platform` job, and a final **required** `build` job
(`needs: build-platform`, `if: always()`) fails if any leg failed or was
cancelled (a skipped leg does not fail it). Branch
protection requires the stable `build`; the per-leg names stay informational. See
§7 for the shape.

---

## §3 Go version — single source, no drift

The Go toolchain version is owned by `repo-layout.md` §3 and lives in **one
place: the `go` directive in `go.mod`.** CI MUST read it from there rather than
hardcoding a second value that can silently drift:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
```

`go-version-file` is **required for new CLIs** — it removes the second source
entirely. A literal `go-version:` pin is tolerated only as a transitional state
in not-yet-migrated repos and is a divergence to close (§8), never an option for
new code: in practice the two values drift. The current version skew (`sfdc` on
1.24, and
`hubspot` declaring `1.23.0` in `go.mod` while CI pins `1.22`) is exactly the
drift this rule prevents (§8).

---

## §4 The CGO split-build matrix

The credstore Keychain backend requires **CGO on macOS**; Linux and Windows use
pure-Go keyring backends and build static with CGO off
(`working-with-secrets.md` §1.4; the credstore release regression is the
cautionary tale).

CI's `build-platform` matrix (§2) MUST exercise **all three shipped OSes** so a
platform-specific regression is caught pre-merge, not discovered at release time
— with the required `build` aggregate gating on it:

- a macOS runner with `CGO_ENABLED=1` (links `Security.framework`),
- a Linux runner with `CGO_ENABLED=0` (static),
- a Windows runner with `CGO_ENABLED=0` (static) — or, lighter-weight, a
  `goreleaser build --snapshot --single-target` smoke job that compiles the
  Windows target without a full test run.

A CLI that skips macOS-CGO can ship a darwin binary with no Keychain support; a
CLI that skips Windows can ship a binary that doesn't compile — and not notice
until a user's release breaks. The release-time Mach-O verification gate
(`distribution.md` §2) is the second line of defense for darwin; CI is the
first, and the *only* one for Windows. (That same release-time darwin path also
code-signs each binary with a stable identity and gates on the designated
requirement — `distribution.md` §2A.) **Today only `cli-common` runs Windows in
CI** — every shipping CLI builds Windows solely at release via goreleaser, so a
Windows-only break is invisible pre-release (§8).

---

## §5 Lint

Lint runs `golangci/golangci-lint-action@v7` against the repo's `.golangci.yml`
(format `version: "2"`, linter set per `repo-layout.md` §5). The
`golangci-lint` binary version is pinned in the action `with: version:` and
kept consistent across the family. `codereview-cli` ships no `.golangci.yml` and
falls back to defaults, which defeats the purpose of a shared lint standard — a
divergence (§8). A `go.work` monorepo lints per module against each module's own
config: `atlassian-cli` carries three (`shared/`, `tools/cfl`, `tools/jtk`) and
path-filters which run.

---

## §6 Coverage

The `test` job SHOULD capture coverage (`make test-cover`). Uploading to a
coverage service and enforcing a threshold are **optional**: `google-readonly`
gates on a coverage floor (`make test-cover-check`) and that is permitted, but
no CLI is required to. Do not block merges on coverage unless the repo opts in.

---

## §7 Consuming the shared CI — composite actions

CI logic is shared via **composite actions** in `open-cli-collective/.github`,
**not** a reusable workflow. The reason is the status-check contract. A reusable
workflow is called at the *job* level, so its jobs surface as **prefixed**
contexts (`ci / build`, `ci / test`) that you cannot rename to bare — which would
force a branch-protection rewrite in every repo. A **composite action is called
as a *step*** inside the repo's own jobs, so the job names — and therefore the
required-check contexts `build`/`test`/`lint`/`pr-title` — **stay bare and branch
protection is untouched.** (`auto-release` and `release` use *reusable workflows*
instead — they run on push/tag and produce no PR checks, so the prefix issue
never arises: `release.md` §6.)

A conformant repo keeps local jobs that call the shared composites:

```yaml
name: CI
on:
  push: { branches: [main] }
  pull_request:
concurrency: { group: ci-${{ github.ref }}, cancel-in-progress: true }
jobs:
  build-platform:                          # OS matrix — NOT a required check
    name: build (${{ matrix.os }})
    strategy:
      fail-fast: false
      matrix: { os: [ubuntu-latest, macos-latest, windows-latest] }   # §4
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: open-cli-collective/.github/actions/go-build@v1
        with: { go-version-file: go.mod }                              # §3
  build:                                   # required aggregate — stable check name
    needs: [build-platform]
    if: ${{ always() }}
    runs-on: ubuntu-latest
    steps:
      # fail only on a real failure/cancellation; a skipped leg (e.g. a future
      # path filter) must NOT fail the gate — which `= "success"` would do
      - if: ${{ contains(needs.*.result, 'failure') || contains(needs.*.result, 'cancelled') }}
        run: echo "::error::a build-platform leg failed or was cancelled" && exit 1
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: open-cli-collective/.github/actions/go-test@v1
        with: { go-version-file: go.mod }
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: open-cli-collective/.github/actions/go-lint@v1
        with: { go-version-file: go.mod }   # golangci version is pinned inside the composite (§5)
  identity-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: open-cli-collective/.github/actions/identity-check@v1   # distribution.md §8.2
  pr-title:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: open-cli-collective/.github/actions/pr-title@v1           # §1 grammar
```

The composites pin `go-version` (from `go.mod`) and the golangci version, so a
family-wide bump is one edit in `.github`. The job names stay the repo's, so the
required checks remain bare `build`/`test`/`lint`/`pr-title`/`identity-check` —
**no branch-protection churn on migration.** Pin the `@v1` ref.

**Monorepo:** the same composites are called from the repo's own per-tool,
path-filtered jobs (`build-cfl`, `lint-jtk`, …). The composite doesn't care how
many jobs call it — which is exactly why composite (not a reusable workflow) fits
the monorepo: it preserves the bespoke path-filter topology and the per-tool
check names branch protection already requires.

---

## §8 Current divergences

The repo-axis docs are forward-looking. Current divergences, called out so a
future audit knows what to fix and a new CLI does not cargo-cult them:

- **Monolithic single-job CI** in `slack-chat-api`, `google-readonly`, and
  `cli-common` (build+test in one job) vs the separated `build`/`test`/`lint`
  jobs in `newrelic-cli`, `codereview-cli`, `salesforce-cli`, `hubspot-cli`. The
  separated form is the standard (§2).
- **No Windows in CI** anywhere except `cli-common`; every shipping CLI builds
  Windows only at release time via goreleaser (§4).
- **No PR-title check** in any repo — conventional-commit discipline currently
  relies on reviewer diligence (§2).
- **Go versions**: `hubspot-cli` `go.mod` is `1.23.0` but CI pins `1.22`;
  `salesforce-cli` on `1.24`; others `1.26` (§3).
- **golangci-lint version skew** (`v2.12.2` in newer repos, `v2.0.2` in
  `salesforce-cli`); **`.golangci.yml` missing only in `codereview-cli`**
  (`atlassian-cli` has three per-module configs, not none) (§5).
- **No shared CI composite exists yet** — every repo's `ci.yml` is copy-pasted.
  Building the composite actions (§7) and migrating repos onto them is the
  rollout.
- **No `packaging/identity.yml` or `identity-check` anywhere yet** — the manifest
  and its drift-guard (`distribution.md` §8) are new; both ship as part of the
  rollout, manifest first.
