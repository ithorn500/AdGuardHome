# Measure Twice, Cut Once (Mandatory)

This repo follows the cross-repo agent mandate: **measure twice, cut once**. Before editing, agents must do a read-only evidence pass, define the problem and affected paths, record the design and validation plan, then make one coherent change set. No panic patching, speculative micro-fixes, or isolated edits when behavior spans components.

# Guidance for coding agents

This root is the current AdGuardHome fork source workspace at `/opt/adguard`; `/mnt/adguard` is
runtime mirror/payload only. It tracks `origin=https://github.com/ithorn500/AdGuardHome.git` and
`upstream=https://github.com/AdguardTeam/AdGuardHome.git`.

The cross-repo source of truth is `/opt/AIGateway/AGENTS.md`. This file is the AdGuardHome-local
overlay for DNS/filtering fork and runtime payload rules.

## ACSA Console Surface Mandate

Every AdGuard/edge-network functionality designed or developed must have a live graphical surface in both the Amber Console Windows app and the Console web app on `console.amber.com`. Delivery evidence must include owner-backed state, activity, errors, lifecycle mode, and click-through detail. Text-only status boxes, raw JSON dumps, static mock panels, hidden flags, and fake counters do not count.

## Amber Network Host/IP Reference

Use DNS names for service operations and SSH commands; use IPs only for orientation, diagnostics,
or DNS work.

| Host / workspace | Address | Notes |
| --- | --- | --- |
| `gemma.amber.com` / `/opt/AIGateway` | `192.168.0.48` | Command Repo and Gemma Gateway appliance. |
| `guardian.amber.com` / `/opt/guardian` | `192.168.0.47` | Guardian control plane/runtime owner; source repo is `/opt/guardian`. |
| `amber-bus.amber.com` / `/opt/amber-bus` | `192.168.0.45` | Amber Bus owner host; source repo is `/opt/amber-bus`. |
| `logger.amber.com` / `/opt/logger` | `192.168.0.45` | Logger resolves to the Amber Bus host address; source repo is `/opt/logger`. |
| `adguard.amber.com` / `/opt/adguard` | `192.168.0.4` | DNS/filtering perimeter; source repo is `/opt/adguard`. |
| `pfsense.amber.com` / `/mnt/pfsense` | `192.168.0.5` | Firewall/router perimeter workspace. |
| `actorr.amber.com` / `/opt/actorr` | `192.168.0.49` | Actorr media actuator owner host; source repo is `/opt/actorr`. |
| `homeassistant.amber.com` / `/mnt/homeassistant` | `192.168.0.164` | Live Home Assistant config/telemetry host. |
| `homeassistant.local` | unresolved from AIGateway on 2026-05-24 | Prefer `homeassistant.amber.com` when resolution from AIGateway matters. |
| `memorr.amber.com` / `/opt/memorr` | `192.168.0.46` | Memorr owner host; source repo is `/opt/memorr`. |
| `adserver.amber.com` | `192.168.0.51` | Windows AD DNS/DHCP authority, not a mounted repo owner. |

## Boundary

- AdGuardHome is the LAN DNS/filtering security perimeter.
- Treat it as a separate Amber application, not a Guardian-owned adapter.
- Guardian may consume security policy signals through Amber Bus, but Guardian does not own
  AdGuardHome's process, DNS authority, or `/control/*` API surface.
- Logger is the correlation sink for high-signal AdGuard events.
- Amber Bus is the discovery, manifest, contract, and invoke layer.

## Runtime Rules

- Do not edit `AdGuardHome.yaml`, `data/`, DHCP settings, DNS listeners, rewrites, upstreams,
  filters, or protection state unless the user explicitly asks for that exact live runtime change.
- Do not restart the LXC/process from this host without first confirming the owning host and the
  smallest safe restart path.
- Do not replace the `AdGuardHome` binary from this runtime mount as a side effect of documentation
  or connector planning work.
- Treat `AdGuardHome`, `AdGuardHome.yaml`, `data/`, and backup/runtime payloads as live runtime
  artifacts unless the user explicitly asks for runtime maintenance.

## Fork And Connector Policy

- The Go source fork is present in this git root. Preserve upstream layout and keep fork changes
  narrow.
- Amber changes in the Go fork should stay narrow, ideally under `internal/amberbus/` or
  `internal/amberbusconnector/`.
- The first connector slice is read-only: status, stats, query-log search, clients, filtering
  status, and security summary.
- Guarded write functions require a separate explicit contract update and operator approval.

## Mandatory Change Process

For non-trivial work across AdGuardHome fork code, DNS/filtering behavior, connector contracts, runtime payloads, or Bus/Logger integration:

1. Read-only evidence pass.
2. Design record before editing.
3. One coherent strategic change set.
4. Validation pass covering success and failure.
5. Go/no-go review.
6. No panic patch rule.

Task-specific change-process notes must identify affected runtime paths, source modules, APIs/IPC, schemas/state, UI/Bus surfaces, tests, deployment impact, and explicit success/fail criteria before implementation.

Do not slip isolated fixes into one file and then chase failures. If validation fails, stop, update the evidence/design, and make one revised coherent change set.
