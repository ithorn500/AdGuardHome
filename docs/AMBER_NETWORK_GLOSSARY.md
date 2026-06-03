# Amber Network Glossary

**Status:** Canonical cross-repo naming glossary.  
**Last updated:** 2026-05-24.  
**Canonical source:** `/opt/AIGateway/docs/amber-network/amber-network-glossary.md`.

This glossary records the product names, daemon names, protocols, lanes, and boundary terms used
across the Amber Network repos. Repo-local copies may mirror this file for convenience, but the
AIGateway command-repo copy is the source of truth for cross-repo naming.

## Estate Names

| Name | What it means / does |
|---|---|
| **Amber Network** | The full local ecosystem spanning Gemma Gateway, Guardian, Amber Bus, Logger, Actorr, Home Assistant, AdGuardHome, and pfSense. |
| **AIGateway / Gemma Gateway** | The command repo and AI appliance. Owns fixed LLM lanes, Gateway Ops, Veliai Media, Guardian-facing APIs, memory/retrieval, and native inference surfaces. |
| **Gemma** | The model/runtime identity behind local reasoning, chat, structured generation, and Guardian advisory tasks. |
| **Guardian** | Deterministic policy/action layer. Owns C2, tablets/wallpanels, Device Management, home/security policy, and real-world actuation decisions. |
| **Amber Bus** | Discovery, catalog, contract, graph, invocation, and runtime integration spine for Amber applications. |
| **Logger / SignalScope Logger** | Central observability service for log ingestion, correlation, incidents, live streams, metrics, and operator dashboards. |
| **Actorr** | Independent media semantic actuator for IPTV, VOD, EPG, Xtream provider integration, relay, transcode, and client bootstrap. |
| **Home Assistant** | Live HA instance/config and device/entity/telemetry fabric consumed by Guardian. It is not the primary orchestration codebase. |
| **AdGuardHome** | LAN DNS/filtering security perimeter. Amber integration is read-only first and Bus-native. |
| **pfSense** | Firewall/router perimeter authority for routing, VLANs, NAT, gateway, VPN, DHCP where assigned, and packet enforcement. |

## Veliai / Gateway Names

| Name | What it means / does |
|---|---|
| **Veliai** | Wider local intelligence system: Gateway intelligence, Guardian, media perception, learning, memory, and Amber Bus integration. |
| **Veliai Intelligence** | Product/architecture name for native inference and reasoning. Current code still exposes the implementation as `gemma_engine`. |
| **`gemma_engine`** | C++/pybind native engine implementation under `native/engine/`. Keep this code symbol until a compatibility-safe rename is planned. |
| **Veliai_Manager** | Operator-visible truth for lane state, worker status, hardware health, backend status, and central logging/control-plane surfaces. |
| **`gg-beast-edge`** | C++ Boost.Beast front door. Owns compat ports, Ops portal, Guardian routes, static UI serving, lane queues, Veliai Media hosting, and Veliai-learning hosting. |
| **`gg-process-manager`** | C++ worker supervisor. Reads `deploy/model-registry.yaml`, spawns lane workers, writes `workers.json`, and exposes the process-manager control socket. |
| **`llama-server` worker** | Per-lane inference process built from the local `vendor/llama.cpp` fork. |
| **EngineRunner** | Process-manager abstraction for selectable backend workers such as `llama_cpp` and future `ollama`. |
| **Ops portal** | Gateway operator UI/API on `:11430`, including `/ui/ops`, `/api/v1/ops/*`, and `/api/gg/ops/*`. |
| **Compat listener** | Public OpenAI/Ollama-style entry port: `:11434` chat, `:11435` embeddings, `:11436` fast. |
| **Worker port** | Loopback lane port: `:21434` chat, `:21435` embeddings, `:21436` fast. |
| **chat lane** | Main reasoning lane: `:11434 -> :21434`, Gemma 4 31B on CUDA / RTX 3090. |
| **embed lane** | Embedding lane: `:11435 -> :21435`, BGE-M3 on HIP / AMD 890M. |
| **fast lane** | Low-latency small-model lane: `:11436 -> :21436`, Gemma 4B-class fast profile on HIP / AMD 890M. |
| **Model registry** | `deploy/model-registry.yaml`; source of truth for profiles, devices, model paths, mmproj pairing, and routing intent. |
| **`workers.json`** | `/opt/AIGateway/data/run/workers.json`; live worker state written by `gg-process-manager`. |
| **mmproj** | Multimodal projector paired with text lanes for vision/audio prefill. NPU projector execution must be payload-proven before green status. |
| **Ollama backend** | Future/selectable Gateway-owned backend option. It must keep lane/device policy parity with llama.cpp and fail closed on policy violations. |

