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

Keep this file a pointer — the rules live in `AGENTS.md`, and a second copy is
how they drift.
