# Guidance for coding agents

This root is the current AdGuardHome LXC runtime mount. It contains the active AdGuardHome binary,
configuration, data, and upstream documentation. It is not currently a Go source checkout.

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

## Fork And Connector Policy

- The source fork should be a separate git root/workspace when mounted, preferably with upstream
  AdGuardHome kept trackable.
- Amber changes in the Go fork should stay narrow, ideally under `internal/amberbus/` or
  `internal/amberbusconnector/`.
- The first connector slice is read-only: status, stats, query-log search, clients, filtering
  status, and security summary.
- Guarded write functions require a separate explicit contract update and operator approval.