## Veliai Media / Vision / Learning

| Name | What it means / does |
|---|---|
| **Veliai Media** | Audio/video/NVR/perception subsystem hosted by `gg-beast-edge`: camera/audio ingest, live views, motion, object/person/face/voice events, clips, review, retention, and training packs. |
| **Veliai-Vision** | Live vision/source path inside Veliai Media, including camera and wallpanel media/liveness handoff into the media subsystem. |
| **Veliai-learning** | In-process learning/consolidation surface hosted by `gg-beast-edge`: YOLO dataset export now, future memory repair, context gathering, validation, and promotion gates. |
| **Ingest fast path** | Camera/audio stream path optimized for live views: decode once, keep latest frame/packet state, and serve UI without waiting for inference. |
| **Perception slow path** | Object/person/face/plate/voice analysis path where accuracy matters more than rendering every frame. |
| **Motion gate** | Cheap downscaled frame-difference pass that decides whether heavier perception should sample a frame. |
| **Object path** | Detection path for physical things such as cars, deliveries, mower, dogs, birds, and other objects. YOLO is the canonical classifier family. |
| **Person path** | Body/person detection path. A person can trigger face sampling, but the person label itself is not identity. |
| **Face path** | Face detection/cropping path fed by person detections and still-frame sampling. |
| **Identity path** | Recognition path that links face and voice evidence to stable person/profile records used by Guardian/Gemma. |
| **Voice path** | Audio detection path for speech, wake, and voice candidate events. |
| **Voice print path** | Speaker fingerprint path that confirms or rejects who is speaking. |
| **Plate path** | Vehicle plate candidate/OCR path. It should not override object identity without explicit verification. |
| **Zone** | Operator-defined spatial area used to filter, name, or prioritize detections. |
| **Mask** | Operator-defined ignored area used to suppress known false positives. |
| **Training pack** | Operator-curated examples for a face, voice, object, or class. |
| **Review item** | UI-visible event/candidate requiring operator decision: seen, false positive, train, export, mask, or reallocate. |
| **Evidence export** | Clip/snapshot/manifest bundle for a time window or event. |
| **Retention policy** | Rules deciding how long clips, snapshots, motion windows, review items, and continuous segments remain under `/Data/Veliai`. |
| **YOLO dataset export** | Veliai-learning batch output under `/Data/Veliai/nvr/training/yolo/<dataset-id>/` with images, labels, `data.yaml`, classes, and manifest. |
| **Model promotion** | Replacing a live model with a trained candidate. This remains gated by validation and operator approval until automated gates are mature. |

## Veliai Compute / Grid

| Name | What it means / does |
|---|---|
| **Veliai-Compute** | Native compute-offload node model for Veliai Media workloads such as frame preprocessing, motion gates, evidence batches, and future detector execution. |
| **Velia-Grid** | Low-latency local protocol between Veliai Media clients and Veliai-Compute nodes. It carries admission, leases, status, references, and compact results; it is not a video bus or Guardian policy API. |
| **Guardian Managed Compute** | Guardian Device Management registry/control surface for Veliai-Compute nodes, desired state, heartbeats, drain/resume, and runtime config. |
| **frame/tensor/audio reference** | Compact hot-path references such as `frame_ref`, `tensor_ref`, or `audio_ref`; media bytes stay in frame rings, shared memory, mmap, or evidence stores. |
| **lease** | Short-lived Velia-Grid admission grant for realtime or near-realtime compute. |
| **source_subscribe** | Planned Velia-Grid flow where a compute node subscribes directly to Guardian-authorized media sources so Veliai Media does not become a fanout bottleneck. |

