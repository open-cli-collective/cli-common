# Working with Repository Layout

This document is the family-wide standard for the **static shape of a CLI
repository** — directory structure, the files it must contain, the Go-version
policy, the Makefile contract, the lint config, and the repository settings.
It is the foundation the other repo-axis docs build on: `ci.md`, `release.md`,
and `distribution.md` all consume primitives defined here.

This is **normative for new CLIs.**

Companion pillars:
- `ci.md` — invokes the Makefile targets (§4) and lint config (§5) defined here.
- `release.md` — reads `version.txt` (§2) and the commit conventions (§7).
- `distribution.md` — reads `.goreleaser.*` (§2).
- The behavior-axis docs (`command-surface.md`, `output-and-rendering.md`,
  `working-with-secrets.md`, `working-with-state.md`, `scriptability.md`) govern
  what lives *inside* the directories below. This doc owns the skeleton; they
  own the contents.

**`repo-layout.md` is foundational among the repo-axis docs** — when another
repo-axis doc appears to conflict on a primitive defined here (Go version,
Makefile targets, lint config, `version.txt`), this doc wins. See
`docs/README.md` for the full conflict-resolution order.

---

## §1 Directory layout

```
cmd/<binary>/main.go        # entry point — one per shipped binary
internal/
  cmd/
    root/                   # root command + Options struct (the DI seam)
    initcmd/                # <tool> init        (scriptability.md §1)
    setcred/                # <tool> set-credential (working-with-secrets.md §1.5.2)
    config/                 # <tool> config {show,clear,test}
    me/                     # <tool> me          (scriptability.md §4)
    <feature>/              # one package per domain command group
  config/                   # config-file load/save (working-with-state.md §3)
  client/                   # the data layer — API client (output-and-rendering.md §9)
  output/  (or view/)       # the presentation layer — pure render fns
  version/                  # version string wiring (release.md §2)
  noleak/                   # secret no-leak test helper (working-with-secrets.md)
  testutil/                 # shared test fixtures
api/                        # OPTIONAL: exported public client library
```

This is the shape `slck` (the cli-common pilot) and `nrq` follow. A new CLI
starts from this tree. The three-layer seam — `client` (data) → `output`
(presentation) → `internal/cmd` (command glue) — is mandated by
`output-and-rendering.md` §9; this section only fixes where those layers live.

---

## §2 Required files

| File | Status | Notes |
|---|---|---|
| `README.md` | MUST | install + usage |
| `LICENSE` | MUST | MIT |
| `CLAUDE.md` | MUST | per-repo agent guidance |
| `.golangci.yml` | MUST | §5 |
| `Makefile` | MUST | §4 |
| `version.txt` | MUST | `release.md` §2 source-of-truth |
| `.gitignore` / `.gitattributes` | MUST | |
| `.goreleaser.{yml,yaml}` | MUST if distributed | `distribution.md` §1 |
| `packaging/identity.yml` | MUST if distributed | `distribution.md` §8 — required by `identity-check` |
| `CONTRIBUTING.md` | SHOULD | |
| `CHANGELOG.md` | SHOULD | release-generated |

Monorepo (`atlassian-cli`): per-tool files live under `tools/<tool>/`
(`version.txt`, `.goreleaser-<tool>.yml`); repo-root carries the shared
`go.work`, `LICENSE`, and CI.

---

## §3 Go version policy

- The floor is **Go 1.26.** New repos pin the floor.
- The **single source is the `go` directive in `go.mod`.** Everything else
  (`ci.md` §3, local dev) reads from there. Do not duplicate the version into a
  workflow literal that can drift.
- Bump across the family in lockstep. `salesforce-cli` is on `1.24`;
  `hubspot-cli` declares `1.23.0` in `go.mod` but pins `1.22` in CI (a mismatch —
  sync both) (§8).
- Monorepo: each tool has its own `go.mod`; all sit on the floor.

---

## §4 Makefile target contract

CI and release invoke the Makefile, so the target **names** are a contract.
Every repo's Makefile MUST provide:

| Target | Does |
|---|---|
| `build` | compile the binary/binaries |
| `test` | `go test ./...` |
| `test-cover` | tests with coverage profile |
| `lint` | `golangci-lint run` |
| `fmt` | `gofmt`/`goimports` |
| `tidy` | `go mod tidy` |
| `deps` | fetch/verify deps |
| `check` | `tidy` + `fmt` + `lint` + `test` + `build` — the local pre-push gate |
| `install` | install to a local bin |
| `release` / `snapshot` | goreleaser wrappers |
| `clean` | remove build artifacts |

