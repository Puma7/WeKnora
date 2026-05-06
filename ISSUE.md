# WeKnora — Health Check / Bit-Rot Audit

> Periodic health check from **2026-05-06**
> Branch: `claude/legacy-health-check-w6Tp9`
> Method: Static analysis with five parallel sub-agents (Go backend, Go infrastructure/IM, Python DocReader, Vue/TS frontend, deployment/DevOps).
> Status: **Documentation only** — no code changes made.

---

## 0. Project Profile

| Area | Language / Framework | Version | Files (approx.) |
|---|---|---|---|
| Backend | Go | 1.24.11 (per `go.mod`) | 671 `.go` |
| DocReader microservice | Python (gRPC) + FastAPI | Python 3.10.18 | 51 `.py` |
| Frontend | Vue 3 + TypeScript + Vite + Pinia + tdesign-vue-next | TS ~5.8, Vue 3.5 | 101 `.vue` / 64 `.ts` |
| Desktop | Wails | — | `cmd/desktop` |
| Mini app | WeChat Miniprogram | — | `miniprogram/` |
| Database | PostgreSQL + pgvector / SQLite + sqlite-vec | — | `migrations/` |
| Vector / Graph | Milvus, Qdrant, Weaviate, Neo4j | — | docker-compose |
| Observability | Langfuse, Jaeger | — | docker-compose |
| Deployment | Docker Compose, Helm, GitHub Actions | — | — |

---

## 1. TOP-20 — What hurts immediately

> Sorted by realistic blast radius. Severity: **C**ritical / **H**igh / **M**edium / **L**ow.