## Memory / Retrieval Names

| Name | What it means / does |
|---|---|
| **RAG** | Retrieval augmented generation: pulling relevant memory, document, or vector context into prompts. |
| **Mem0** | Memory layer for user/profile/agent memory where enabled by the wider Veliai stack. |
| **Qdrant** | Vector database target for semantic retrieval and profile/context search. |
| **Neo4j** | Graph database target used by Guardian and Amber Bus graph projections with namespace separation. |
| **Profile** | Stable identity/person/object/voice record linking face samples, voice prints, memory, permissions, and Guardian policy. |
| **Context pack** | Validated bundle of relevant facts/evidence prepared for Guardian/Gemma decisions. |
| **Golden pin** | Durable/high-confidence memory item that should be preserved and surfaced as trusted context. |

## Amber Bus Names

| Name | What it means / does |
|---|---|
| **ABOS** | Amber Bus Open Standard draft foundation. |
| **DEMand** | Distributed Edge Messaging and delivery. Required native runtime protocol for application-to-Amber-Bus traffic. |
| **Native-Client** | App-side native Amber Bus client participating in DEMand runtime exchange. |
| **FastIO** | Amber Bus fast local runtime/socket support, typically `/run/amber-bus/fastio.sock`. |
| **Native dataplane** | C++ blob, queue, stream, ACK, replay, and payload endpoints for large or long-running traffic. |
| **HTTP bootstrap** | `/.well-known/*`, manifest downloads, install artifacts, and operator endpoints. These are not the production app-runtime transport. |
| **Functionality catalog** | Bus-readable catalog of callable functions such as `guardian.tablet.heartbeat`, `logger.log.ingest`, and `aigateway.veliai_media.session.create`. |
| **Interface catalog** | Bus-readable list of app interfaces, contracts, paths, topics, and stability. |
| **App manifest** | App-owned Bus identity document with `app_id`, display name, capabilities, transports, interfaces, and functions. |
| **Migration mode** | Transitional app mode where direct APIs may still exist while the Bus path is added. |
| **Production mode** | Target app mode where cross-app consumption is Bus-only except explicitly allowed direct data-plane paths. |
| **MCP bridge** | Developer/tooling bridge exposing Bus apps, capabilities, bindings, logs, traffic, health, and guarded dry-run invoke. It is not a hidden production daemon. |
| **Ops Console** | Consumer-only Bus verification console. |

## Guardian Names

| Name | What it means / does |
|---|---|
| **`guardian_core`** | Native Guardian runtime package under `/opt/guardian/src/guardian_core`. Starts apps, scheduler, C2, trigger server, HA event bridge, and control-plane tasks. |
| **Guardian C2** | Operator/control deck and HTTP live feed/actions for Guardian management. |
| **Device Management** | Guardian source of truth for cameras, wallpanels/tablets, compute nodes, desired state, update intent, and commissioning. |
| **Guardian tablet** | Android-managed wall/tablet appliance client. Built in AIGateway, published through Guardian release artifacts, governed by Guardian desired state. |
| **Wallpanel** | Guardian-managed display endpoint. Android tablets are wallpanel implementations, not a separate policy plane. |
| **`HaControlPlane`** | Guardian control loop that reads HA state, runs policy/optimizer logic, and calls HA services through guarded paths. |
| **`StateCache`** | Guardian mirror of HA entity state populated at startup and updated from HA events. |
| **`GuardianActuationGateway`** | Deduplication and audit wrapper for HA service calls. |
| **`GuardianScheduler`** | Central fixed-rate timer, state listener, and event dispatch scheduler inside `guardian_core`. |
| **`guardian_bus`** | In-process async pub/sub bus for Guardian app-to-app events. |
| **OMEGA** | Guardian/HA strategy and executor family for house/energy decisions. Current runtime must stay governed by Guardian policy and safety gates. |
| **Jarvis** | Current wake/head/voice UX identity. Direction is to preserve existing Jarvis behavior while Guardian wake naming/training matures. |
| **EcoFlow / EcoStream Ultra** | Energy hardware/integration domain with guarded command paths, especially around AC outlet/task switches and bounded numeric controls. |
| **CentralLogBusApp** | Guardian central-log/file-plane choke point and bounded transient cache for scoped Guardian control-plane artifacts. |
| **Feature switch inventory** | Machine-readable inventory of live/off/shadow/config-only Guardian gates. |
| **Guardian Gemma tool bridge** | Governed path where Gemma tool proposals flow through Guardian policy and Amber Bus instead of directly actuating devices. |

