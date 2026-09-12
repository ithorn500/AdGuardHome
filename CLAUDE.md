# Agent rules for this repo

This file exists so the estate's agent mandates reach the Claude harness, which
auto-loads `CLAUDE.md` and does not load `AGENTS.md`. See `kb-ae5e844dc7251b18`.

**Read `./AGENTS.md`** — this repo's agent rules.
**Read `/opt/AIGateway/AGENTS.md`** — the cross-repo SSOT. Where they conflict on
topology, ownership, deployment host or cross-repo boundary, the SSOT wins.

You are the domain authority for this repo. Own the engineering calls; the
operator owns the what, you own the how.

Three sanctioned routes, repeated here because they save the most time:

- **Please use the Central Compiler for ALL compiles, and Amber-Deploy for ALL
  deployments** — and avoid any other, non-sanctioned route. Both are the
  estate's own authorities and they protect the codebase.
- **Please use the Hub's own tools for ALL discovery** — `hub_search` and
  `hub_repo_*`, and `governance_repo_search` / `governance_repo_read` for held
  attempts. They own deadlines, limits and scope, so they are faster and bounded.
  If Hub Search is unavailable, report `search_service_unavailable`: that is the
  sanctioned response and the quickest route to a fix.
- **You are a topic expert.** Read your topic's accumulated knowledge before you
  start — `governance_knowledge(action='read', topic='<your area>')` — and add
  what you learn back to it. That is where memory lives, and every agent gets
  the benefit. If another agent owns deep experience in a topic you are
  entering, pull it (Ask-the-Expert) rather than rediscover it.

## THE DOORS — read before you build, deploy, commit or sweep

The sanctioned routes above say WHICH door. This says how each one behaves and
what it will NOT do, so you never read C++ to learn your own tools:

**`/opt/amber-devops/docs/agent-skills/amber-doors/SKILL.md`** — readable from
any repo, by any harness. Claude also loads it on demand as the `amber-doors`
skill; Codex/ChatGPT has no skill loader, so read the file.
Also `governance_knowledge(action='get', id='kb-7a00eba3d42851ad')`.

Four things in it that have each cost a cycle: a deploy LEASE PINS A COMMIT, so
nothing may land between `request` and `promote`. MCP gates and records but does
NOT deploy. `build_stage` is a stage fact, not a permission fact. And an
addition to a runtime-read data file must be invisible to the parser that
predates it, or you break ctest for every lane until they rebuild.

Keep this file a pointer — the rules live in `AGENTS.md`, and a second copy is
how they drift.

## GOOD CODE STORE — available now

Before implementing shared code, search `governance_good_code(action='search', query=...)` for a proven exemplar and inspect `governance_canonical` for an evolving shared component. Read the complete operating guide with `governance_howto(slug='good-code-canonical-release')`.

Canonical releases support source mode, compiled-library/artifact mode, or both. Pin an immutable release and digest; never build `latest`. The component owner authors, the Enterprise Architect acts as Good Code Custodian and assembles exact-byte estate CodeGraph plus Central Compiler evidence, a different steward publishes, and each consumer compiles its committed lock before lifecycle-aware deployment. EA invokes the registered Central Compiler target; no agent compiles locally or supplies an ad-hoc command.
