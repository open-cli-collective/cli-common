# cli-common standards

Nine normative documents define how every Open CLI Collective CLI is built and behaves, split across two axes. New CLIs implement to these.

- **Behavior axis** — what the running binary does: secrets, state, command surface, output, scriptability. Five docs.
- **Repo axis** — how the project is structured, built, released, and shipped: repo layout, CI, release, distribution. Four docs.

The two axes are largely orthogonal: the behavior axis governs the *program*, the repo axis governs the *project*. When an item touches both, the behavior axis wins on binary behavior and the repo axis wins on project mechanics.

**Behavior-axis conflict resolution order**, highest to lowest authority:

1. `working-with-secrets.md` (foundational, predates the rest)
2. `working-with-state.md` (foundational, predates the rest)
3. `command-surface.md` — owns command-tree shape and flag taxonomy
4. `output-and-rendering.md` — owns what a command prints; defers to `command-surface.md` for what flags exist
5. `scriptability.md` — synthesizes the others for installer-script use; defers to all four above for the rules it cross-refs

**Repo-axis conflict resolution order**, highest to lowest authority:

1. `repo-layout.md` — foundational; owns the shared primitives (Go-version source, Makefile target contract, `.golangci.yml`, `version.txt`, repo settings) the other three consume
2. `release.md` — owns the version/tag contract
3. `ci.md` — owns the pre-merge gate
4. `distribution.md` — owns tag → artifacts → channels; defers to `release.md` for the tag/version contract

The repo-axis lifecycle docs own disjoint phases — **ci = pre-merge gate · release = tag minting · distribution = tag → channels** — so genuine conflicts are rare; where they touch a boundary, the producing doc wins (e.g. `release.md` over `distribution.md` on how a tag is formed).

When two docs appear to conflict, the one higher on its axis wins **on the surfaces that doc actually defines.** `working-with-secrets.md` governs credential ingress flags, keyring write behavior, and the `_migration` JSON envelope — not (for example) the verb chosen for a credential-rotation command, which remains `command-surface.md`'s domain. `working-with-state.md` governs config and cache layout, the `refresh` command's signature, hermetic test isolation — not output formatting of a `refresh --status` listing, which is `output-and-rendering.md`'s domain. The hierarchy decides which doc's stance prevails *within its own scope*; out-of-scope claims do not auto-win.

### Behavior axis — what the running binary does

| Doc | Use this when… |
|---|---|
| [`working-with-secrets.md`](working-with-secrets.md) | Working with anything credential-related — keyring backends, `credential_ref`, `init` secret ingress, `set-credential`, `--overwrite`, deployment material vs access secret, file-backend fallback, the `_migration` JSON envelope. |
| [`working-with-state.md`](working-with-state.md) | Working with non-secret on-disk state — config file location, cache layout, atomic writes, hermetic test isolation, the `refresh` command, legacy migration acceptance matrix. |
| [`command-surface.md`](command-surface.md) | Adding or naming commands and flags — verbs (`create` / `delete` / `add` / `remove`), positional-vs-flag, mutation safety (resource `--force` vs credential `--overwrite`), the two prompt classes (setup wizards vs safety confirmations), boolean discipline, async (`--wait`/`--no-wait`), short-alias map, naming hygiene. |
| [`output-and-rendering.md`](output-and-rendering.md) | Shaping what a command prints — text-first principle, the `--id` / `--extended` / `--fulltext` / `--fields` coordinate system, pipe-delimited tables, key:value blocks, ISO-8601 dates, pagination, name/ID resolution, stdout/stderr stream discipline, color stance, JSON scope, the data ↔ presentation seam. |
| [`scriptability.md`](scriptability.md) | Making a CLI deployable — `init` wizard parity, `--non-interactive` + TTY detection, exit codes (the `me` health-check contract), the browser-open pattern, `--profile` reservation, cross-refs to secret-ingress and `refresh`. |

### Repo axis — how the project is built, released, and shipped

| Doc | Use this when… |
|---|---|
| [`repo-layout.md`](repo-layout.md) | Setting up or auditing a repo's static shape — directory layout, required files, the Go-version policy, the Makefile target contract, `.golangci.yml`, `version.txt`, branch protection and merge settings, commit hygiene. |
| [`ci.md`](ci.md) | The pre-merge gate — `ci.yml` triggers, the separate build/test/lint/pr-title jobs, the CGO split-build matrix, lint and coverage, consuming the shared CI composite actions (not a reusable workflow — keeps bare check names). |
| [`release.md`](release.md) | Cutting a release — conventional commits, `version.txt` (`MAJOR.MINOR`) + run-number patch, the dual-gate auto-release, the dedicated tag-token re-trigger handoff (split from the tap token), release recovery/idempotency, the monorepo per-tool variant. |
| [`distribution.md`](distribution.md) | Shipping the binary — the goreleaser build matrix, the CGO-darwin verification gate, artifact identity, Homebrew cask, winget, chocolatey, apt/`.deb` + rpm via `linux-packages`. Snap is being decommissioned. |

Tool-specific specs (jtk's `internal/cmd/GUARDRAILS.md` and `internal/cmd/OUTPUT_SPEC.md`) instantiate the family-wide layer above and add per-tool decisions on top.

These docs are forward-looking pattern docs. Per-CLI divergences from the standards are catalogued inline in each doc's "Current divergences" section; backporting the standards to existing CLIs is a separate workstream.