## Actorr Names

| Name | What it means / does |
|---|---|
| **Actorr Portal** | Web UI/API for provider credentials, settings, relay status, EPG, curation, diagnostics, and client rollout. |
| **semantic actuator** | Actorr's smart shaping layer between provider feeds and local media systems. |
| **Velox** | Actorr native media engine family for stream open, probe, relay, copy, transcode, and direct serve paths. |
| **`veloxd`** | Native Velox daemon for session, network, metrics, live playback, and VOD playback. |
| **Veilox** | Actorr native VPN / WireGuard feature family. |
| **`veiloxd`** | Native VPN daemon for WireGuard config parsing, dry-run/apply planning, health, and status APIs. |
| **Actorr Client** | Host-side runtime installed on the media server/client machine. |
| **`actorr_clientd`** | Client daemon that manages local mounted view and connection back to Actorr. |
| **Nexus** | Actorr media visor / virtualization runtime. |
| **`actorr_nexusd`** | Nexus daemon publishing snapshot/state and managing the virtual media namespace. |
| **Fabric** | Actorr control-plane and orchestration layer. |
| **`actorr_fabricd`** | Fabric daemon for broader native runtime policy/orchestration. |
| **media hypervisor** | Actorr architecture label for Client + Nexus + Fabric mounted-library virtualization. |
| **relay / stream relay** | Stable local playback URL while Actorr fetches provider streams in the background. |
| **copy mode** | Relay mode with minimal transformation when direct compatibility is possible. |
| **transcode mode** | Re-encoding mode used when a client or source stream requires safer output. |
| **native HLS relay** | Native-first segment relay path for straightforward HLS serving. |
| **STRM output** | `.strm` pointer files for Emby/Jellyfin-style libraries. |
| **Curator** | Content selection and allow-list layer. |
| **Content Manifest** | Approved live stream, VOD, series, and episode IDs. |
| **Vault map** | Provider item to friendly local name / LCN mapping. |
| **STRM repository** | Local disk or SMB output layer for library pointer files. |
| **Provider registry** | Upstream media provider metadata and configuration registry. |
| **Xtream client service** | Provider integration service for Xtream-compatible live/VOD/series catalogs. |
| **HDHomeRun mock** | Tuner-style emulation so Emby/Plex can discover Actorr as live TV. |
| **EPG** | Electronic Program Guide pipeline. |
| **XMLTV** | XML guide source/output format used by the EPG pipeline. |
| **Sky mapping** | UK channel mapping layer aligning upstream channels to Sky-style names and LCNs. |

## Logger Names

| Name | What it means / does |
|---|---|
| **SignalScope Logger** | Full Logger product name and central observability service. |
| **native logger service** | Supported deployed C++ Logger runtime against `data/logs.db`. |
| **`logger.log.ingest`** | Amber Bus function for normalized log ingestion. |
| **`logger.correlation.query`** | Amber Bus function for grouped incident/correlation reads. |
| **`logger.incident.stream`** | Amber Bus function for live operator streams. |
| **`logger.operator.control`** | Logger operations surface for saved views, incident updates, collector reloads, retention, and health. |
| **ingest channel** | Logger tag showing whether an event arrived through `direct` provider-local HTTP or `amber-bus`. |
| **correlation** | Grouping related log events into incident-style records. |
| **service silence** | Logger check for expected services that have stopped reporting. |

## Perimeter Names

