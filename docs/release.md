# Working with Releases

This document is the family-wide standard for **cutting a release** — how a
merge to `main` becomes a version tag. It owns the conventional-commit
gating, the version source-of-truth, and the tag-minting mechanism. Its scope
**ends the moment a tag is pushed**; what happens next (goreleaser builds,
channel publishing) belongs to `distribution.md`.

This is **normative for new CLIs.**

Companion pillars (repo axis):
- `repo-layout.md` — owns `version.txt` and the commit-hygiene rules this doc
  builds on.
- `ci.md` — the pre-merge gate (including the PR-title check, §1). A release
  only ever cuts from a commit that already passed CI.
- `distribution.md` — consumes the tag this doc mints. **`release.md` owns the
  tag/version contract; `distribution.md` defers to it** for how the tag is
  formed.

**When this doc appears to conflict with `repo-layout.md`, that wins** for the
primitives it owns (`version.txt`, commit conventions). See `docs/README.md`
for the full conflict-resolution order.

---

## §1 Conventional commits → release gating

Commit type decides **whether a release cuts at all** — not the size of the
version bump (the version comes entirely from §2):

| Type | Cuts a release? |
|---|---|
| `feat:` | yes |
| `fix:` | yes |
| `refactor:` `test:` `docs:` `ci:` `chore:` | no |

There is deliberately **no commit-type → bump-magnitude mapping.** `feat:` does
not auto-bump the minor and `fix:` does not auto-bump the patch; both simply
answer "does this merge ship?" The `MAJOR.MINOR` line is human-controlled in
`version.txt` and the patch is the CI run number (§2).

Squash-merge (`repo-layout.md` §6) means the **PR title is the commit that lands
on `main`**, so the PR title MUST be a conventional commit. This is enforced by
a **CI check on pull requests** (`ci.md` §2) — a local `commit-msg` hook never
sees the squashed PR title, so the gate has to live in CI. Commit messages MUST
NOT mention AI tooling (`repo-layout.md` §7).

### §1.1 The shared commit grammar

The auto-release **commit gate** (§3) and the CI **`pr-title`** check (`ci.md`
§2) MUST parse the same grammar, or a title can pass CI while auto-release skips
it (or vice-versa). One regex, used by both:

```
^(feat|fix|refactor|test|docs|ci|chore|build|perf|style)(\([^)]+\))?!?: .+
```

- `pr-title` accepts the full type set — any match is a valid title.
- the release gate matches only the **`feat|fix`** subset of the same grammar.

Both MUST handle a **scope** (`feat(cli): …`) and a **breaking-change bang**
(`feat!: …`, `feat(cli)!: …`); a naive `feat:`-prefix check misses these and
desyncs the two gates. The `!` marks a breaking change but does **not** auto-bump
major — the version is human-set (§2), so `feat!:` cuts a release exactly like
`feat:`.

---

## §2 Versioning — `MAJOR.MINOR` + run-number patch

`version.txt` at the repo root holds the **`MAJOR.MINOR`** line (e.g. `3.1`,
`1.0`, `0.1`) — human-set, and the only authority for major/minor. The **patch
is the CI run number** (`GITHUB_RUN_NUMBER`), assigned at release time. The tag
is therefore:

```
v<MAJOR.MINOR from version.txt>.<GITHUB_RUN_NUMBER>      e.g.  v3.1.150
```

This is a **valid 3-part SemVer** tag — package channels (`distribution.md`)
consume it directly. Patch numbers are monotonic and unique but track total CI
runs, so they are non-contiguous (e.g. `…45, 47, 48, 50`); that is the accepted
tradeoff for never hand-bumping the patch. The build stamps the version via
`-ldflags` from the tag; `version.txt` holds only `MAJOR.MINOR`, so there is no
full-version constant in the repo to drift.