`make check` MUST mirror what CI gates on, so a green local `check` predicts a
green CI run.

**Library/shared-module exemption.** The full target set above applies to
**shipped-binary CLI repos**. A library or shared module that produces no binary
and ships through no package channel — e.g. `cli-common` itself, whose `make
check` is just `tidy` + `lint` + `test` — needs only `tidy`/`lint`/`test`/`build`
and is exempt from `install`/`release`/`snapshot`/`test-cover`. Do not force
distribution targets onto a repo that distributes nothing.

---

## §5 Lint configuration

`.golangci.yml`, format `version: "2"`. The **canonical floor linter set** every
repo MUST enable:

```
errcheck  govet  ineffassign  staticcheck  unused  misspell  revive  gosec  errorlint  exhaustive
```

This is already the de-facto family set — `google-readonly` and all three
`atlassian-cli` configs (`shared/`, `tools/cfl`, `tools/jtk`) ship it. `gosec`
and `errorlint` matter for CLIs that handle credentials, which is all of them.
Tests are excluded from `errcheck` (and from `errorlint`/`gosec` where noisy). A
repo MAY add more linters but MUST NOT drop below the floor; the leaner repos
(`slack-chat-api`, `salesforce-cli`) sit below it today (§8). Running on golangci
defaults (no config file) is non-conformant.

---

## §6 Repository settings

### Branch protection (`main`)
- Require a PR with **1 approval**.
- Required status checks: **`build`, `test`, `lint`, `pr-title`, and
  `identity-check`** (`ci.md` §2). `pr-title` is required because it gates release
  eligibility (`release.md` §1); `identity-check` because it guards the
  single-source manifest (`distribution.md` §8.2). `build` is the required
  **aggregate** over the non-required `build-platform` OS matrix (`ci.md` §2/§7)
  — require `build`, never the per-OS legs (their names shift with the matrix).
  Current repos require only
  `test` + `lint` — fold the rest in when migrating. Because CI is shared via
  **composite actions** (`ci.md` §7), these are bare check names and **stay bare
  on migration** — no branch-protection rewrite (the context-rename hazard only
  applies to the reusable-workflow form, which CI does not use).
- Require **signed commits**.
- Require **linear history**.
- No force pushes.

### Merge settings
- **Squash merge only** (no merge commits, no rebase merge). This is why the PR
  title must be a conventional commit (`release.md` §1).
- **Delete branch on merge.**

---

## §7 Commit hygiene

- **Conventional commits** drive releases (`release.md` §1).
- Commit messages MUST NOT mention AI tooling (Claude, Anthropic, ChatGPT,
  Copilot, etc.). Enforce with a `commit-msg` hook that greps a blocklist and
  rejects on match (the reference hook lives in `CLI_CONVENTIONS.md`).

---

## §8 Current divergences

- **`.golangci.yml` missing** in `codereview-cli` only — it is the single repo
  without one. `atlassian-cli` is *not* missing it: as a `go.work` monorepo it
  carries three configs (`shared/`, `tools/cfl`, `tools/jtk`), all on the §5 set.
- **Linter floor not yet met** by `slack-chat-api` and `salesforce-cli` (lean
  6-linter core, missing `revive`/`gosec`/`errorlint`/`exhaustive`) (§5).
- **`CLAUDE.md` missing** in `codereview-cli` (§2).
- **`CONTRIBUTING.md` / `CHANGELOG.md` gaps** in `google-readonly`,
  `salesforce-cli`, `hubspot-cli`, and `atlassian-cli` root (§2).
- **Go versions**: `hubspot-cli` declares `go 1.23.0` in `go.mod` but its CI pins
  `1.22` (an internal mismatch — sync both); `salesforce-cli` is on `1.24`; all
  others on `1.26` (§3).
- **Required-check names**: `newrelic-cli` and `codereview-cli` branch protection
  requires only `test` + `lint` (no `build`, no `pr-title`, no `identity-check`).
  Composite-based CI (`ci.md` §7) keeps these bare, so migration just *adds* the
  missing checks — no renaming of the existing ones (§6).
- **Makefile target coverage** varies; `google-readonly` alone has
  `test-cover-check`. `cli-common` is intentionally minimal (library exemption,
  §4).