| Name | What it means / does |
|---|---|
| **AdGuardHome connector** | Read-only Amber Bus provider surface planned/implemented in the AdGuardHome Go fork for DNS/filtering status, stats, clients, query-log search, and security summary. |
| **DNS perimeter** | AdGuardHome-owned LAN DNS/filtering authority and security signal boundary. |
| **Query-log search** | Bounded, privacy-conscious AdGuardHome forensic query function; raw DNS history should not be routinely mirrored into Logger. |
| **pfSense connector** | Read-only-first Amber Bus surface for firewall/router inventory, interfaces, gateways, DHCP leases where assigned, VPN status, rule inventory, and high-signal events. |
| **packet-enforcement authority** | pfSense's boundary: Guardian may express policy intent, but pfSense owns firewall/router enforcement. |

## Home Assistant Names

| Name | What it means / does |
|---|---|
| **HA entity model** | Home Assistant entities, states, devices, integrations, and service-call surface. Guardian consumes this as telemetry/device fabric. |
| **Live HA config** | `/mnt/homeassistant`; the live Home Assistant configuration and telemetry boundary, not a Home Assistant core/frontend product fork. |
| **HA Core / HA frontend forks** | Separate product forks when mounted or worked through GitHub. Do not confuse them with `/mnt/homeassistant` live config. |
| **Custom integration fork** | Product/custom-integration repos such as Guardian HA integration, Bestway, EcoFlow BLE, and Mammotion. Deploy into HA only when explicitly requested. |

## Common Boundary Rules

| Rule | Meaning |
|---|---|
| **Gateway is the head/brain** | AIGateway owns intelligence, LLM lanes, memory, perception, and Guardian-facing Gateway APIs. |
| **Guardian is arms/legs** | Guardian owns deterministic policy and final actuation decisions. |
| **Amber Bus is spine** | Cross-app discovery, contracts, invoke, graph, and runtime messaging should flow through Amber Bus. |
| **Logger is sink** | High-signal app/runtime events should reach SignalScope Logger, preferably through Amber Bus. |
| **Home Assistant is device fabric** | HA remains the live entity/telemetry/integration surface Guardian consumes; new orchestration belongs in Guardian unless explicitly scoped otherwise. |
| **Actorr is independent** | Actorr may integrate through Bus/manager/tooling edges, but do not force Guardian patterns into its media data-plane. |
| **AdGuardHome and pfSense own perimeters** | Guardian may consume signals and express policy intent, but DNS/filtering/firewall enforcement remains with those perimeter systems. |
| **No second Gateway engine** | The active Gateway appliance is the monolith: `gg-beast-edge`, `gg-process-manager`, lane workers, and native surfaces. |
| **UI is browser, native is C++/engine** | Gateway UI is a browser SPA/static UI. "Native" means C++/`gemma_engine`/llama inference paths, not Svelte/browser UI. |
| **Fixed compat lanes stay meaningful** | `:11434` chat, `:11435` embed, `:11436` fast. If `:11436` fails, the fast profile/worker is not loaded or healthy; the port itself is defined. |
| **No CPU fallback for GPU lanes** | GPU lane intent must stay strict unless the operator explicitly authorizes a different policy. |
| **Media bytes are special** | Bus carries control, descriptors, manifests, and small evidence. Continuous video/audio bytes and large clips use direct/provider-native data paths. |

## Repo Locations

| Repo | Path | Glossary mirror |
|---|---|---|
| AIGateway / Gemma Gateway | `/opt/AIGateway` | `docs/amber-network/amber-network-glossary.md` |
| Guardian | `/mnt/guardian` | `src/docs/AMBER_NETWORK_GLOSSARY.md` |
| Amber Bus | `/mnt/amber-bus` | `docs/AMBER_NETWORK_GLOSSARY.md` |
| Actorr | `/mnt/actorr` | `AMBER_NETWORK_GLOSSARY.md` |
| Logger | `/mnt/logger` | `docs/AMBER_NETWORK_GLOSSARY.md` |
| AdGuardHome | `/mnt/adguard` | `docs/AMBER_NETWORK_GLOSSARY.md` |
| pfSense | `/mnt/pfsense` | `docs/AMBER_NETWORK_GLOSSARY.md` |
| Home Assistant live config | `/mnt/homeassistant` | `docs/AMBER_NETWORK_GLOSSARY.md` |
