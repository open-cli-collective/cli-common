# cli-common — agent entrypoint

Shared libraries and the normative standards docs for the Open CLI Collective
CLI family. This repo is the standards home, so the source-of-truth links
below are local files, not GitHub URLs.

Read first:

- [`docs/development.md`](docs/development.md) — repo-local facts: package
  map, build/test commands, hermetic-test rules, release/tagging policy for
  this library.
- [`docs/README.md`](docs/README.md) — the standards index: a one-line "use
  this when…" per doc, plus the cross-doc conflict-resolution order.

Shared automation:

Source of truth: https://github.com/open-cli-collective/.github
Local convenience copy, if present: `../.github`

When editing a standards doc, keep per-CLI divergences in that doc's
"Current divergences" section, and follow
[`docs/agent-implementation.md`](docs/agent-implementation.md) for how agent
guidance is shaped and where a repeated failure mode gets enforced (docs,
tests, lint, CI, or shared helpers).
