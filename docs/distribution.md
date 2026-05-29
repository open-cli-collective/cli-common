# Working with Distribution

This document is the family-wide standard for **getting a built binary to end
users** — the goreleaser build, the pre-publish safety gate, the artifact
naming, and the package channels. It begins where `release.md` ends: a version
tag exists and `release.yml` has fired.

This is **normative for new CLIs.** It supersedes the older root-level guides
`platform-distribution.md` and `linux-distribution.md`, which are stale
(anchored on `confluence-cli PR #96`, since folded into `atlassian-cli`).

Companion pillars:
- `release.md` — mints the tag this doc consumes. **This doc defers to
  `release.md` for the tag/version contract** and never re-defines it.
- `repo-layout.md` — owns `.goreleaser.*` placement and the Go-version the build
  uses.
- `working-with-secrets.md` §1.4 — the credstore Keychain backend that makes the
  §2 CGO gate load-bearing.

**The kept channels:** macOS Homebrew (cask), Windows winget, Windows
chocolatey, Linux apt/`.deb`, Linux rpm. **Snap is being decommissioned (§7).**

---

## §1 goreleaser build matrix

The build produces **six binaries**: `{darwin, linux, windows} × {amd64,
arm64}`. Archives are `tar.gz` (Unix) and `zip` (Windows).

The **CGO split** is mandatory for any CLI using the credstore Keychain backend:

- **darwin** build IDs set `CGO_ENABLED=1` (links `Security.framework` for
  Keychain).
- **linux + windows** build IDs set `CGO_ENABLED=0` (static, pure-Go keyring
  backends).

This split is the single most error-prone part of the goreleaser config; CI
exercises all three OSes pre-merge (`ci.md` §4) and §2 verifies darwin at
release.

---

## §2 The CGO-darwin pre-publish verification gate

Before publishing darwin artifacts, `release.yml` MUST verify the macOS build
actually has working Keychain support — two checks, both observed in
`slack-chat-api`/`newrelic-cli` and both required:

1. **Link check** — `otool -L` the amd64 binary and confirm it is dynamically
   linked against `/System/Library/Frameworks/Security.framework`; fail the
   release if absent.
2. **Functional check** — run the arm64 binary in a hermetic
   `HOME`/`XDG_CONFIG_HOME` (with `<SERVICE>_KEYRING_BACKEND` unset) and assert
   the keyring resolves to Keychain. The command and assertions are
   **manifest-driven** — the identity manifest's `keychain_probe` (§8), not
   hardcoded: `slck`/`nrq` run `<bin> --output json config show` and assert
   `.backend == "keychain"` and `.backend_source == "auto"` (the JSON field is
   `backend_source`, not `source`); `atlassian-cli` text-greps `config show`. A
   CLI whose `config show` can't surface the backend declares its own probe in
   the manifest.

This is regression insurance from the credstore keyring saga: a silently
`CGO_ENABLED=0` darwin build compiles and passes tests but ships **without
Keychain support**, breaking every macOS user on upgrade. The link check proves
the symbol is bound; the functional check proves it actually resolves. Any CLI
using credstore's Keychain backend MUST gate the release on both.

---

## §3 macOS — Homebrew cask

- Published to the shared tap **`open-cli-collective/homebrew-tap`** as
  `Casks/<canonical_cask>.rb` (the manifest's `packages.homebrew.canonical_cask`;
  new CLIs set it to the binary short name, e.g. `slck` — grandfathered tools may
  differ).
- A **cask, not a formula** — we ship a prebuilt binary, not a source build. The
  cask also handles Gatekeeper quarantine removal for the unsigned binary. The
  tap's `Formula/` directory is **deprecated** (cask-only since 2026-01-16); new
  CLIs MUST NOT add a formula, and the surviving `Formula/*.rb` are legacy
  remnants (§10).
- **Standard: goreleaser `homebrew_casks`.** goreleaser owns the canonical cask
  — URL/checksum wiring, `caveats`, and install hooks (slck's Gatekeeper
  `xattr -dr com.apple.quarantine` step is expressible as a cask hook). It is the
  single place release logic lives; see goreleaser's `homebrew_casks` docs.
- **Alias casks via a thin post-step.** `alternative_names` is goreleaser Pro
  only, so a tool with an alias (`slck`→`slack-chat-cli`, `jtk`→`jira-ticket-cli`)
  needs a post-step for the extra token/name. **Implementation (atomic form):**
  goreleaser *renders* the canonical cask but does **not** push it
  (`skip_upload: true`); a single post-step then commits the goreleaser-rendered
  canonical cask **plus** the alias copies to the tap in **one commit/push**. The
  alias copies take the canonical cask's URL/checksum verbatim (never recompute)
  and differ only in token/name. One atomic push means there is no window where
  the canonical cask is live but its alias isn't; the post-step MUST fail visibly
  on any push error.
