# Agent skills guidelines

Rules for writing and maintaining agent skills in this repository. To review a skill, ask your agent
to check it against these rules.

Every skill must comply with the [Agent Skills specification](https://agentskills.io/specification).
This file only adds the rules specific to this repository.

## Location

* Skills live in `.agents/skills/<skill-name>/`, where Codex discovers them.
* `.claude/skills` is a symlink to `.agents/skills` so Claude Code finds the same skills.
* Keep skills exactly one level deep. Claude Code does not discover skills in nested directories.

## Naming and license

* Prefix every public skill name with `dpf-`, so names stay unique if we publish skills to a
  central catalog.
* Skills for internal infrastructure (CI, test setups) use the `internal-` prefix. They are
  excluded from the public mirror.
* Never put internal hostnames, credentials or internal-only workflows into other skills.
* Set `license: Apache-2.0` in the frontmatter.

## Content

* **No business logic.** A skill tells the agent where to look and how to proceed. It does not
  copy how DPF works. Point to the code, API types, docs and Helm charts instead of restating them.
  This costs some tokens, but a skill that only points to sources does not go stale when the code
  changes.
* **Be version-aware.** If the answer depends on the DPF version, have the skill find the
  installed version first and read the docs or code for that version, from the local checkout or
  the matching `vX.Y.Z` tag on [GitHub](https://github.com/NVIDIA/doca-platform). Fall back to the
  `public-release-vX.Y` branch only when there is no tag, because the branch moves on after GA.
* **Read-only by default.** A skill only reads, unless its `description` says that it changes a
  cluster or system.
* **Confirm every change.** Before each change, a skill shows the context (for example the kube
  context), the exact command and its impact, and waits for the user to confirm. One confirmation
  covers one change.
* **Run changes only on request.** A skill that changes a cluster or system runs only when the user
  asks for it by name. Set `disable-model-invocation: true` in the frontmatter for Claude Code and
  `policy.allow_implicit_invocation: false` in the skill's `agents/openai.yaml` for Codex, which
  ignores the frontmatter field.
* **Avoid overlap.** Before adding a skill, check whether an existing one should be extended. If
  one skill needs another, refer to it instead of copying its content.
* **Human-curated.** Drafting with an agent is fine, but a human must curate and review the
  result. Agent-generated skills give no benefit on average
  ([SkillsBench](https://arxiv.org/abs/2602.12670)).

## Ownership

Every skill has owners who know its background and review its changes. If necessary, add the
skill directory to `CODEOWNERS` with a small group of people, not the whole project.

## Review

* Review skills like code.
* Validate skills against the specification with
  [skills-ref](https://github.com/agentskills/agentskills/tree/main/skills-ref):
  `skills-ref validate .agents/skills/<skill-name>`.