To release a new `MAJOR.MINOR`, bump `version.txt`. **The path gate (§3) MUST
include `version.txt`** so a deliberate `MAJOR.MINOR` bump ships on its own
merge. Current repos exclude it (§7) — meaning today a `version.txt`-only PR
does *not* release and the new base only takes effect on the next qualifying
code merge. The reusable workflow fixes this by adding `version.txt` to the
release paths.

---

## §3 The dual-gate auto-release

`.github/workflows/auto-release.yml` runs on push to `main` and decides whether
to mint a tag. **Both gates must pass** — this is what keeps doc-only and
CI-only merges from cutting pointless releases:

1. **Path gate** — the merge touched `**.go`, `go.mod`, `go.sum`, **or
   `version.txt`** (the last so a deliberate `MAJOR.MINOR` bump ships, per §2 —
   current repos omit it, §7). A change to only `README.md`, workflows, or docs
   does not release.
2. **Commit gate** — the landed commit is `feat:` or `fix:` (§1).

On pass, the workflow:

1. reads `MAJOR.MINOR` from `version.txt` (§2),
2. forms the tag `v<MAJOR.MINOR>.<GITHUB_RUN_NUMBER>` (e.g. `3.1` → `v3.1.150`),
3. pushes the tag.

### §3.1 The token handoff (load-bearing)

The tag push MUST use a **dedicated token, not the default `GITHUB_TOKEN`.**
GitHub deliberately suppresses workflow triggers for refs pushed with the
built-in `GITHUB_TOKEN` (its recursive-workflow guard), so a tag pushed that way
would sit there and `release.yml` would never fire. A separate token is what
lets the tag push **re-trigger** the release workflow. This is the single most
common thing to get wrong standing up a new repo — never "simplify" it back to
`GITHUB_TOKEN`.

**Token choice (in preference order):**

1. A **GitHub App installation token** — short-lived, scoped to the repo's
   `contents: write`. Preferred: no long-lived credential, no human owner.
2. A dedicated, narrowly-scoped PAT named **`RELEASE_TAG_TOKEN`** (tag/contents
   push only) — kept **separate** from the Homebrew-tap push token
   (`HOMEBREW_TAP_TOKEN`, `distribution.md` §6).

Current repos overload a single `TAP_GITHUB_TOKEN` for both the tag re-trigger
and the tap push (§7); new CLIs MUST NOT conflate the two.

---

## §4 The release workflow

`.github/workflows/release.yml` triggers on tag push matching `v*`. It runs
goreleaser to build and publish. **The build matrix, the CGO-darwin
verification gate, and every publish channel are owned by `distribution.md`** —
`release.md`'s responsibility is only that a correctly-formed tag exists and
`release.yml` fires on it.

### §4.1 Release recovery and idempotency

Releases will partially fail; the standard requires they be recoverable:

- **Re-run from an existing tag.** `release.yml` MUST be safe to re-run on the
  same tag (manual `workflow_dispatch` with the tag, or re-running the failed
  run). Idempotent re-publish is a **goreleaser `release:` config** concern, not
  a flag: set **`replace_existing_artifacts: true`** so a re-run overwrites the
  already-uploaded artifacts. The `release.mode` setting (`keep-existing` /
  `append` / `replace`) governs the release **notes/body**, not artifact
  re-upload — don't conflate the two. Use `--skip=...` to bypass steps already
  done. **`--clean` only wipes the local `dist/` dir** — keep it for a clean
  rebuild, but it does **not** make an already-created GitHub release idempotent.
- **Per-channel isolation.** A failure in one publish channel MUST NOT abort the
  others; channels publish independently so a chocolatey moderation hold does
  not block Homebrew.
- **Surface silent failures.** The linux-packages dispatch runs
  `continue-on-error: true` (`distribution.md` §5.2) so a hiccup does not fail
  the release — but a swallowed failure MUST still be made visible (a workflow
  annotation / summary line / non-fatal notice), never logged as success. A
  release that "passed" while a channel silently dropped is the failure mode
  this rule exists to prevent.