| # | Severity | Finding | Location |
|---|---|---|---|
| 1 | **C** | CORS: `AllowOrigins: ["*"]` **with** `AllowCredentials: true` (CORS-spec violation; browsers drop tokens, but the pattern signals a fundamental misunderstanding) | `internal/router/router.go:80-87` |
| 2 | **C** | Slack webhook signature verification skipped when `signingSecret == ""` → unsigned requests are accepted | `internal/im/slack/adapter.go:100-103` |
| 3 | **C** | Hardcoded default passwords in compose: Neo4j `password`, MinIO `minioadmin`, ClickHouse `clickhouse` | `docker-compose.yml:128, 237-238, 437-438` |
| 4 | **C** | Langfuse `ENCRYPTION_KEY` defaults to 64 × `0` (zero entropy) | `docker-compose.yml:523` |
| 5 | **C** | `curl -LsSf https://astral.sh/uv/install.sh \| sh` unverified during build (supply-chain RCE) | `docker/Dockerfile.app:79`, `scripts/start_all.sh:142` |
| 6 | **C** | JWT and refresh tokens stored in `localStorage` (XSS-exposed) | `frontend/src/stores/auth.ts:63,77,82,144-152`, `frontend/src/utils/request.ts:32,41,134,146-147` |
| 7 | **C** | `asyncio.run()` called from synchronous library code → `RuntimeError`, blocks gRPC workers | `docreader/parser/web_parser.py:100` |
| 8 | **C** | Mutable default arguments (list) in method → shared state across calls | `docreader/parser/doc_parser.py:234-235` |
| 9 | **C** | No `resources.limits` on **any** compose service → one OOM takes down the host | `docker-compose.yml`, `docker-compose.dev.yml` |
| 10 | **H** | `c.ShouldBindJSON(&req)` discarded with `_ =` → invalid request bodies pass silently | `internal/handler/tag.go:305` |
| 11 | **H** | `math/rand` (globally seeded) used by `RandomSelect` in HTTP handler → weak/predictable | `internal/handler/initialization.go:8, 2372` |
| 12 | **H** | `http.Client{}` without timeout in four reranker implementations → can hang forever | `internal/models/rerank/{zhipu,aliyun,jina,remote_api}*.go:63-90` |
| 13 | **H** | WeChat file download performs no SSRF validation on the server-supplied URL | `internal/im/wechat/adapter.go:143-200` |
| 14 | **H** | 13× `_ = s.saveKBCloneProgress(...)` → clone/move progress silently lost | `internal/application/service/knowledge_clone_move.go:260,310,342,368,413,430,440,484,579,743,758,797,810` |
| 15 | **H** | `actions/checkout@v3` (deprecated, EOL 2025) | `.github/workflows/docker-image.yml:18,67,100,143` |
| 16 | **H** | `image: …:latest` for Neo4j, Jaeger, Dex, MinIO (dev), `weknora-ui`, `weknora-app`, `docreader` | `docker-compose.yml:3,29,144,174,290,366`, `docker-compose.dev.yml:39,106,165,190` |
| 17 | **H** | `ENCODED_PASSWORD=$(python3 -c "…quote('$DB_PASSWORD'…")` — DB password expanded via shell (quote-escape bug; logging leaks plaintext) | `scripts/migrate.sh:51-69` |
| 18 | **H** | Deep watchers (`{ deep: true }`) on 13 Vue components watching large lists/stores | `frontend/src/views/chat/index.vue:213,218`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:826,851`, `frontend/src/views/knowledge/KnowledgeBase.vue:887`, others |
| 19 | **H** | `context.Background()` in many goroutines / request paths → shutdown/cancel ignored (12-h timeout keeps running) | `internal/handler/initialization.go:1069-1073`, `internal/application/service/session.go:507-512`, `internal/handler/session/agent_stream_handler.go:392-405`, `internal/im/qaqueue.go:142,152,157,198,231,238,255,266`, `internal/im/service.go:511,536,562,579,589` |
| 20 | **H** | `subprocess.run(["which", …])` without `timeout=` (can hang, blocks workers) | `docreader/parser/doc_parser.py:258-259` |

---

## 2. Language-Specific Modernization (Bit Rot)

### 2.1 Go (target: 1.24.11)

| File:Line | Severity | Finding |
|---|---|---|
| `internal/handler/initialization.go:8, 2372` | H | Uses `math/rand` with global seed instead of `math/rand/v2`. In concurrent HTTP handlers this is non-deterministic; in `RandomSelect(...)` it is weak. |
| `internal/infrastructure/web_search/{google,bing,duckduckgo,baidu,ollama,tavily}.go` (multiple lines, see comments in code) | M | `fmt.Errorf` without `%w` → `errors.Is/As` won't match; inconsistent error chains. |
| various packages | L | No relevant `io/ioutil` use, no `golang.org/x/net/context`, no `strings.Title` — very clean. Mandatory `go vet` and `golangci-lint` still recommended. |

### 2.2 Python (target: 3.10+)

| File:Line | Severity | Finding |
|---|---|---|
| `docreader/parser/web_parser.py:100` | C | `chtml = asyncio.run(self.scrape(url))` from a sync method → event-loop conflict in async caller context. |
| 18+ files (`docx_parser.py:10`, `chain_parser.py:43`, `excel_parser.py:59`, `doc_parser.py:234`, …) | H | Outdated typings: `from typing import List, Dict, Optional` instead of PEP 585/604 (`list[...]`, `X \| None`). |
| Several parsers | M | Frequent `os.path.*` instead of `pathlib.Path` (~30 occurrences). Pure modernization debt. |
| `docreader/parser/doc_parser.py:223`, `docreader/utils/endecode.py:53` | M | `open(...)` without explicit `encoding=` (binary mode is fine, string mode is a latent trap). |

### 2.3 Frontend (Vue 3 / TypeScript)

| File:Line | Severity | Finding |
|---|---|---|
| `frontend/src/App.vue:147,149,158,160`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:642,646,1184,1186,1476,1480`, `frontend/src/views/settings/GeneralSettings.vue:243,245`, `frontend/src/components/KnowledgeBaseSelector.vue:128`, `frontend/src/utils/caret.ts:22` | H | 12× `@ts-ignore` without comment/justification. |
| `frontend/src/stores/organization.ts:78-380`, `frontend/src/stores/menu.ts:80,113`, `frontend/src/components/MentionSelector.vue:164,168,206,208`, `frontend/src/types/tool-results.ts:288` | H | 46+ `any`, almost every `catch` block typed `catch (e: any)`. |
| `frontend/src/components/UserMenu.vue:379`, `frontend/src/components/doc-content.vue:150`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:690-691,1283`, `frontend/src/views/settings/AgentSettings.vue:847,981,1708`, `frontend/src/utils/tdesign-icon-offline.ts:55,69`, `frontend/src/composables/useTheme.ts:37` | M | Direct `document.querySelector/getElementById` without null-guards instead of Vue `ref`s. |
| `frontend/src/components/IMChannelsOverviewPanel.vue:59`, `frontend/src/stores/organization.ts:201`, `frontend/src/views/settings/WebSearchSettings.vue:273-274`, `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1121-1122`, `frontend/src/views/knowledge/components/DocumentListView.vue:241,243,247,249-250` | M | 15+ non-null assertions (`!.`) on map lookups / server data, no runtime validation. |
| `frontend/src/components/menu.vue:535` | L | Promise chain (`.then`) inside an otherwise `async/await`-driven codebase. |

### 2.4 Deployment / DevOps

| File:Line | Severity | Finding |
|---|---|---|
| `.github/workflows/docker-image.yml:18,67,100,143` | H | `actions/checkout@v3` (deprecated). |
| `docker-compose.yml:3,29,144,174,290,366`, `docker-compose.dev.yml:39,106,165,190` | H | `:latest` tags for Neo4j, Jaeger, Dex, MinIO (dev), `weknora-ui/app/docreader`. |
| `docker/Dockerfile.docreader:4` | M | `python:3.10.18-bookworm` — 3.10 EOL October 2026. |
| `docker/Dockerfile.app:51` | M | `debian:12.12-slim` — Distroless or Alpine would be smaller and more secure. |

---

## 3. Bugs & Unhandled Edge Cases

### 3.1 Go

| File:Line | Severity | Finding |
|---|---|---|
| `internal/handler/tag.go:305` | C | `_ = c.ShouldBindJSON(&req)` — bind error discarded; an invalid JSON body yields zero values and processing continues. |
| `internal/handler/knowledgebase.go:285` | H | `_ = json.Unmarshal(b, &dataMap)` — error discarded; the subsequent `if dataMap != nil` check does not reliably catch this. |
| `internal/im/feishu/adapter.go:735,791,796,837,873,876,918` | H | 9× `payload, _ := json.Marshal(...)` → on marshal error invalid payloads are sent. |
| `internal/im/slack/adapter.go:143` | H | `json.Unmarshal(bodyBytes, &rawEvent)` error ignored → `files` stays nil. |
| `internal/models/chat/ollama.go:193,200` | H | Tool-parameter unmarshal errors ignored → `function.Parameters` empty. |
| `internal/im/slack/adapter.go:153` | H | Type assertion without `,ok` on `rawEvent.Event.Files` → potential panic. |
| `internal/handler/initialization.go:1069-1073` | H | Goroutine with `context.WithTimeout(context.Background(), 12*time.Hour)` — survives server shutdown. |
| `internal/handler/session/agent_stream_handler.go:392-405` | H | `bgCtx := context.Background()` for `AppendEvent` mid-request → ignores client cancel. |
| `internal/application/service/session.go:507-512` | H | Detached `context.Background()` in async goroutine. |
| `internal/im/qaqueue.go:142,152,157,198,231,238,255,266`; `internal/im/service.go:511,536,562,579,589` | M | 13 `context.Background()` calls in queue/service paths. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:65,162,182,222,229,239,294,296,327`; `:177` | M/H | 9× `context.Background()` plus `resp.Body.Close()` without `defer` (leak risk on errors between read and close). |
| `internal/infrastructure/docparser/mineru_converter.go:263` | H | Same pattern: `resp.Body.Close()` without `defer`. |
| `internal/handler/initialization.go:2038` | M | `defer file.Close()` on upload — errors are swallowed; on write-flush errors no signal. |
| `internal/agent/tools/web_fetch.go:286`; `internal/infrastructure/web_fetch/fetcher.go:53` | M | DNS lookup uses `context.Background()` — can hang without timeout. |
| `internal/application/service/knowledge_clone_move.go:260…810` (13 locations) | H | `_ = s.saveKBCloneProgress(...)` — progress persistence silently lost. |