- The push to the tap uses a dedicated **`HOMEBREW_TAP_TOKEN`** (§6).
- **Current split (divergences, §10):** `google-readonly`, `salesforce-cli`,
  `hubspot-cli`, and both atlassian tools already use goreleaser `homebrew_casks`;
  `slack-chat-api` and `newrelic-cli` hand-roll the cask via heredoc in
  `release.yml`, and `newrelic-cli` additionally regenerates a
  `Formula/newrelic-cli.rb`. The reusable workflow standardizes on goreleaser
  casks + the alias post-step; the heredocs (and the nrq formula) are removed on
  migration.

---

## §4 Windows — winget and chocolatey

### §4.1 winget
Three-manifest YAML (version / installer / locale) submitted to
`microsoft/winget-pkgs` via `winget-publish.yml`; `test-winget.yml` validates
the manifests first. The version comes from the tag (`release.md`).

### §4.2 chocolatey
A `.nuspec` + install script published via `chocolatey-publish.yml`, validated
by `test-chocolatey.yml`. Chocolatey runs **automated moderation** on every
submission; the package MUST satisfy the `CPMR` rule series — principally
checksums on all downloaded artifacts, reachable/authoritative download URLs,
and complete package metadata. The exact rule set is encoded in the existing
`chocolatey-publish.yml`; when authoring the reusable workflow, lift the current
checks verbatim rather than re-deriving them.

---

## §5 Linux — apt/`.deb` and rpm via `linux-packages`

This channel is **live and automated** — verified 2026-05-29: the shared repo
holds signed packages for `slck`, `jtk`, `cfl`, `sfdc`, and `google-readonly`
(gro's Linux package uses the long repo name, not the binary — a grandfathered
divergence, §10), plus `nrq` / `hspt` by config.

### §5.1 Package generation (nfpms)
goreleaser's `nfpms` block builds `.deb` and `.rpm`. Standard shape:

```yaml
nfpms:
  - package_name: <binary>
    vendor: Open CLI Collective
    maintainer: Open CLI Collective <https://github.com/open-cli-collective>
    description: <one line>
    license: MIT
    formats: [deb, rpm]
    bindir: /usr/bin
    contents:
      - src: LICENSE
        dst: /usr/share/licenses/<binary>/LICENSE
```

### §5.2 The dispatch contract
`release.yml` has a `linux-packages` job that hands the built packages off to
the shared repo via **`repository_dispatch`**:

- action: `peter-evans/repository-dispatch`
- repository: `open-cli-collective/linux-packages`
- event-type: **`package-release`**
- client-payload: `{ "package": "<binary>", "version": "${{ github.ref_name }}", "repo": "open-cli-collective/<repo>" }`
- token: **`LINUX_PACKAGES_DISPATCH_TOKEN`**
- **`continue-on-error: true`** — a publish hiccup MUST NOT fail the release.
  But the failure MUST still be surfaced, not swallowed (`release.md` §4.1).

### §5.3 What `linux-packages` does on receipt
Its `receive-package.yml` downloads the `.deb`/`.rpm` from the source release,
**GPG-signs** them (`LINUX_PACKAGES_GPG_PRIVATE_KEY` / `…_PASSPHRASE`), rebuilds
APT metadata with **`reprepro`** and RPM metadata with **`createrepo_c`**,
commits, and deploys GitHub Pages. The public signing key lives at
`keys/gpg.asc`. End users add the apt/rpm repo URL + that key.

A new CLI's only obligation here is §5.1 (emit deb/rpm) + §5.2 (dispatch);
`linux-packages` is the shared sink and needs no per-CLI changes beyond being
listed in its README.

---

## §6 Secrets inventory

| Secret | Used for |
|---|---|
| `RELEASE_TAG_TOKEN` (or a GitHub App token) | the tag re-trigger (`release.md` §3.1) |
| `HOMEBREW_TAP_TOKEN` | push the cask to `homebrew-tap` (§3) |
| `CHOCOLATEY_API_KEY` | chocolatey publish (§4.2) |
| `WINGET_GITHUB_TOKEN` | winget-pkgs submission (§4.1) |
| `LINUX_PACKAGES_DISPATCH_TOKEN` | the §5.2 `repository_dispatch` |

The GPG signing keys (`LINUX_PACKAGES_GPG_*`) live in the `linux-packages` repo,
not in the CLI repos. **Current repos overload a single `TAP_GITHUB_TOKEN`** for
both the tag re-trigger and the tap push; the standard splits them (§10).

---

## §7 Snap — decommissioned

Snap is **out of scope for the family and is being decommissioned.** It is not a
vestige — it is still wired up across the family in three states:

