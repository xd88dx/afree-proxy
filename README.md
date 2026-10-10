# AFree Proxy

A single Go binary that turns free upstream LLM quotas (Cline accounts + opencode zen free models + WorkBuddy accounts) into one clean **OpenAI-compatible `/v1` gateway** for coding IDEs — Cursor, ZCode, Cline, Claude Code, OpenClaw — with account pooling, per-request proxy rotation, and an English admin panel.

Fork-in-progress of [YuJunZhiXue/Cline-proxy](https://github.com/YuJunZhiXue/Cline-proxy), reworked for solo public deployment. This project is not affiliated with Cline, opencode, Tencent, or WorkBuddy; use their free tiers respectfully.

## What it does

```
Cursor / ZCode / Cline / Claude Code ──►  afree-proxy  ──►  cline account pool (round-robin)
   /v1/chat/completions                        │
   /v1/messages (Anthropic)                    ├─►  opencode zen free models (multi-key, proxy pool)
   /v1/responses (OpenAI Responses)            ├─►  WorkBuddy accounts (OAuth, pool, scheduler)
   /v1/models                                  └─►  egress socks5/http proxy pool (rotation or identity binding)
```

- **One endpoint, all upstreams.** The gateway inspects the requested `model` and routes it: `cn:` / `global:` models go to WorkBuddy, zen free models (`mimo-v2.5-free`, `nemotron-3-ultra-free`, …) go to opencode zen, paid zen models are rejected with a clear 400, and everything else goes to the Cline account pool. Combos let you define your own alias model IDs on the supported platforms.
- **WorkBuddy inside the same gateway.** The `OpenCode → WorkBuddy` page embeds the upstream panel for OAuth account login, account pooling, breaker/cooldown state, scheduled check-in/activity/travel/keepalive jobs, streaming and non-streaming chat, reasoning-model compatibility, and fingerprint sanitization. WorkBuddy models use explicit `cn:` / `global:` IDs so bare model names keep their existing Cline/OpenCode routes.
- **Three client dialects, one upstream language.** All upstreams are OpenAI chat-completions servers. `/v1/chat/completions` is a near-passthrough; `/v1/responses` and `/v1/messages` (Anthropic) are translated in and converted back, including streaming events, tool calls, and usage.
- **IDE-controlled parameters are respected.** `max_tokens`, `temperature`, `top_p`, `stop`, `tools`, `tool_choice`, `reasoning_effort`, … all pass through from the client. The gateway only fills gaps (128k default output budget when the client sends none) and enforces semantics upstreams get wrong: `tool_choice: "none"` is enforced by stripping tools, unknown model names return a clean 400 (`STRICT_MODEL_MATCH=false` restores the old catch-all), and `stop` sequences are deterministically truncated client-side for non-stream responses.
- **Pool everything.** Cline accounts round-robin with automatic 429 cooldown ("Try again in 17h 59m" parsing) and auto-recovery. Zen keys and WorkBuddy accounts use their own independent pools with cooldown/breaker handling. Egress proxies rotate per request so IP-based rate limits don't bottleneck one address; proxy failures never poison account state. **Proxy isolation** (on by default) upgrades this to fixed identity↔exit binding: a bound Cline account, zen key, or WorkBuddy account always egresses via its own main/backup proxy and is skipped entirely when both are unavailable — see [Proxy isolation](#proxy-isolation-identity--exit-binding).
- **Function calling that actually works.** Battle-tested against live upstreams across chat stream/non-stream, parallel tool calls, `/v1/responses` and Anthropic streaming, with repair logic for upstream tool-argument quirks. See the test matrix below.
- **English admin panel** at `/admin/`: Dashboard, Accounts, Add accounts (OAuth / refreshToken / static `sk_` API key — auto-detected, also batch), Gateway settings (API keys, models, headers), **Proxy pool** (one shared socks5/http egress list with per-upstream toggles for Cline and zen), opencode free models, **WorkBuddy**, Combos, request logs, live stats.

## Quick start (Docker)

```bash
docker run -d --name afree-proxy \
  -p 3457:3457 \
  -v cline-proxy-data:/app/data \
  -e PORT=3457 \
  -e API_KEY=change-me \
  -e ADMIN_PASSWORD=change-me-too \
  ghcr.io/foxy1402/cline-proxy:latest

curl http://127.0.0.1:3457/health
```

Then open `http://127.0.0.1:3457/admin/`, log in with `ADMIN_PASSWORD`, and add accounts or zen keys. The image is multi-arch (`linux/amd64`, `linux/arm64`); `:latest` is published on every build, and version tags (`vX.Y.Z`) additionally publish `:X.Y.Z`.

> The image name follows the GitHub repository name. If your repo is named differently, replace `afree-proxy` with the repo name (GHCR images are always lowercase).

## Portainer stack template

Paste into Portainer → **Stacks → Add stack**, fill in the environment variables in the UI (Portainer interpolates them), then deploy:

```yaml
services:
  afree-proxy:
    image: ghcr.io/foxy1402/cline-proxy:latest
    container_name: afree-proxy
    restart: unless-stopped
    ports:
      - "${PROXY_PORT:-3457}:${PROXY_PORT:-3457}"
    volumes:
      - cline-proxy-data:/app/data
    environment:
      - PORT=${PROXY_PORT:-3457}
      - API_KEY=${API_KEY:?set API_KEY in the stack environment}
      - ADMIN_PASSWORD=${ADMIN_PASSWORD:?set ADMIN_PASSWORD in the stack environment}
      - ZEN_KEYS=${ZEN_KEYS:-}
      - CLINE_ACCOUNTS_SEED_FILE=/app/data/cline-seed.json
      - STRICT_MODEL_MATCH=true
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:${PROXY_PORT:-3457}/health"]
      interval: 30s
      timeout: 5s
      start_period: 10s
      retries: 3

volumes:
  cline-proxy-data:
```

Environment variables to define in the Portainer stack UI:

| Variable | Required | Example |
|---|---|---|
| `API_KEY` | yes | any long random string — the only key accepted on `/v1/*` |
| `ADMIN_PASSWORD` | yes | admin panel login password |
| `PROXY_PORT` | no | defaults to `3457`; changes both the container listen port and the host mapping |
| `ZEN_KEYS` | no | comma-separated opencode zen keys; leave empty to use the anonymous `public` key or configure in the admin panel |

To seed Cline accounts on first boot, drop a `cline-seed.json` file into the volume (see [Seeding accounts](#seeding-accounts)).

> **Bind-mount note (Portainer/NAS users):** set `PUID`/`PGID` to your host user and the entrypoint self-heals the data-directory ownership on every start (`chown` is run inside the container before the gateway drops privileges) — no manual `chown` needed, even after recreating the container. Defaults are `100`/`101` (the image's built-in `app` user). A **named volume** (`-v cline-proxy-data:/app/data`) is also fine and needs no ownership setup at all.

## Configuration

All state lives in the `/app/data` volume (`cline-accounts.json`, `zen-config.json`, `combos.json`, `requests.jsonl`, plus `workbuddy/` and `.workbuddy-proxies.json`). Backup = copy the volume.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3457` | Listen port (`-port` flag wins over the env var) |
| `DATA_DIR` | `/app/data` | State directory inside the container |
| `API_KEY` / `API_KEY_FILE` | empty | The single valid `/v1` key (via `Authorization: Bearer` or `x-api-key`). When set, admin-panel-generated keys are ignored for `/v1` |
| `ADMIN_PASSWORD` / `ADMIN_PASSWORD_FILE` | empty | Admin panel password; when set, every `/admin/*` route requires login |
| `REQUIRE_ADMIN_AUTH` | `true` | Set `false` only when a reverse proxy already handles auth |
| `PROXY_PORT` | — | Compose/Portainer convenience: sets host mapping + `PORT` together |
| `STRICT_MODEL_MATCH` | `true` | `400` for unknown model names instead of silently serving the default model |
| `POOL_STRATEGY` | `round_robin` | Cline account strategy: `round_robin` / `fill` / `random` (env wins over panel config) |
| `ZEN_KEYS` | empty | opencode zen keys, comma-separated; panel config is not overwritten when it already has keys |
| `CLINE_ACCOUNTS_SEED_FILE` | empty | Seed JSON imported at boot when the pool is empty |
| `PROXY_ISOLATION` | unset | Identity↔exit binding (see [Proxy isolation](#proxy-isolation-identity--exit-binding)). Only `true`/`false` are accepted: `true` forces it on (panel toggle read-only); `false` defaults it off (panel can still change it); unset defaults to on. Any other value warns and is treated as unset |
| `PUID` / `PGID` | `100` / `101` | UID/GID the gateway runs as. The entrypoint runs as root, re-owns the data directories to `PUID:PGID`, then drops privileges — set these to your host user for bind mounts |
| `LOG_REQUESTS` | `true` | Request logging (metadata only: IP, path, model, status, duration — never conversation content) |
| `LOG_FILE_MAX_MB` | `10` | `requests.jsonl` size cap; wiped when exceeded |
| `MAX_BODY_MB` | `32` | Request body limit; larger bodies get `413` |
| `APPLY_SYSTEM_PROMPT_OVERRIDE` | `false` | `true` enables replacing client system prompts with `override.md` |
| `STREAM_LOG` | `false` | Dump raw Anthropic-path SSE to disk (full conversations — debugging only) |
| `CLIENT_IP_HEADER` | empty | Trust this header for client IP behind a reverse proxy (e.g. `X-Real-IP`); by default only `RemoteAddr` is used |

Fail-closed startup: binding a non-loopback address without `API_KEY` and `ADMIN_PASSWORD` refuses to start.

### opencode zen session IDs

The zen free tier's session gate is a **stateless format check**: any ID matching
`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$` is accepted — the upstream neither checks
"has this ID been seen" nor timestamp freshness. The gateway therefore mints a
compatible ID locally per zen key at first use (the same structure the official
CLI produces, descending timestamp bits + random tail), keeps it sticky, and
persists it in the data volume (`.zen-sessions.json`). There is no embedded CLI,
no harvester, and no related environment variables. Legacy placeholder IDs
(`sess_*`) in an existing session file are replaced with minted ones on first
load.

If a session gets rejected (FreeTier `403`), the gateway mints a fresh ID
locally after two consecutive `403`s (one-minute per-key backoff) — no
subprocess, no quota cost. Should zen ever tighten the gate (valid-format IDs
also rejected), the rising refresh frequency in the logs is the early signal.

The admin panel's opencode tab lists every key's session ID, state, and mint
time, plus a per-key **Test** button with a probe-model picker (default
**auto**: big-pickle first, then live-synced models; any free zen model can be
chosen): it sends one real probe request pinned to that key, and on success
clears the key's cooldown immediately (a rate-limited answer — 429, or a
403/503 whose body zen words as a limit — reports the upstream's expected
recovery time instead). Note a successful probe consumes one request of that
key's quota — the same trade-off as the cline Test button.

## Connecting your IDE

**Cursor / ZCode / anything OpenAI-flavored** — `POST /v1/chat/completions`:

```
Base URL:  http://<host>:3457/v1
API Key:   <API_KEY>
Model:     cline-free/deepseek-v4.1-flash   (cline pool)
           mimo-v2.5-free                    (opencode zen)
```

**Claude Code / Cline** — Anthropic dialect `POST /v1/messages`, same base URL and key.

**OpenAI Responses dialect** — `POST /v1/responses` (tool calls, streaming events, and usage are fully translated).

`GET /v1/models` lists every model the gateway can serve, including your Combos. Free-model feeds sync automatically (cline official feed every 60s, zen catalog every 10min): the live list is authoritative and the built-in seeds only cover a cold start / offline boot, so delisted or newly-freed models appear or disappear on their own.

### Seeding accounts

Two account kinds are supported and can be mixed:

```json
[
  {"refreshToken": "workos-oauth-refresh-token", "email": "acc1@example.com"},
  {"apiToken": "sk_...", "email": "acc2@example.com"}
]
```

Mount the file anywhere in the container and point `CLINE_ACCOUNTS_SEED_FILE` at it; it's imported when the pool is empty. Static `sk_` API keys are used directly as Bearer tokens (no refresh; a 401 marks them expired). OAuth accounts can also be added via the panel's browser login flow, manual token paste, or batch import.

**Newly added identities start disabled on all three platforms.** An account, zen key, or WorkBuddy account added after this version lands in the pool with routing participation off (Cline: `pool_enabled` false; zen: no routing entry; WorkBuddy: `pool_enabled` false) and is skipped by rotation until you tick its **Pool** checkbox in the panel. Identities already in the pool keep their current state — a legacy account without the field stays enabled, so upgrading never stops serving. Zen keys coming from `ZEN_KEYS` follow the same rule.

### Combos (alias models)

Create user-defined alias IDs in the panel (e.g. `my-cline-flash` → `cline-free/deepseek-v4.1-flash` on the cline platform, or any zen free model). Strictly same-platform targets; aliases show up in `/v1/models` so IDEs can pick them directly.

### WorkBuddy

Open `/admin/`, select **OpenCode → WorkBuddy**. The page embeds the WorkBuddy2API panel behind the existing admin session; there is no second panel password to manage. The subsystem is independent from the Cline and zen pools and can fail to initialize without preventing the rest of the gateway from starting.

- **OAuth and account pool.** Add CN or global accounts through the browser login flow. Credentials are stored under `data/workbuddy/auths/`; pool state and configuration live under `data/workbuddy/`. Accounts are hot-loaded after login and retain the upstream pool's breaker, cooldown, sticky-session, retry, and weighted-selection behavior.
- **Scheduled jobs.** The upstream scheduler is ported as-is, including configurable check-in, activity, travel, keepalive, growth-task, and balance-refresh schedules. Each job has its own enable flag in the WorkBuddy configuration page.
- **API compatibility.** `POST /v1/chat/completions`, `/v1/messages`, and `/v1/responses` accept WorkBuddy models with a realm prefix: `cn:<model>` or `global:<model>`. The WorkBuddy handler supports streaming and non-streaming requests, reasoning fields/effort downgrade, tool-compatible payload handling, and the upstream fingerprint-sanitization switch. `GET /v1/models` merges the prefixed WorkBuddy IDs into the normal model list.
- **Routing boundary.** Bare model names are never routed to WorkBuddy, even if the same model ID exists upstream. This preserves the existing Cline/OpenCode routing and strict-model behavior. Remove the `cn:` / `global:` prefix only when calling the standalone WorkBuddy implementation, not this gateway.
- **Proxy binding.** With proxy isolation enabled, each WorkBuddy UID is assigned a stable main/backup pair from the existing proxy pool on first use. Bindings are persisted in `data/.workbuddy-proxies.json` and can be inspected or changed through `GET /admin/api/workbuddy/proxy`, `POST /admin/api/workbuddy/proxy/set`, and `POST /admin/api/workbuddy/proxy/clear`. Authenticated account actions, scheduled jobs, task automation, and chat requests use the bound exit; if both bound exits are cooling or removed, the request fails rather than falling back to direct egress.
- **OAuth transport boundary.** Device authorization, login polling, and the model-catalog refresh run before an account UID is known and therefore do not use the account binding. They carry no existing account credential. Once an account is created, all account-authenticated WorkBuddy traffic follows the UID binding.

### Proxy isolation (identity ↔ exit binding)

Without isolation, every upstream attempt rotates across the whole proxy pool: each account's egress IP drifts between exits *and* each exit serves multiple accounts in turn — both are classic risk-control red flags ("same IP, many accounts", "one account, many IPs"). Isolation mode (default on) replaces the rotation with a fixed binding:

- Each Cline account, zen key, and WorkBuddy account can bind one **main** and one **backup** proxy. Requests always egress via the main, fall back to the backup, and are **skipped entirely** while both are cooling or removed from the pool — never routed through another exit or a direct connection. Isolation outranks availability. WorkBuddy bindings are allocated round-robin on first use and persisted automatically.
- Unbound identities follow the panel's per-platform global policy toggles (Cline / OpenCode / WorkBuddy, default: use the proxy pool), so an empty binding config behaves exactly like before.
- - Assign bindings per account (Accounts page) / per key (opencode page), or use **Assign proxies evenly** (`main = pool[i%N]`, `backup = pool[(i+1)%N]`). A binding whose proxy was removed from the pool is flagged ⚠ and its identity stays skipped until fixed.
- Toggle it off on the Proxy pool page to restore legacy per-request rotation (bindings are then ignored). `PROXY_ISOLATION=true` forces it on and makes the panel toggle read-only; `false` only changes the default to off (the panel can still change it). Only `true`/`false` are accepted — any other value warns and is treated as unset.
- **Per-proxy online rate.** The Proxy pool page lists every proxy with its real-traffic transport-layer success rate — cumulative since the pool was last edited, plus a rolling window over the last 50 attempts. Upstream 4xx/5xx never counts against the proxy (the tunnel worked); client aborts count for neither side. Sampling is fully passive — no probing traffic is ever generated — and stats never influence routing: a proxy below 80% recent success (≥10 samples) is highlighted red so you can swap it out yourself. Editing the proxy list resets all counters; stats survive restarts (`.proxy-health.json`). Cline, OpenCode, and WorkBuddy traffic all count; direct exits don't.

socks5/socks5h proxies are fully supported on both paths: the gateway dials them natively for regular upstream traffic, and for CLI minting it fronts a one-shot local HTTP-CONNECT→SOCKS5 bridge (the CLI only ever sees an http proxy, which its runtime is documented to honor — socks5 semantics are fulfilled by the gateway itself, so a mint can never silently fall back to a direct connection).

One side benefit on the zen side: the free quota (~200 requests / 5h) is accounted **per egress IP**, so binding keys to distinct exits is also what actually unlocks multi-key capacity, not just ban-avoidance.

## Battle-tested

Verified against live upstreams with real free-tier credentials (16-probe chat matrix + 12-probe function-calling matrix, all passing):

- Streaming shape (role chunk, `[DONE]`, `finish_reason` `tool_calls` vs `stop`), full tool round-trips, parallel tool calls with distinct indices/ids
- `stream_options.include_usage`, array content parts, long multi-turn histories, parameter passthrough, clean 4xx errors
- Client aborts propagate (no account/key cooldown pollution), 8-way parallel load, container healthcheck, seed import, key rotation

Known upstream quirks (not gateway bugs): zen's `muse-spark-*-free` models only work on the native `/v1/responses` endpoint (the gateway routes them there automatically and re-emits chat/Anthropic shapes); `deepseek-v4-flash-free` was removed from the catalog as deprecated; cline's `stop` handling wipes content when the model's reasoning echoes the stop word (gateway truncates non-stream output as compensation); some reasoning-heavy models eat small `max_tokens` budgets before producing visible text. 

## Development

```bash
go build ./... && go vet ./...
./afree-proxy -host 127.0.0.1 -port 3457   # or: go run . 
./start.sh                                  # build-or-docker wrapper
docker compose up -d --build                # build from source (PROXY_PORT to change the port)
```

One more env var is dev-only and deliberately off by default: `KILL_PORT_ON_START=true` makes the gateway force-kill whatever holds the listen port before binding (a Windows convenience, implemented with `Stop-Process`). Leave it unset in production — it kills a process it did not start, and that may be a legitimate service.

CI: every push to `main` runs `go build` + `go vet`, then publishes the multi-arch image to GHCR via Buildx (amd64 compiled natively, arm64 cross-compiled via `TARGETARCH`; only the embedded opencode CLI stage runs under QEMU for arm64, at build time — the images themselves are native on both arches). Tag a release with `v*` to publish `:vX.Y.Z` alongside `:latest`.

Project layout:

```
├── main.go                  entry point, CLI flags, running detection
├── internal/app/
│   ├── proxy.go             /v1 routing, chat handler, upstream calls, aggregation
│   ├── responses.go         /v1/responses dialect translation
│   ├── zen.go               opencode zen upstream, routing, rate-limit defense
│   ├── zen_session.go       sticky zen sessions (CLI-minted sess_, per-key identity)
│   ├── zen_session.go        zen sticky session IDs (locally minted)
│   ├── zen_endpoint.go      endpoint auto-learn (chat vs /v1/responses per model)
│   ├── tls_bun.go           uTLS ClientHello mimicry for the zen upstream
│   ├── compact.go           opencode-style context compaction for zen free models
│   ├── proxy_pool.go        egress proxy pool (uTLS/h1 client cache, per-request rotation)
│   ├── models.go            cline free-model feed sync + default model
│   ├── pool.go              cline account pool (OAuth refresh, static API keys, cooldowns)
│   ├── seed.go              boot-time account seeding
│   ├── combos.go            alias model IDs
│   ├── admin.go/_auth/_html/_zen  admin panel: API, session auth, UI, zen page
│   ├── workbuddy.go/_config/_proxy/_dialects  WorkBuddy subsystem, exit binding, client dialects
│   ├── config.go            env-driven configuration (fail-closed checks)
│   ├── logs.go / stats.go   request logging + token statistics
│   └── types.go             shared data structures
├── internal/cline/          cline upstream auth (WorkOS OAuth refresh)
├── internal/workbuddy/      vendored WorkBuddy2API pool, panel, scheduler, upstream
├── internal/kit/            HTTP client, random IDs, data paths
├── Dockerfile               multi-arch (amd64 native + arm64 cross-compile)
└── docker-compose.yml       source build, PROXY_PORT-parameterized
```

## Credits

Built on [YuJunZhiXue/Cline-proxy](https://github.com/YuJunZhiXue/Cline-proxy). Thanks to the [LINUX DO](https://linux.do) community.