### 3.2 Python

| File:Line | Severity | Finding |
|---|---|---|
| `docreader/parser/doc_parser.py:234-235` | C | Mutable defaults: `possible_path: List[str] = []`, `environment_variable: List[str] = []` are mutated (`.extend(...)` line 247) → cross-call state. |
| `docreader/parser/chain_parser.py:62-66` | H | `except Exception: logger.exception(...); continue` — too broad, swallows `KeyboardInterrupt`/`SystemExit`. |
| `docreader/parser/docx_parser.py:337-342` | M | Wide `except Exception as e: logger.error(...) continue` without fallback document or re-raise. |
| `docreader/parser/docx_parser.py:310` | M | `img = img[0]` without preceding `if not img:` check. |
| `docreader/parser/docx_parser.py:178` | M | `image_data.object.close()` only called in success path → handle leak on error in `.decode_image()`. |

### 3.3 Frontend

| File:Line | Severity | Finding |
|---|---|---|
| `frontend/src/components/Input-field.vue:549`, `frontend/src/components/menu.vue:680`, `frontend/src/hooks/useKnowledgeBase.ts:71,180`, `frontend/src/views/settings/ChatHistorySettings.vue:184`, `frontend/src/views/settings/RetrievalSettings.vue:179` | H | 6× `.catch(() => {})` — errors swallowed without log or UI feedback. |
| `frontend/src/views/chat/index.vue:162-202` | M | `suggestedQuestionsFetchId` pattern incomplete; `setTimeout`-debounce without `AbortController` → stale responses possible on session switch. |
| `frontend/src/views/chat/components/botmsg.vue:260` | M | `parentMd.value.addEventListener(...)` without null check. |
| `frontend/src/utils/mermaidViewer.ts:152-154,212-213,233` | M | `divEl` removed in cleanup without existence check. |
| `frontend/src/components/doc-content.vue:905-906` | M | `v-for` with `:key="index"` over `processedChunks` — breaks on reorder/filter. |
| `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1286,1301,1316,1519,1549,1616,1632,1645,1652`; `frontend/src/views/agent/AgentEditorModal.vue:3256,3298,3344` | M | `addEventListener` without matching `removeEventListener` in `onUnmounted` — memory leak on frequent mount/unmount. |
| `frontend/src/components/IMChannelPanel.vue:575,724` | M | `wechatPollTimer = setTimeout(pollOnce, 500)` recursively re-armed; `stopWeChatPolling()` may call `clearTimeout` too late → timer survives unmount. |
| `frontend/src/api/chat/streame.ts:149-159` | M | `chunkHandler` not wrapped in `try/catch` → stream silently stops on handler error. |