- **Active** `snap` jobs in `release.yml` (`snapcore/action-build` +
  `snapcore/action-publish`, reading `snap/snapcraft.yaml`): `slack-chat-api`,
  `hubspot-cli`, `newrelic-cli`.
- **Gated off**: `google-readonly` has the same job but `if: false`
  ("temporarily disabled — waiting for personal-files interface approval").
- **Orphaned** `snap/` dirs with no job: `salesforce-cli`, `atlassian-cli` `cfl`,
  `atlassian-cli` `jtk`.

(Snap is a `snap/snapcraft.yaml` file plus a workflow job — there is no goreleaser
`snapcrafts` block anywhere. "Active" means the job would publish on release; the
live Snapcraft-store listings were not independently confirmed.)

- **New CLIs MUST NOT add snap** — no `snap/` dir, no `snap` job, no Snapcraft
  listing.
- **Decommission steps:** for the active publishers (`slck`, `hspt`, `nrq`) and
  the gated-off `gro`: (1) remove the `snap` job from `release.yml`; (2) delete
  the `snap/` dir; (3) optionally unpublish or archive any Snapcraft-store
  listing. For the orphaned dirs (`sfdc`, `cfl`, `jtk`): delete the `snap/` dir.
- **User impact:** any existing snap users stop receiving updates — communicate
  the migration path (Homebrew/apt/rpm) before pulling a listing.

---

## §8 Artifact identity

Package identifiers are **user-facing and sticky** — a published winget/choco ID
is the string a user types to install, and changing it strands them. Two rules:

1. **New CLIs** derive identifiers from the **binary short name** (which is *not*
   the repo name — `slack-chat-api` ships `slck`).
2. **Every repo declares its identifiers in a machine-readable manifest**,
   `packaging/identity.yml` — the **authoritative** declaration. The model is
   *authoritative manifest + enforced duplicates*, with a clean split:
   - **Read-from-manifest** for everything the reusable workflows *generate*: the
     §5.2 dispatch `package`, the Homebrew alias list, the §2 keychain probe, the
     tag form. No duplication exists, so there is nothing to check.
   - **Enforced duplicate** for values that must also live in a tool-native file
     the tool owns (`.goreleaser` `name_template`/`package_name`/`homebrew_casks`,
     the winget manifests, the chocolatey `.nuspec`). The manifest stays
     authoritative; §8.2 `identity-check` enforces the native copies match it.

   The manifest is *not* a renderer for goreleaser/winget/choco config — those
   keep their tool-native files; the manifest is the authority those files are
   checked against.

### §8.1 The `packaging/identity.yml` schema (`open-cli-identity/v1`)

```yaml
schema: open-cli-identity/v1
repo: slack-chat-api
module: github.com/open-cli-collective/slack-chat-api
binary: slck
service_name: slack-chat-api            # keyring service / config dir
version_file: version.txt
goreleaser_config: .goreleaser.yaml

tag:
  prefix: v                             # 'cfl-v' for a monorepo tool
  version_scheme: major_minor_run_patch # version.txt = MAJOR.MINOR, patch = run number (release.md §2)

archives:
  name_template: "slck_v{{ .Version }}_{{ .Os }}_{{ .Arch }}"   # load-bearing — download URLs depend on it

packages:
  homebrew:
    canonical_cask: slck
    alias_casks: [slack-chat-cli]       # emitted by the thin alias post-step (§3)
    caveats: packaging/homebrew/caveats.txt
    postflight: packaging/homebrew/postflight.rb
  winget:     { id: OpenCLICollective.slack-chat-cli }   # grandfathered long form (§10)
  chocolatey: { id: slack-chat-cli }
  linux:      { package_name: slck }    # nfpm package_name AND the §5.2 dispatch 'package' key
  snap:       { state: decommissioning }                 # §7

keychain_probe:                         # drives the §2 darwin functional gate; one shared runner
  env_unset: [SLACK_CHAT_API_KEYRING_BACKEND]   # clear backend overrides so auto-detect runs
  seed_config:                          # written under the hermetic XDG_CONFIG_HOME
    path: slack-chat-api/config.yml
    content: |
      credential_ref: slack-chat-api/default
      workspace: smoke
  command: ["--output", "json", "config", "show"]
  output: json                          # json → assert jq paths; text → match regexes
  assertions:                           # output: json
    .backend: keychain
    .backend_source: auto
    .credential_ref: slack-chat-api/default
  # output: text alternative (e.g. atlassian) —
  #   output: text
  #   match: ['backend:\s*keychain', 'source:\s*auto']
```

