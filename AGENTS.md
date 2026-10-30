# AGENTS.md

## Scope

- The application lives in `avatar-harness/`; commands and paths below are relative to that directory unless stated otherwise.
- The maintained core is Go + Vue/Three.js. Use active code and `README.md` for current behavior; exclude `.cache/` and `web/node_modules/` from routine searches. Reference repositories and licences are recorded in `sources.lock.json` and `integrations/licenses/`.
- Entry: `cmd/avatar-harness/main.go` → `internal/cli`. Session/tool orchestration lives in `internal/runtime`; browser event and media coordination lives in `web/src/useHarness.ts`, not the Vue components.

## Build and verify

- Source builds need Go 1.24+ and Node.js 22.12+. Use `./scripts/go` rather than system `go`: it selects the cached SDK when available and keeps Go caches project-local.
- `web/assets.go` embeds `web/dist`: generate it before Go builds/tests on a fresh checkout and regenerate it after frontend edits. Neither `./scripts/build` nor `./start` builds frontend sources.

```sh
cd web
npm ci
npm test
npm run build
cd ..
./scripts/go test -race ./...
./scripts/build
```

- Focused Go test: `./scripts/go test -race ./internal/runtime -run '^TestOrderedTTSPrefetch$'`.
- Focused frontend test, from `web/`: `node --experimental-strip-types --test tests/realtime.test.mjs`. These use Node's built-in test runner, not Vitest. `npm run build` includes Vue typechecking.
- Unit suites use local fixtures, without cloud keys or GPUs. The legacy import test skips if the optional `sqlite3` CLI is unavailable.

## Runtime and integration gotchas

- Noninteractive startup: `./start serve --no-open`, normally at `http://127.0.0.1:8765`. Bare `./start` can enter terminal menus.
- Config defaults to `.avatar-harness/config.json`; `AVATAR_HARNESS_CONFIG` or `--config` overrides it. Its parent directory also holds `secrets.env`, session history, and server state: use a separate directory for smoke-test configs, preserving existing `.avatar-harness/` data. Existing environment values take precedence over `secrets.env`.
- For provider/tool/media changes, read `docs/provider-contract.md`; Python/GPU models run as external services, while internal factories are compiled Go registrations, not Python `module:function` imports. Bailian speech setup and live probes are documented in `docs/bailian.md`.
- Preserve the `<speak/>` answer-commit gate: tool-decision preludes must not reach TTS. Cancellation and stale-result filtering span both Go and browser input/turn IDs.
- `scripts/voice-probe.mjs` and the browser voice probe exercise real cloud providers through a configured running server; they are not offline tests. `cmd/harness-bench` measures scripted control-plane overhead, not model latency or three-second voice acceptance.
- Voice latency starts at the last voiced input frame and requires observed AudioContext playback (`audio_within_budget`); packet arrival, text output, and audio/video synchronization are separate measurements.
- Before character-asset changes, consult `../assets/characters/mpfb/README.md` and `web/public/models/NOTICE.md` for the relevant asset's provenance and limits. LAM ZIPs require a dedicated renderer, not GLTFLoader.