### 3.4 Deployment

| File:Line | Severity | Finding |
|---|---|---|
| `docker-compose.yml` (Redis 222-228, Qdrant 299-312, Weaviate 342-363, Milvus gRPC :19530) | H | Healthchecks missing or only cover web port (9091), not the gRPC endpoint. `depends_on` with `service_started` triggers startup race. |
| `docker-compose.yml:212-217` | M | Postgres healthcheck: `interval=10s`, `retries=3`, `start_period=30s` — if init takes >30s, fails before `start_period` elapses. |
| `docker-compose.yml:149-154` | M | `app` depends on Redis with `service_started` instead of `service_healthy`. |
| `migrations/` | M | Multiple migrations without `IF NOT EXISTS` / down counterpart — not idempotent. |
| `scripts/dev.sh`, `scripts/build_images.sh` | H | No `set -euo pipefail`. Unset vars and broken pipes fail silently. |

---

## 4. Inconsistencies & Architectural Drift

### 4.1 Go

- **Webhook verification inconsistent across IM platforms** — Slack lets unsigned through, Telegram uses `subtle.ConstantTimeCompare`, WeCom/DingTalk `hmac.Equal`, Feishu token-based, WeChat refuses webhooks. No common interface, no central policy. (`internal/im/{slack,telegram,wecom,dingtalk,feishu,wechat}/*adapter*.go`)
- **HTTP clients inconsistent** — Rerankers: `&http.Client{}` (no timeout); WeChat: 30s; Feishu: 10s; `web_fetch`: SSRF wrapper. There is no central factory. (`internal/models/rerank/*`, `internal/im/wechat/adapter.go:40`, `internal/im/feishu/adapter.go:36`)
- **Magic numbers** — Queue limits defined in `internal/im/qaqueue.go:17-33`, but `streamReaperInterval`, `streamOrphanTTL`, `defaultCloudTimeout` (`internal/infrastructure/docparser/mineru_cloud_converter.go:120`) are scattered.
- **Duplicate code** — `cardkitCreate` and `sendCardByCardID` in `internal/im/feishu/adapter.go:735-829` build their HTTP requests almost identically.
- **`internal/handler/knowledgebase.go:284-290`** — Struct → JSON → Map → JSON just to set a single field. A dedicated DTO or `MarshalJSON` would be cleaner.
- **SQLite repository: `fmt.Sprintf` for table names** — `internal/application/repository/retriever/sqlite/repository.go:503,520,533`. Table names are dimension-derived (`vecTableName(dim)`), not user input — but the pattern sets a bad precedent.

### 4.2 Python

- **`print()` in library code instead of logger** — `docreader/parser/chain_parser.py:179`, `markdown_parser.py:124`, `web_parser.py:149-162`, `excel_parser.py:114-118`.
- **Inconsistent error patterns** — some parsers return `Document(content="Error...")`, others `Document()`, others raise. (`docreader/parser/pdf_parser.py:54`, `web_parser.py:112-113`)
- **Per-module logger with own `setLevel(...)`** — `docreader/parser/base_parser.py:10`, `config.py:7` — should be configured centrally.
- **`enableMultimodal` vs. `enable_multimodal`, `maxPages` vs. `max_pages`** in `docreader/parser/docx_parser.py:294+`.
- **High duplication** in parsers (`Docx`, `DocxParser`, `Docx2Parser`, `MarkitdownParser` + fallback chains). A common base is missing.

### 4.3 Frontend

- **Two API clients** — `frontend/src/utils/request.ts` (axios) vs. `frontend/src/api/chat/streame.ts` (fetchEventSource). Token refresh and error handling implemented separately in both.
- **Inconsistent error shapes** — `request.ts:199-215` returns a mixed object; stores extract sometimes `.message`, sometimes `.error.message`. No shared `ErrorResponse` interface.
- **Composition vs. Options API** — 99% Composition API, but composables in `src/composables/` mix `ref`/`reactive` without a clear convention.

### 4.4 Deployment

- **`.env.example` ↔ `.env.lite.example`** — key schema drifts (Lite uses `DB_PATH` instead of `DB_HOST/DB_PORT`; ~12 Langfuse / DB keys missing). No documented mapping.
- **`docker-compose.yml` ↔ `docker-compose.dev.yml`** — Dev uses `:latest` for MinIO/Jaeger/Neo4j; prod has version pins. Parity broken.
- **`helm/values.yaml:60-66`** — `image.tag: ""` falls through to `Chart.appVersion=v0.5.1`, no digest pin.
- **`docker-compose.yml:127`** — Username `neo4j` hardcoded, only the password is variabilized.

---

## 5. Performance & Resource Leaks

### 5.1 Go