**New CLIs** populate this from the binary short name (cask `slck`, winget
`OpenCLICollective.slck`, choco `slck`, linux `slck`, archive
`<binary>_v{{ .Version }}_…`). **Existing tools record their grandfathered
values** — `slack-chat-cli` (slck's winget/choco) matches neither repo nor
binary; `google-readonly` is gro's linux `package_name`; archive templates vary
(`hspt_{{ .Version }}`, `{{ .ProjectName }}_…`). The manifest captures reality
rather than forcing a user-facing ID change (§10).

### §8.2 The identity-check (single-source enforcement)

A manifest that nothing verifies is just one more drift source. The
**`identity-check`** composite action (`ci.md` §7, required on PRs) asserts the
**tool-native duplicates** (the "enforced duplicate" half of rule 2) match the
manifest: `.goreleaser` (`binary`, `archives.name_template`, nfpm
`package_name`, the `homebrew_casks` token), the winget manifests, and the
chocolatey `.nuspec`. The values the workflows generate directly from the
manifest (the §5.2 dispatch `package`, the alias-cask list, the keychain probe)
aren't duplicated, so there's nothing to check there. A mismatch fails the PR —
that's what makes the manifest authoritative rather than a fourth, silently
drifting copy.

### §8.3 Monorepo

One identity file **per binary** — `tools/cfl/packaging/identity.yml`,
`tools/jtk/packaging/identity.yml` (or a single file with a top-level `tools:`
map) — so each binary's release identity stays explicit. The alias cask
(`jira-ticket-cli` ← `jtk`) is just an `alias_casks` entry.

The nfpm `package_name` and the dispatch `package` key MUST match the manifest's
`packages.linux.package_name` — a mismatch routes packages under the wrong name
in `linux-packages`.

---

## §9 Consuming the reusable workflow

Canonical implementation:
`open-cli-collective/.github/.github/workflows/release.yml`. The local caller
passes the channels it ships and the goreleaser config path; the publish jobs
(homebrew / winget / chocolatey / linux-packages dispatch) are parameterized and
isolated per `release.md` §4.1. Pin the `@v1` ref. Secrets (§6) pass through
from the calling repo.

---

## §10 Current divergences

- **Snap**: active jobs in `slck`/`hspt`/`nrq`, gated off (`if: false`) in `gro`,
  orphaned `snap/` dirs in `sfdc` + atlassian `cfl`/`jtk`; decommission pending
  (§7).
- **Long-form winget/choco IDs** (`OpenCLICollective.slack-chat-cli`,
  `.newrelic-cli`, `.google-readonly`) predate the §8 binary-short-name
  convention — grandfathered, not migration targets.
- **Mixed archive templates**: `slck`/`nrq`/`gro` use `<binary>_v{{ .Version }}_…`
  (literal `v`); `hspt` uses `hspt_{{ .Version }}_…` (no `v`); `sfdc` +
  `atlassian-cli` use `{{ .ProjectName }}_{{ .Version }}_…` (no `v`). New CLIs
  standardize on the `_v` form (§8).
- **gro's Linux package name is `google-readonly`** (the repo/long name), not the
  binary `gro` — both the nfpm `package_name` and the §5.2 dispatch `package` key
  use `google-readonly`. Grandfathered; new CLIs use the binary short name (§8).
- **Homebrew `Formula/` remnants** in `homebrew-tap`: `newrelic-cli` (orphaned —
  formula stuck at 1.0.26 while the cask is 1.0.11), `gro` and `gmail-ro` (both
  marked deprecated 2026-01-28). Cask-only is the standard (§3); these should be
  removed and release automation kept from re-adding formulae.
- **Homebrew cask is split**: goreleaser `homebrew_casks` in `google-readonly`,
  `salesforce-cli`, `hubspot-cli`, `cfl`, `jtk`; hand-rolled heredoc in
  `slack-chat-api` and `newrelic-cli` (and `nrq` also regenerates a
  `Formula/newrelic-cli.rb`). Standard is goreleaser casks + a thin alias
  post-step (§3); the heredocs and the nrq formula are removed on migration.
- **`hubspot-cli`'s `linux-packages` dispatch lacks `continue-on-error: true`**
  (§5.2) — a dispatch failure would fail its release; add it on migration.
- **Overloaded token**: current repos use one `TAP_GITHUB_TOKEN` for tag push +
  tap push; the standard splits it (`RELEASE_TAG_TOKEN` + `HOMEBREW_TAP_TOKEN`,
  or a GitHub App token) (§6, `release.md` §3.1).
- **No reusable workflow yet** — `release.yml`, `chocolatey-publish.yml`,
  `winget-publish.yml`, and the per-channel test workflows are copy-pasted
  across repos (doubled per-tool in `atlassian-cli`).
- **CGO verification gate** is present in `slack-chat-api` and `newrelic-cli`;
  confirm it exists in every credstore-Keychain CLI's `release.yml` (§2).
