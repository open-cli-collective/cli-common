# Working with Agent Implementation

The Open CLI Collective's standards are meant to be implemented by people and by
agents. This document is the family-wide policy for **how agents work inside CLI
repositories**: where they find source-of-truth guidance, how agent entrypoint
files are shaped, how new CLIs start from the standards, and how repeated agent
failure modes become durable checks or shared helpers.

This is **normative for new CLI work** and the target state for existing CLI
repos. It complements the harness-engineering principles described by OpenAI:
thin entrypoints, repository knowledge as the system of record, mechanical
enforcement for invariants, and feedback loops from review into the repo
itself: https://openai.com/index/harness-engineering/

## Authority boundary

This doc governs **agent operating discipline**: discovery, source-of-truth
selection, local guidance shape, implementation workflow, and feedback loops.
It does not redefine CLI runtime behavior or repository mechanics.

When this doc appears to conflict with another standard:

- The behavior-axis docs win for binary behavior: secrets, state, command
  surface, output, and scriptability.
- The repo-axis docs win for project mechanics: required files, CI, release, and
  distribution.
- This doc wins for the shape and use of agent guidance entrypoints.

See `docs/README.md` for the full behavior-axis and repo-axis ordering.

---

## §1 Source-of-truth model

Agent-readable guidance has three layers. Do not collapse them into one local
file.

| Layer | Source of truth | Holds |
|---|---|---|
| Shared CLI standards | `open-cli-collective/cli-common/docs` | Family-wide behavior, repo mechanics, and this agent policy. |
| Shared automation | `open-cli-collective/.github` | Composite actions, reusable workflows, and automation implementation. |
| Repo-local facts | `<repo>/docs/development.md` or a tool-local equivalent | Binary name, local package layout, concrete Make targets, provider-specific facts, and known local divergences. |

When a repo-local guidance file references shared standards or automation, the
GitHub URL is the source of truth. A relative local path may be included as a
convenience for side-by-side workspaces, but it is never the authority.

Use the shared citation shape from `docs/README.md`:

```md
Source of truth: https://github.com/open-cli-collective/cli-common/blob/main/docs/command-surface.md
Local convenience copy, if present: `../cli-common/docs/command-surface.md`
```

Nested guidance files must adjust the local path relative to their own location.
A broken local shortcut is worse than no shortcut because it trains agents to
trust a path that does not resolve.

---

## §2 Agent entrypoints are indexes

Every new CLI repo has both `AGENTS.md` and `CLAUDE.md` at the repo root. They
are peer compatibility entrypoints for different harnesses, not two policy
containers.

They MUST:

- Point first to the repo-local development guide.
- Point to the shared standards index in `cli-common/docs`.
- Point to shared automation in `open-cli-collective/.github` when automation
  conventions matter.
- Use the GitHub source-of-truth plus optional local convenience path pattern.
- Stay short enough to read before doing work.

They MUST NOT:

- Copy shared standards prose.
- Make one agent entrypoint depend on another, such as `AGENTS.md` pointing to
  `CLAUDE.md`.
- Describe conventions as belonging to one vendor or model.
- Accumulate stale command, release, or CI details that belong in the repo-local
  guide or canonical standards.

For monorepos or nested tools, add tool-local entrypoints only where they reduce
navigation cost. The same index-only rule applies at every depth.

---

## §3 Implementing a new CLI

An agent implementing a new CLI starts from the standards index, not from the
nearest existing repo that happens to look similar.

Required sequence:

1. Read `docs/README.md` and identify the behavior-axis and repo-axis docs that
   apply to the work.
2. Create the standard repo skeleton from `repo-layout.md`.
3. Add `AGENTS.md` and `CLAUDE.md` as indexes, and `docs/development.md` for
   repo-local facts, not copied shared policy.
4. Implement runtime behavior against the behavior-axis docs.
5. Implement CI, release, distribution, and required files against the repo-axis
   docs.
6. Verify through the repo's standard Makefile and CI contracts.

When a standard leaves a choice open, choose the local pattern that keeps the CLI
closest to the rest of the family. If a new CLI genuinely needs a new pattern,
document the reason in the local guide and consider whether the shared standard
needs an update.

---

## §4 Changing an existing CLI

Existing CLIs may diverge from target state. Agents MUST inspect current behavior
before claiming compliance or deleting local guidance.

For each change:

- Read the relevant standards and repo-local guide before touching code.
- Verify actual behavior in the codebase when a divergence note might be stale.
- Preserve unrelated divergences unless the ticket explicitly closes them.
- Update local guidance only with facts specific to that repo or tool.
- Prefer linking to shared standards over copying policy text.
- Keep generated PRs, commits, and release notes free of agent provenance.

Do not write "this repo follows the standard" unless the touched surface was
checked. A narrower statement like "new commands should follow
`command-surface.md`" is safer than overstating legacy conformance.

---

## §5 Feedback loops and enforcement

The right response to a repeated agent mistake is not a longer prompt. Promote
the correction into the repo.

Use the lowest durable enforcement layer that fits:

| Failure mode | Durable response |
|---|---|
| Agent cannot find the right rule | Add or fix an index link. |
| Rule is present but ambiguous | Clarify the standard or repo-local guide. |
| Rule is repeated across repos | Move it to `cli-common/docs` or `.github`. |
| Rule is mechanical | Add a test, lint, Make target, CI job, or script. |
| Rule requires shared code | Add or extend a `cli-common` helper. |
| Rule is repo-specific | Keep it in `docs/development.md` or a tool-local equivalent. |

Mechanical checks are preferred for invariants that repeatedly affect releases,
credential safety, output contracts, generated artifacts, PR titles, source-link
shape, or CI behavior. Documentation still matters, but prose should not be the
only guardrail for a failure mode that can be tested cheaply.

---

## §6 Local guidance review checklist

When reviewing agent-facing guidance in a CLI repo, check:

- `AGENTS.md` and `CLAUDE.md` are peer indexes.
- Shared standards references use GitHub source-of-truth links.
- Optional local convenience paths resolve from the file that contains them.
- Repo-specific details live in `docs/development.md` or a tool-local equivalent.
- Shared automation is referenced as implementation, not copied as policy.
- Vendor names appear only where naming a compatibility entrypoint or external
  tool is unavoidable.
- Repeated mistakes are backed by docs or checks rather than tribal memory.