| File:Line | Severity | Finding |
|---|---|---|
| `internal/models/rerank/{zhipu,aliyun,jina,remote_api}*.go:63-90` | H | `&http.Client{}` without timeout in four implementations. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:177`, `mineru_converter.go:263` | H | `resp.Body.Close()` without `defer`. |
| `internal/im/feishu/adapter.go:581` (`feishuStreams map[...]`) and counterparts in Slack/Telegram | M | Stream maps without hard upper bound; cleanup depends on reaper goroutine. |
| `internal/sandbox/validator.go:466` | M | `compilePatterns()` recompiles regex per call — if in hot path, allocations per request. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:294-296` | L | Full `extract_result` JSON logged (only truncated to 4000 chars). |
| `internal/im/service.go:589-591` | M | WS-leader-renewal goroutine via `WithCancel(context.Background())`. Cancel-fn stored, but not guaranteed to be called → leak until Redis TTL (15s). |
| `internal/im/feishu/adapter.go:67-85` | M | `startStreamReaper()` started via `sync.Once`, but `StopStreamReaper` not guaranteed to be called on shutdown. |
| `internal/models/rerank/*.go` (default `&http.Client{}`) | M | Inherits system `HTTP_PROXY`/`HTTPS_PROXY` automatically — API calls may unintentionally route through a proxy. |

### 5.2 Python

| File:Line | Severity | Finding |
|---|---|---|
| `docreader/parser/web_parser.py:100` | C | `asyncio.run()` blocks the gRPC worker thread completely (loop creation + teardown per call). |
| `docreader/parser/doc_parser.py:258-259` | H | `subprocess.run(["which", ...])` without `timeout` — `which` can hang. |
| `docreader/parser/docx_parser.py:112-113` | M | `ProcessPoolExecutor(max_workers=min(4, os.cpu_count() or 2))` — hardwired, no config hook, memory spikes for large documents. |
| `docreader/parser/doc_parser.py:118` (`TempFileContext(content, ".doc")`) | M | Fully in-memory; no streaming fallback (gRPC limit 50 MB per `config.py:71`). |

### 5.3 Frontend