- **Idempotent re-publish.** Re-running a channel for an already-published
  version is a no-op (or an explicit `--overwrite`), never a duplicate.

---

## §5 Monorepo variant

`atlassian-cli` is a `go.work` monorepo (`tools/cfl`, `tools/jtk`). It runs the
same machinery **per tool**:

- Separate `auto-release-cfl.yml` / `auto-release-jtk.yml`, each with the §3
  path gate scoped to that tool's subtree (`tools/<tool>/**`, plus `shared/**`).
- Tool-prefixed tags: `cfl-v<MAJOR.MINOR>.<run>`, `jtk-v<MAJOR.MINOR>.<run>`
  (e.g. `cfl-v0.9.150`). Per-tool `version.txt` lives at the tool root.
- Separate `release-<tool>.yml` triggered on the matching tag prefix. Because
  goreleaser wants a bare-SemVer tag, the workflow mints a **temporary `v<MAJOR.
  MINOR>.<run>` tag** for goreleaser, then re-tags the release to the
  tool-prefixed form and deletes the temporary tag. **Sharp edge:** goreleaser
  runs *before* the rename, so any release-download URL it emits must be
  templated to the final prefixed tag or it will 404 (see atlassian-cli's
  `CLAUDE.md` and the `homebrew_casks` `url.template`).

A new monorepo follows this shape: one auto-release + one release workflow per
shipped binary, path-filtered, prefix-tagged.

---

## §6 Consuming the reusable workflow

Canonical implementation:
`open-cli-collective/.github/.github/workflows/auto-release.yml`. A conformant
repo's local workflow is a thin caller:

```yaml
name: Auto Release
on:
  push:
    branches: [main]
jobs:
  auto-release:
    uses: open-cli-collective/.github/.github/workflows/auto-release.yml@v1
    with:
      tag-prefix: v                              # 'cfl-v' / 'jtk-v' for monorepo tools
      version-file: version.txt
      release-paths: "**.go,go.mod,go.sum,version.txt"  # §3 path gate (incl. version.txt, §2)
      tool-paths: ""                             # monorepo: 'tools/cfl/**,shared/**'
    secrets:
      tag-token: ${{ secrets.RELEASE_TAG_TOKEN }}   # §3.1 — or a GitHub App token
```

Inputs: `tag-prefix`, `version-file`, `release-paths`, `tool-paths`. Secret:
`tag-token` (the §3.1 dedicated token). Pin the `@v1` ref.

---

## §7 Current divergences

- **`version.txt` is present everywhere with release machinery** — all six
  shipping repos plus both atlassian tools carry it (`MAJOR.MINOR`);
  `codereview-cli` lacks it only because it has no release workflow yet. There is
  no "embedded source version" divergence (an earlier draft claimed one — it was
  wrong).
- **`version.txt` excluded from the path gate** in every current repo — a
  `MAJOR.MINOR`-only bump PR does not release (§2/§3). The reusable workflow adds
  it back.
- **Commit-gate grammar unverified** — current `auto-release.yml` gates on a
  `feat:`/`fix:` prefix; confirm it (and the new `pr-title` check) accept scoped
  and bang forms per §1.1 when authoring the reusable workflow, or scoped/bang
  titles will silently skip releases.
- **Overloaded release token**: all current repos use one `TAP_GITHUB_TOKEN` for
  both the tag re-trigger and the Homebrew-tap push; the standard splits these
  (`RELEASE_TAG_TOKEN` + `HOMEBREW_TAP_TOKEN`, or a GitHub App token) (§3.1).
- **`codereview-cli` has no release machinery at all** (only `ci.yml`) — no
  `auto-release.yml`, no `release.yml`. It is the obvious first beneficiary of
  the reusable workflows.
- **No reusable workflow exists yet** — `auto-release.yml` / `release.yml` are
  copy-pasted across six repos (doubled per-tool in `atlassian-cli`).