| File:Line | Severity | Finding |
|---|---|---|
| 13 sites incl. `frontend/src/components/doc-content.vue:355`, `frontend/src/components/Input-field.vue:1495`, `frontend/src/views/chat/index.vue:213,218`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:826,851`, `frontend/src/views/creatChat/creatChat.vue:168-169`, `frontend/src/views/knowledge/KnowledgeBase.vue:887`, `frontend/src/views/knowledge/components/FAQEntryManager.vue:2826,3270` | H | `watch(..., { deep: true })` over large lists/stores → O(n) per change. |
| `frontend/src/components/document-preview.vue:413`, `frontend/src/views/chat/components/botmsg.vue:30` | M | Images without `loading="lazy"` and without `width`/`height` (CLS). |
| `frontend/src/api/chat/streame.ts:26-187` | M | No stream abort on route change / session switch — earlier SSE keeps running. |
| `frontend/package.json` (`tdesign-vue-next`) + `frontend/vite.config.ts` | L | No explicit tree-shaking hint; monitor bundle size. |

### 5.4 Deployment

| File:Line | Severity | Finding |
|---|---|---|
| `docker-compose.yml`, `docker-compose.dev.yml` (all services) | C | No `deploy.resources.limits` / `requests`. |
| `docker-compose.yml:209,601-612` | H | `postgres-data` as anonymous named volume — gone on `docker volume rm`, no backup hint. |
| `docker-compose.yml:270,440-441` | M | `jaeger_data`, `langfuse_clickhouse_logs` without rotation/TTL → disk fills up over time. |

---

## 6. Security & Technical Debt

### 6.1 Critical (act now)

| File:Line | Finding |
|---|---|
| `internal/router/router.go:80-87` | `AllowOrigins: ["*"]` + `AllowCredentials: true`. The CORS spec forbids this combination; browsers either drop cookies/headers, or open cross-origin paths emerge. → Set explicit allowlist or `AllowOriginFunc`. |
| `internal/im/slack/adapter.go:100-103` | `if a.signingSecret == "" { return nil }` — missing config ⇒ signature check bypass. |
| `docker-compose.yml:128, 237-238, 437-438, 522-525`, `.env.example:211` | Hardcoded defaults: `NEO4J_PASSWORD=password`, `MINIO_ROOT_PASSWORD=minioadmin`, ClickHouse `clickhouse/clickhouse`, `LANGFUSE_SALT=…change-me`, `JWT_SECRET=weknora-jwt-secret`. |
| `docker-compose.yml:523`, `docker-compose.dev.yml:333` | `LANGFUSE_ENCRYPTION_KEY=00…00` (64 zeros). |
| `docker/Dockerfile.app:79`, `scripts/start_all.sh:142` | `curl … \| sh` without checksum/signature check — supply-chain. |
| `frontend/src/stores/auth.ts:63,77,82,144-152`, `frontend/src/utils/request.ts:32,41,134,146-147` | JWT + refresh token in `localStorage` — easily stolen via XSS. |

### 6.2 High

| File:Line | Finding |
|---|---|
| `internal/im/feishu/adapter.go:746` | `Authorization: Bearer …` on requests that may pass through logging middleware. |
| `internal/im/wechat/adapter.go:155` | `http.NewRequestWithContext(ctx, GET, msg.FileKey, nil)` — no SSRF validation on the server-supplied URL. (`internal/infrastructure/docparser/mineru_cloud_converter.go:349` does it correctly.) |
| `internal/im/wechat/adapter.go:31-32`, Feishu, Telegram | Hardcoded API base URLs without override capability for testing/self-hosting. |
| `internal/im/slack/adapter.go:100` | Verification delegated to `slack.NewSecretsVerifier()`; unclear whether constant-time. |
| `internal/handler/initialization.go:2372` | `math/rand` for selection logic in HTTP handler. |
| `docreader/parser/web_parser.py:39-84` | `scrape(url)` accepts arbitrary URLs — Playwright will load `file://` etc. SSRF/LFI risk. |
| `rerank_server_demo.py:46` | `model_path = '/data1/home/lwx/work/Download/rerank_model_weight'` — hardcoded path, unrelated host context. |
| `rerank_server_demo.py:66` | `/rerank` without auth/rate-limit/body-size check. |
| `frontend/src/components/GlobalCommandPalette.vue:127,154` | `v-html="highlight(...)"` with custom regex instead of DOMPurify. |
| `frontend/src/views/chat/components/AgentStreamDisplay.vue:329,365` | `v-html="floatPopup.content"`, `v-html="wikiDrawerContent"` — sanitization at setter site not guaranteed. |
| `frontend/src/components/document-preview.vue:121,331`, `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1083,1099,1203`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:1290` | Direct `innerHTML = '...'` (even if sources are currently internal, harder to audit than `v-html` + DOMPurify). |
| `frontend/src/views/auth/Login.vue:96,102,111`; `frontend/src/views/chat/components/docInfo.vue:17`; `frontend/src/views/settings/StorageEngineSettings.vue:253-362` | `target="_blank"` without `rel="noopener noreferrer"`. |
| `docker-compose.yml:114-115, 290-291` | MinIO console (9001) and Neo4j (7474/7687) exposed on host; with defaults trivially takeable. |
| `scripts/migrate.sh:67-69` | `echo "DB_PASSWORD: ${DB_PASSWORD}"` writes the password to logs. |

### 6.3 Medium

| File:Line | Finding |
|---|---|
| `docreader/parser/docx_parser.py:17` | `python-docx` uses `xml.etree` — no `defusedxml`. XXE risk on untrusted DOCX. |
| `docreader/main.py:115` | `logger.info("Read(URL): url=%s", request.url)` — URL incl. query params logged. |
| `internal/handler/user.go` (to verify) | Audit logging paths for token/PII leakage. |
| `helm/values.yaml:84` | Comment "Disabled - official images run as root" — unresolved security requirement. |
| `helm/`, GitHub workflows | `permissions:` not restrictively set; third-party actions without SHA pin (to verify). |
| `docker-compose.yml:127` | Neo4j username hardcoded. |
| `cmd/desktop/main.go:46-100` | `dragHandlerJS` injected via `DomReady`; CSP configuration not visible. |
| `miniprogram/utils/config.js:27` | `wx.setStorageSync('weknora_settings', next)` contains `apiKey` in plaintext. |
| `frontend/vite.config.ts:42-53` | No production-relevant security headers (`Content-Security-Policy`, `X-Frame-Options`). |

### 6.4 Low / Info — TODO/FIXME/HACK/XXX

| File:Line | Marker | Content |
|---|---|---|
| `internal/datasource/connector/yuque/connector.go` | TODO | "Serial fetch for v1 (user groups typically <10). TODO(perf): parallelize if slow." |
| `frontend/src/views/agent/AgentEditorModal.vue:2247` | XXX | Comment about default naming "My XXX" (UX hint). |
| `docker-compose.yml:392-393, 519-521` | Hint | "生产部署务必用 openssl rand 重新生成!" — Defaults explicitly marked as placeholders. |
| `rerank_server_demo.py:9-11` | Dead code | `# import os; # os.environ['CUDA_LAUNCH_BLOCKING']='1'`. |
| `docreader/utils/request.py:97` | Comment | "尝试保留格式，例如 test-req-1-XXX". |

---

## 7. Summary & Recommendations

### Quantitative picture

| Area | Critical | High | Medium | Low/Info |
|---|---|---|---|---|
| Go (handler/service/repo) | 4 | 6 | 5 | 1 |
| Go (infra/agent/IM/models) | 2 | 12 | 17 | 2 |
| Python DocReader | 3 | 9 | 13 | 12 |
| Frontend (Vue/TS) + Wails + Miniapp | 3 | 5 | 13 | 4 |
| Deployment / DevOps | 6 | 8 | 8 | 5 |
| **Total** | **18** | **40** | **56** | **24** |

### Hardening Roadmap

**Sprint 1 — "Stop the bleeding"**
1. Switch CORS in `internal/router/router.go:80-87` to an allowlist.
2. Force Slack verification in `internal/im/slack/adapter.go:100-103` (`return errors.New("signing secret missing")` if empty).
3. Remove all default secrets from compose — fail-fast when env unset (see `docker-compose.yml`).
4. Move JWT/refresh token out of `localStorage`, use HTTP-only cookies (`frontend/src/stores/auth.ts`, `frontend/src/utils/request.ts`).
5. Replace `asyncio.run()` in `docreader/parser/web_parser.py:100` with pure async or sync `requests`/`httpx`.
6. Fix mutable defaults in `docreader/parser/doc_parser.py:234-235`.
7. Add `resources.limits` for all services in `docker-compose.yml`.

**Sprint 2 — "No more silent failures"**
8. Clean up all `_ = c.ShouldBindJSON(...)` / `_ = json.Unmarshal(...)` / `_ = saveKBCloneProgress(...)`.
9. Replace `&http.Client{}` globally with a factory that always sets timeout + transport (`internal/models/rerank/*`, `internal/im/*`).
10. Add SSRF validation to all server-supplied URLs (`internal/im/wechat/adapter.go:155`).
11. `defer resp.Body.Close()` in `internal/infrastructure/docparser/mineru_*.go`.
12. `actions/checkout@v3` → `@v4` and SHA-pin all third-party actions.
13. Replace `:latest` tags with version pins; pin Helm to digest.

**Sprint 3 — "Cleanup"**
14. Reduce Vue `deep: true` watchers; `AbortController` in `streame.ts` and `views/chat/index.vue`.
15. Migrate Python typings to PEP 585/604 (all 18 files).
16. `print()` → `logger` in DocReader.
17. Pull IM webhook verification behind a common interface.
18. Reconcile `.env.example` ↔ `.env.lite.example` schemas.
19. `set -euo pipefail` in `scripts/dev.sh`, `scripts/build_images.sh`.

**Sprint 4 — "Hardening"**
20. CSP headers for frontend (`vite.config.ts`) and Wails build.
21. Distroless migration for `Dockerfile.app`.
22. Containers as non-root (USER) in all Dockerfiles.
23. Idempotent migrations + down counterparts.
24. `defusedxml` for DOCX/XML paths in DocReader.
25. Healthchecks for Redis, Qdrant, Weaviate; enforce `depends_on: service_healthy`.

---

*Created by five parallel static-analysis sub-agents on 2026-05-06. Findings are based on code reading without runtime verification; individual findings (e.g., exact sanitization paths in the frontend) should be manually confirmed.*

---

## 8. Follow-up Critical Audit (added 2026-05-06)

A second pass focused on areas the first audit skimmed (custom crypto, OIDC, init migrations, request signing). Five additional **Critical** findings:

### 8.1 AES-128-ECB used for WeChat media file encryption
**File:** `internal/im/wechat/crypto.go:10-72` (used at `internal/im/wechat/adapter.go:194`)
**Vector:** `decryptAES128ECB`/`encryptAES128ECB` use ECB mode without IV. ECB is deterministic — identical 16-byte plaintext blocks produce identical ciphertext blocks, leaking patterns (the famous "ECB-Penguin" weakness). Any media file content with repeating structure (PDFs, headers, padding) is partially recoverable from the ciphertext by an observer of CDN traffic.
**Evidence:**
```go
func decryptAES128ECB(ciphertext, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    // ...
    for i := 0; i < len(ciphertext); i += bs {
        block.Decrypt(plaintext[i:i+bs], ciphertext[i:i+bs])  // pure ECB, no IV
    }
```
**Why critical:** Confidentiality of every uploaded/downloaded WeChat media file is degraded. Plus PKCS#7 padding is verified non-constant-time (line 30-42: padding-oracle vector via early `break` on mismatch), enabling padding-oracle decryption attacks if the API returns differentiable error responses.
**Suggested fix:** Replace with AES-GCM (random IV per blob, AEAD). If iLink wire-protocol forces ECB on the client side, isolate the call site, document the limitation, and ensure the key is per-message and never reused.

### 8.2 OIDC nonce/state not validated server-side — CSRF / account takeover
**File:** `internal/handler/auth.go:243-303`
**Vector:** `decodeOIDCState(...)` only checks that `RedirectURI` is non-empty (line 300). The `Nonce` field in the state payload is never compared to a server-side store; in fact there is no server-side store. An attacker who can lure a victim to a forged OIDC callback URL (`/oidc/callback?state=<attacker-encoded>&code=<attacker-IdP-code>`) can complete the OAuth exchange in the victim's browser session, binding the victim's WeKnora session to the attacker's IdP identity.
**Evidence:**
```go
decodedState, err := decodeOIDCState(state)               // line 244 — only base64+JSON parse
// ... no nonce verification anywhere ...
resp, err := h.userService.LoginWithOIDC(ctx, code, ...)  // line 257 — proceeds on faith
```
And the payload definition shows the nonce is parsed but never used:
```go
type oidcStatePayload struct {
    Nonce       string `json:"nonce"`
    RedirectURI string `json:"redirect_uri,omitempty"`
}
```
**Why critical:** OIDC RFC 6749 §10.12 mandates state/nonce binding to the user's session. Missing this check is a textbook OAuth login CSRF that yields full account takeover or unauthorized linking.
**Suggested fix:** Issue an HTTP-only, SameSite=Strict cookie on the `/oidc/start` endpoint with the nonce; validate it against `decodedState.Nonce` in the callback. Reject mismatches.

### 8.3 `TENANT_AES_KEY` length never validated → service-wide panic on first encrypt
**File:** `internal/application/service/tenant.go:23-25`
**Vector:** `apiKeySecret()` returns whatever bytes are in `os.Getenv("TENANT_AES_KEY")`, including 0 bytes. `aes.NewCipher` only accepts 16/24/32-byte keys; any other length returns an error which the service `panic()`s on (line 254, "Failed to create AES cipher: ...").
**Evidence:**
```go
var apiKeySecret = func() []byte {
    return []byte(os.Getenv("TENANT_AES_KEY"))
}
// ...
block, err := aes.NewCipher(apiKeySecret())
if err != nil {
    panic("Failed to create AES cipher: " + err.Error())
}
```
**Why critical:** Misconfiguration crashes the goroutine on the first user/tenant create or API-key rotation request. Compared to `internal/utils/crypto.go:19-25` (which correctly returns `nil` when length ≠ 32), this path is markedly less defensive. In contrast, `SYSTEM_AES_KEY` callers degrade gracefully; tenant operations hard-fail.
**Suggested fix:** Validate key length at startup (`config.go` init) and `apiKeySecret()`; refuse to start the server if invalid, instead of panicking deep inside a request handler.

### 8.4 Init migration `00-init-db.sql` unconditionally drops 7 user-data tables
**File:** `migrations/mysql/00-init-db.sql:1-7` (and same pattern in `migrations/sqlite/000000_init.down.sql:1-13`)
**Vector:** The "init" up-migration begins with `DROP TABLE IF EXISTS tenants; DROP TABLE IF EXISTS models; DROP TABLE IF EXISTS knowledge_bases; DROP TABLE IF EXISTS knowledges; DROP TABLE IF EXISTS sessions; DROP TABLE IF EXISTS messages; DROP TABLE IF EXISTS chunks;`. Any operator who runs `migrate force 0` (a documented recovery step in `scripts/migrate.sh`) or accidentally re-applies the init migration on a populated DB **wipes all user data, knowledge bases, and chat history** with no confirmation prompt.
**Evidence:**
```sql
DROP TABLE IF EXISTS tenants;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS knowledge_bases;
DROP TABLE IF EXISTS knowledges;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS chunks;
CREATE TABLE tenants ( ... );
```
**Why critical:** Irreversible data loss on a single operator mistake. The MySQL init script does not even guard with `CREATE DATABASE IF NOT EXISTS` semantics; the SQLite down-migration is by definition destructive but the up-migration shouldn't be. Combined with the fact that production deployments share the migrate tool with dev workflows, this is a foot-gun.
**Suggested fix:** Remove the `DROP TABLE` lines from the up migration. If a clean-slate flag is needed, add a separate `migrations/reset/` set behind an explicit env switch.

### 8.5 API request signer uses `math/rand` for nonce + MD5 for signature
**File:** `internal/models/utils/signer.go:32, 53, 65-77`
**Vector:** `Sign(...)` builds an HMAC-style signature for upstream API calls (WeKnoraCloud). Two compounding flaws: (a) the nonce is generated with `math/rand.Intn` (line 74) — globally seeded with a constant time, so the sequence is predictable across processes; (b) the signature itself uses MD5 (lines 32, 53), which is collision-vulnerable. A network observer can predict future nonces from a few captured ones, then forge or replay signed requests.
**Evidence:**
```go
import (
    "crypto/md5"
    "math/rand"   // ← non-CSPRNG
)
// ...
bodyMD5 := md5Hex(bodyForHash)                           // line 32
signature := md5Hex(strings.Join(parts, "&"))            // line 53
// ...
func generateNonce(length int) string {
    b := make([]byte, length)
    for i := range b {
        b[i] = nonceChars[rand.Intn(len(nonceChars))]    // line 74 — predictable
    }
    return string(b)
}
```
**Why critical:** Replay protection is the entire point of a signed-request scheme. With a predictable nonce, an attacker observing one signed request can derive the seed and pre-compute valid future signatures, defeating the integrity guarantee. MD5 compounds the issue with collision attacks.
**Suggested fix:** Replace `math/rand` with `crypto/rand` for the nonce. If the upstream protocol allows it, switch to HMAC-SHA256; if MD5 is wire-mandated by the third-party API, at minimum use `crypto/rand` for the nonce and document the upstream constraint as known protocol weakness.

---

**Updated counts:** Critical 18 → **23**, total findings now 143.
