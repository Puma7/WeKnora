# WeKnora — Health-Check / Bit-Rot-Audit

> Periodischer Health-Check vom **2026-05-06**
> Branch: `claude/legacy-health-check-w6Tp9`
> Methode: Statische Analyse mit fünf parallelen Subagenten (Go-Backend, Go-Infrastruktur/IM, Python-DocReader, Vue/TS-Frontend, Deployment/DevOps).
> Status: **Nur Dokumentation** — keine Codeänderungen vorgenommen.

---

## 0. Projekt-Steckbrief

| Bereich | Sprache / Framework | Version | Dateien (ca.) |
|---|---|---|---|
| Backend | Go | 1.24.11 (per `go.mod`) | 671 `.go` |
| DocReader-Microservice | Python (gRPC) + FastAPI | Python 3.10.18 | 51 `.py` |
| Frontend | Vue 3 + TypeScript + Vite + Pinia + tdesign-vue-next | TS ~5.8, Vue 3.5 | 101 `.vue` / 64 `.ts` |
| Desktop | Wails | — | `cmd/desktop` |
| Mini-App | WeChat Miniprogram | — | `miniprogram/` |
| Datenbank | PostgreSQL + pgvector / SQLite + sqlite-vec | — | `migrations/` |
| Vector / Graph | Milvus, Qdrant, Weaviate, Neo4j | — | docker-compose |
| Observability | Langfuse, Jaeger | — | docker-compose |
| Deployment | Docker Compose, Helm, GitHub Actions | — | — |

---

## 1. TOP-20 — Was sofort weh tut

> Sortiert nach realistischem Schadenspotenzial. Schwere: **C**ritical / **H**igh / **M**edium / **L**ow.

| # | Schwere | Befund | Fundstelle |
|---|---|---|---|
| 1 | **C** | CORS: `AllowOrigins: ["*"]` **mit** `AllowCredentials: true` (CORS-Spec-Verstoß; Browser werfen Tokens weg, aber Pattern signalisiert grobes Missverständnis) | `internal/router/router.go:80-87` |
| 2 | **C** | Slack-Webhook-Signatur wird übergangen, wenn `signingSecret == ""` → unsignierte Requests werden akzeptiert | `internal/im/slack/adapter.go:100-103` |
| 3 | **C** | Hardcoded Default-Passwörter in Compose: Neo4j `password`, MinIO `minioadmin`, ClickHouse `clickhouse` | `docker-compose.yml:128, 237-238, 437-438` |
| 4 | **C** | Langfuse `ENCRYPTION_KEY` Default = 64 × `0` (Null-Entropie) | `docker-compose.yml:523` |
| 5 | **C** | `curl -LsSf https://astral.sh/uv/install.sh \| sh` ungeprüft im Build (Supply-Chain-RCE) | `docker/Dockerfile.app:79`, `scripts/start_all.sh:142` |
| 6 | **C** | JWT- und Refresh-Token in `localStorage` (XSS-anfällig) | `frontend/src/stores/auth.ts:63,77,82,144-152`, `frontend/src/utils/request.ts:32,41,134,146-147` |
| 7 | **C** | `asyncio.run()` aus synchronem Library-Code aufgerufen → `RuntimeError`, blockiert gRPC-Worker | `docreader/parser/web_parser.py:100` |
| 8 | **C** | Mutable Default-Argumente (Liste) in Methode → Shared State zwischen Aufrufen | `docreader/parser/doc_parser.py:234-235` |
| 9 | **C** | Keine `resources.limits` in **keinem** Compose-Service → ein OOM kippt den Host | `docker-compose.yml`, `docker-compose.dev.yml` |
| 10 | **H** | `c.ShouldBindJSON(&req)` wird mit `_ =` verworfen → ungültige Request-Bodies passieren stillschweigend | `internal/handler/tag.go:305` |
| 11 | **H** | `math/rand` (global geseedet) für `RandomSelect` in HTTP-Handler → schwach/vorhersagbar | `internal/handler/initialization.go:8, 2372` |
| 12 | **H** | `http.Client{}` ohne Timeout in vier Reranker-Implementierungen → kann ewig hängen | `internal/models/rerank/{zhipu,aliyun,jina,remote_api}*.go:63-90` |
| 13 | **H** | WeChat-Datei-Download ohne SSRF-Validierung der vom Server gelieferten URL | `internal/im/wechat/adapter.go:143-200` |
| 14 | **H** | 13× `_ = s.saveKBCloneProgress(...)` → Klon-/Move-Fortschritt geht still verloren | `internal/application/service/knowledge_clone_move.go:260,310,342,368,413,430,440,484,579,743,758,797,810` |
| 15 | **H** | `actions/checkout@v3` (deprecated, Auslauf 2025) | `.github/workflows/docker-image.yml:18,67,100,143` |
| 16 | **H** | `image: …:latest` für Neo4j, Jaeger, Dex, MinIO (dev), `weknora-ui`, `weknora-app`, `docreader` | `docker-compose.yml:3,29,144,174,290,366`, `docker-compose.dev.yml:39,106,165,190` |
| 17 | **H** | `ENCODED_PASSWORD=$(python3 -c "…quote('$DB_PASSWORD'…")` — DB-Passwort wird via Shell expandiert (Quote-Escape-Bug; Logging zeigt Klartext) | `scripts/migrate.sh:51-69` |
| 18 | **H** | Deep-Watcher (`{ deep: true }`) in 13 Vue-Komponenten auf große Listen/Stores | `frontend/src/views/chat/index.vue:213,218`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:826,851`, `frontend/src/views/knowledge/KnowledgeBase.vue:887`, weitere |
| 19 | **H** | `context.Background()` in vielen Goroutines/Request-Pfaden → Shutdown/Cancel wird ignoriert (12 h Timeout läuft weiter) | `internal/handler/initialization.go:1069-1073`, `internal/application/service/session.go:507-512`, `internal/handler/session/agent_stream_handler.go:392-405`, `internal/im/qaqueue.go:142,152,157,198,231,238,255,266`, `internal/im/service.go:511,536,562,579,589` |
| 20 | **H** | `subprocess.run(["which", …])` ohne `timeout=` (kann hängen, blockiert Worker) | `docreader/parser/doc_parser.py:258-259` |

---

## 2. Sprachspezifische Modernisierung (Bit-Rot)

### 2.1 Go (Ziel: 1.24.11)

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `internal/handler/initialization.go:8, 2372` | H | `math/rand` mit globalem Seed verwendet, statt `math/rand/v2`. In nebenläufigen HTTP-Handlern unbestimmt; in `RandomSelect(...)` schwach. |
| `internal/infrastructure/web_search/{google,bing,duckduckgo,baidu,ollama,tavily}.go` (mehrere Zeilen, siehe Kommentare im Code) | M | `fmt.Errorf` ohne `%w` → `errors.Is/As` greift nicht; uneinheitliche Fehlerketten. |
| diverse Pakete | L | Keine relevanten `io/ioutil`-Funde, keine `golang.org/x/net/context`, keine `strings.Title` — sehr sauber. Pflicht-`go vet` und `golangci-lint` aber empfehlenswert. |

### 2.2 Python (Ziel: 3.10+)

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `docreader/parser/web_parser.py:100` | C | `chtml = asyncio.run(self.scrape(url))` aus sync. Methode → Event-Loop-Konflikt im async Aufrufkontext. |
| 18+ Dateien (`docx_parser.py:10`, `chain_parser.py:43`, `excel_parser.py:59`, `doc_parser.py:234`, …) | H | Veraltete Typings: `from typing import List, Dict, Optional` statt PEP 585/604 (`list[...]`, `X \| None`). |
| Mehrere Parser | M | Häufiges `os.path.*` statt `pathlib.Path` (~30 Vorkommen). Reine Modernisierungsschuld. |
| `docreader/parser/doc_parser.py:223`, `docreader/utils/endecode.py:53` | M | `open(...)` ohne explizites `encoding=` (in Binary-Modus ok, in String-Modus eine offene Falle). |

### 2.3 Frontend (Vue 3 / TypeScript)

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `frontend/src/App.vue:147,149,158,160`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:642,646,1184,1186,1476,1480`, `frontend/src/views/settings/GeneralSettings.vue:243,245`, `frontend/src/components/KnowledgeBaseSelector.vue:128`, `frontend/src/utils/caret.ts:22` | H | 12× `@ts-ignore` ohne Kommentar/Begründung. |
| `frontend/src/stores/organization.ts:78-380`, `frontend/src/stores/menu.ts:80,113`, `frontend/src/components/MentionSelector.vue:164,168,206,208`, `frontend/src/types/tool-results.ts:288` | H | 46+ `any`, fast alle `catch`-Blöcke `catch (e: any)`. |
| `frontend/src/components/UserMenu.vue:379`, `frontend/src/components/doc-content.vue:150`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:690-691,1283`, `frontend/src/views/settings/AgentSettings.vue:847,981,1708`, `frontend/src/utils/tdesign-icon-offline.ts:55,69`, `frontend/src/composables/useTheme.ts:37` | M | Direktes `document.querySelector/getElementById` ohne `null`-Guard, statt Vue-`ref`s. |
| `frontend/src/components/IMChannelsOverviewPanel.vue:59`, `frontend/src/stores/organization.ts:201`, `frontend/src/views/settings/WebSearchSettings.vue:273-274`, `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1121-1122`, `frontend/src/views/knowledge/components/DocumentListView.vue:241,243,247,249-250` | M | 15+ Non-Null-Assertions (`!.`) auf Map-Treffer/Server-Daten, keine Laufzeit-Validierung. |
| `frontend/src/components/menu.vue:535` | L | Promise-Chain (`.then`) inmitten ansonsten `async/await`-getriebener Codebase. |

### 2.4 Deployment / DevOps

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `.github/workflows/docker-image.yml:18,67,100,143` | H | `actions/checkout@v3` (deprecated). |
| `docker-compose.yml:3,29,144,174,290,366`, `docker-compose.dev.yml:39,106,165,190` | H | `:latest`-Tags für Neo4j, Jaeger, Dex, MinIO (dev), `weknora-ui/app/docreader`. |
| `docker/Dockerfile.docreader:4` | M | `python:3.10.18-bookworm` — 3.10 EOL Oktober 2026. |
| `docker/Dockerfile.app:51` | M | `debian:12.12-slim` — Distroless oder Alpine wäre kleiner und sicherer. |

---

## 3. Bugs & unbehandelte Edge-Cases

### 3.1 Go

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `internal/handler/tag.go:305` | C | `_ = c.ShouldBindJSON(&req)` — Bind-Fehler verworfen; ungültiger JSON-Body liefert Zero-Values und läuft weiter. |
| `internal/handler/knowledgebase.go:285` | H | `_ = json.Unmarshal(b, &dataMap)` — Fehler verworfen; nachfolgender `if dataMap != nil`-Check fängt das nicht zuverlässig ab. |
| `internal/im/feishu/adapter.go:735,791,796,837,873,876,918` | H | 9× `payload, _ := json.Marshal(...)` → bei Marshal-Fehler werden invalide Payloads gesendet. |
| `internal/im/slack/adapter.go:143` | H | `json.Unmarshal(bodyBytes, &rawEvent)` Fehler ignoriert → `files` bleibt nil. |
| `internal/models/chat/ollama.go:193,200` | H | Tool-Parameter-Unmarshal-Fehler ignoriert → leeres `function.Parameters`. |
| `internal/im/slack/adapter.go:153` | H | Type-Assertion ohne `,ok` auf `rawEvent.Event.Files` → potenzielle Panic. |
| `internal/handler/initialization.go:1069-1073` | H | Goroutine mit `context.WithTimeout(context.Background(), 12*time.Hour)` — überlebt Server-Shutdown. |
| `internal/handler/session/agent_stream_handler.go:392-405` | H | `bgCtx := context.Background()` für `AppendEvent` mitten im Request-Pfad → ignoriert Client-Cancel. |
| `internal/application/service/session.go:507-512` | H | Detached `context.Background()` in Async-Goroutine. |
| `internal/im/qaqueue.go:142,152,157,198,231,238,255,266`; `internal/im/service.go:511,536,562,579,589` | M | 13 `context.Background()`-Vorkommen in Queue-/Service-Pfaden. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:65,162,182,222,229,239,294,296,327`; `:177` | M/H | 9× `context.Background()` plus `resp.Body.Close()` ohne `defer` (Leak-Risiko bei Fehlern zwischen Read und Close). |
| `internal/infrastructure/docparser/mineru_converter.go:263` | H | dito: `resp.Body.Close()` ohne `defer`. |
| `internal/handler/initialization.go:2038` | M | `defer file.Close()` auf Upload — Fehler werden geschluckt; bei Schreib-Flush-Fehlern kein Hinweis. |
| `internal/agent/tools/web_fetch.go:286`; `internal/infrastructure/web_fetch/fetcher.go:53` | M | DNS-Lookup mit `context.Background()` — kann ohne Timeout hängen. |
| `internal/application/service/knowledge_clone_move.go:260…810` (13 Stellen) | H | `_ = s.saveKBCloneProgress(...)` — Fortschritt-Persistierung schweigend verloren. |

### 3.2 Python

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `docreader/parser/doc_parser.py:234-235` | C | Mutable Defaults: `possible_path: List[str] = []`, `environment_variable: List[str] = []` werden mutiert (`.extend(...)` Z. 247) → Cross-Call-State. |
| `docreader/parser/chain_parser.py:62-66` | H | `except Exception: logger.exception(...); continue` — fängt zu breit, frisst `KeyboardInterrupt`/`SystemExit`. |
| `docreader/parser/docx_parser.py:337-342` | M | Breiter `except Exception as e: logger.error(...) continue` ohne Fallback-Doc oder Re-Raise. |
| `docreader/parser/docx_parser.py:310` | M | `img = img[0]` ohne `if not img:` davor. |
| `docreader/parser/docx_parser.py:178` | M | `image_data.object.close()` nur im Success-Path → bei Fehler in `.decode_image()` Handle-Leak. |

### 3.3 Frontend

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `frontend/src/components/Input-field.vue:549`, `frontend/src/components/menu.vue:680`, `frontend/src/hooks/useKnowledgeBase.ts:71,180`, `frontend/src/views/settings/ChatHistorySettings.vue:184`, `frontend/src/views/settings/RetrievalSettings.vue:179` | H | 6× `.catch(() => {})` — Fehler werden ohne Log oder UI-Feedback geschluckt. |
| `frontend/src/views/chat/index.vue:162-202` | M | `suggestedQuestionsFetchId`-Pattern unvollständig; `setTimeout`-Debounce ohne `AbortController` → Stale-Antworten möglich beim Sessionwechsel. |
| `frontend/src/views/chat/components/botmsg.vue:260` | M | `parentMd.value.addEventListener(...)` ohne `null`-Check. |
| `frontend/src/utils/mermaidViewer.ts:152-154,212-213,233` | M | `divEl` wird in Cleanup ohne Existenz-Check entfernt. |
| `frontend/src/components/doc-content.vue:905-906` | M | `v-for`-`:key="index"` auf `processedChunks` — bricht bei Reorder/Filter. |
| `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1286,1301,1316,1519,1549,1616,1632,1645,1652`; `frontend/src/views/agent/AgentEditorModal.vue:3256,3298,3344` | M | `addEventListener` ohne korrespondierendes `removeEventListener` in `onUnmounted` — Memory-Leak bei häufigem Mount/Unmount. |
| `frontend/src/components/IMChannelPanel.vue:575,724` | M | `wechatPollTimer = setTimeout(pollOnce, 500)` rekursiv neu gesetzt; `stopWeChatPolling()` ruft `clearTimeout` evtl. zu spät → Timer überlebt Unmount. |
| `frontend/src/api/chat/streame.ts:149-159` | M | `chunkHandler` wird nicht in `try/catch` gewrapt → Stream stoppt still bei Handler-Fehler. |

### 3.4 Deployment

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `docker-compose.yml` (Redis 222-228, Qdrant 299-312, Weaviate 342-363, Milvus gRPC :19530) | H | Healthchecks fehlen oder decken nur Web-Port (9091), nicht den gRPC-Endpoint. `depends_on` mit `service_started` löst Startup-Race aus. |
| `docker-compose.yml:212-217` | M | Postgres-Healthcheck: `interval=10s`, `retries=3`, `start_period=30s` — wenn Init >30 s, schlägt's fehl, bevor `start_period` abläuft. |
| `docker-compose.yml:149-154` | M | `app` hängt an Redis mit `service_started` statt `service_healthy`. |
| `migrations/` | M | Mehrere Migrations ohne `IF NOT EXISTS`/Down-Counterpart — nicht idempotent. |
| `scripts/dev.sh`, `scripts/build_images.sh` | H | Kein `set -euo pipefail`. Unset-Vars und kaputte Pipes failen still. |

---

## 4. Inkonsistenzen & Architektur-Drift

### 4.1 Go

- **Webhook-Verifikation uneinheitlich pro IM-Plattform** — Slack lässt unsignierte durch, Telegram nutzt `subtle.ConstantTimeCompare`, WeCom/DingTalk `hmac.Equal`, Feishu Token-basiert, WeChat verweigert Webhooks. Kein gemeinsames Interface, keine zentrale Policy. (`internal/im/{slack,telegram,wecom,dingtalk,feishu,wechat}/*adapter*.go`)
- **HTTP-Clients uneinheitlich** — Reranker: `&http.Client{}` (kein Timeout); WeChat: 30 s; Feishu: 10 s; `web_fetch`: SSRF-Wrapper. Es gibt keinen zentralen Factory-Punkt. (`internal/models/rerank/*`, `internal/im/wechat/adapter.go:40`, `internal/im/feishu/adapter.go:36`)
- **Magic Numbers** — Queue-Limits `internal/im/qaqueue.go:17-33` definiert, aber `streamReaperInterval`, `streamOrphanTTL`, `defaultCloudTimeout` (`internal/infrastructure/docparser/mineru_cloud_converter.go:120`) sind verstreut.
- **Duplikat-Code** — `cardkitCreate` und `sendCardByCardID` in `internal/im/feishu/adapter.go:735-829` bauen den HTTP-Request fast identisch auf.
- **`cmd/handler/knowledgebase.go:284-290`** — Struct → JSON → Map → JSON nur um *ein* Feld zu setzen. Eigener DTO oder `MarshalJSON` wäre sauberer.
- **SQLite-Repository: `fmt.Sprintf` für Tabellennamen** — `internal/application/repository/retriever/sqlite/repository.go:503,520,533`. Tabellennamen sind dimension-abgeleitet (`vecTableName(dim)`), keine User-Inputs — aber das Pattern setzt schlechte Präzedenz.

### 4.2 Python

- **`print()` in Library-Code statt Logger** — `docreader/parser/chain_parser.py:179`, `markdown_parser.py:124`, `web_parser.py:149-162`, `excel_parser.py:114-118`.
- **Uneinheitliche Fehlermuster** — manche Parser geben `Document(content="Error...")`, andere `Document()`, andere raisen. (`docreader/parser/pdf_parser.py:54`, `web_parser.py:112-113`)
- **Logger pro Modul mit eigenem `setLevel(...)`** — `docreader/parser/base_parser.py:10`, `config.py:7` — sollte zentral konfiguriert sein.
- **`enableMultimodal` vs. `enable_multimodal`, `maxPages` vs. `max_pages`** in `docreader/parser/docx_parser.py:294+`.
- **Hohe Code-Duplikation** in den Parsern (`Docx`, `DocxParser`, `Docx2Parser`, `MarkitdownParser` + Fallback-Ketten). Eine gemeinsame Basis fehlt.

### 4.3 Frontend

- **Zwei API-Clients** — `frontend/src/utils/request.ts` (axios) vs. `frontend/src/api/chat/streame.ts` (fetchEventSource). Token-Refresh und Fehlerbehandlung in beiden separat.
- **Inkonsistente Fehlerformen** — `request.ts:199-215` liefert Mischobjekt; Stores ziehen daraus mal `.message`, mal `.error.message`. Kein gemeinsames `ErrorResponse`-Interface.
- **Composition vs. Options API** — 99% Composition API, aber Composables in `src/composables/` mischen `ref`/`reactive` ohne klare Konvention.

### 4.4 Deployment

- **`.env.example` ↔ `.env.lite.example`** — Schlüssel-Schema divergiert (Lite hat `DB_PATH` statt `DB_HOST/DB_PORT`; ~12 Langfuse-/DB-Keys fehlen). Kein dokumentierter Mapping.
- **`docker-compose.yml` ↔ `docker-compose.dev.yml`** — Dev nutzt `:latest` für MinIO/Jaeger/Neo4j; Prod hat Versionspins. Parität gebrochen.
- **`helm/values.yaml:60-66`** — `image.tag: ""` fällt auf `Chart.appVersion=v0.5.1`, kein Digest-Pin.
- **`docker-compose.yml:127`** — Username `neo4j` hardcoded, nur Passwort variabilisiert.

---

## 5. Performance & Ressourcen-Leaks

### 5.1 Go

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `internal/models/rerank/{zhipu,aliyun,jina,remote_api}*.go:63-90` | H | `&http.Client{}` ohne Timeout in vier Implementierungen. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:177`, `mineru_converter.go:263` | H | `resp.Body.Close()` ohne `defer`. |
| `internal/im/feishu/adapter.go:581` (`feishuStreams map[...]`) und Pendants in Slack/Telegram | M | Stream-Maps ohne harte Obergrenze; Cleanup hängt am Reaper-Goroutine. |
| `internal/sandbox/validator.go:466` | M | `compilePatterns()` kompiliert pro Aufruf — falls in Hot-Path, Allokationen pro Request. |
| `internal/infrastructure/docparser/mineru_cloud_converter.go:294-296` | L | Vollständiges `extract_result`-JSON ins Log (nur 4000 Zeichen abgeschnitten). |
| `internal/im/service.go:589-591` | M | WS-Leader-Renewal-Goroutine via `WithCancel(context.Background())`. Cancel-Fn gespeichert, aber nicht garantiert aufgerufen → Leak bis Redis-TTL (15 s). |
| `internal/im/feishu/adapter.go:67-85` | M | `startStreamReaper()` über `sync.Once` gestartet, aber `StopStreamReaper` nicht garantiert auf Shutdown gerufen. |
| `internal/models/rerank/*.go` (default `&http.Client{}`) | M | Nutzt System-`HTTP_PROXY`/`HTTPS_PROXY` automatisch — API-Calls können ungewollt durch Proxy fließen. |

### 5.2 Python

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `docreader/parser/web_parser.py:100` | C | `asyncio.run()` blockiert den gRPC-Worker-Thread komplett (Loop-Erzeugung + Teardown pro Aufruf). |
| `docreader/parser/doc_parser.py:258-259` | H | `subprocess.run(["which", ...])` ohne `timeout` — `which` kann hängen. |
| `docreader/parser/docx_parser.py:112-113` | M | `ProcessPoolExecutor(max_workers=min(4, os.cpu_count() or 2))` — fix verdrahtet, kein Config-Hook, Speicher-Spitzen bei großen Dokumenten. |
| `docreader/parser/doc_parser.py:118` (`TempFileContext(content, ".doc")`) | M | Komplett-In-Memory; kein Streaming-Fallback (gRPC-Limit 50 MB laut `config.py:71`). |

### 5.3 Frontend

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| 13 Stellen, u. a. `frontend/src/components/doc-content.vue:355`, `frontend/src/components/Input-field.vue:1495`, `frontend/src/views/chat/index.vue:213,218`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:826,851`, `frontend/src/views/creatChat/creatChat.vue:168-169`, `frontend/src/views/knowledge/KnowledgeBase.vue:887`, `frontend/src/views/knowledge/components/FAQEntryManager.vue:2826,3270` | H | `watch(..., { deep: true })` auf große Listen/Stores → O(n) pro Change. |
| `frontend/src/components/document-preview.vue:413`, `frontend/src/views/chat/components/botmsg.vue:30` | M | Bilder ohne `loading="lazy"` und ohne `width`/`height` (CLS). |
| `frontend/src/api/chat/streame.ts:26-187` | M | Keine Stream-Abort beim Routenwechsel/Session-Switch — bisherige SSE läuft weiter. |
| `frontend/package.json` (`tdesign-vue-next`) + `frontend/vite.config.ts` | L | Kein expliziter Tree-Shaking-Hinweis; Bundle-Size beobachten. |

### 5.4 Deployment

| Datei:Zeile | Schwere | Befund |
|---|---|---|
| `docker-compose.yml`, `docker-compose.dev.yml` (alle Services) | C | Keine `deploy.resources.limits`/`requests`. |
| `docker-compose.yml:209,601-612` | H | `postgres-data` als anonymes named-Volume — bei `docker volume rm` weg, kein Backup-Hint. |
| `docker-compose.yml:270,440-441` | M | `jaeger_data`, `langfuse_clickhouse_logs` ohne Rotation/TTL → Disk füllt sich bei Langlauf. |

---

## 6. Sicherheit & Technische Schulden

### 6.1 Critical (sofort handeln)

| Datei:Zeile | Befund |
|---|---|
| `internal/router/router.go:80-87` | `AllowOrigins: ["*"]` + `AllowCredentials: true`. CORS-Spec verbietet diese Kombination; Browser werfen entweder Cookies/Headers weg, oder es entstehen offene Cross-Origin-Pfade. → Whitelist explizit setzen oder `AllowOriginFunc`. |
| `internal/im/slack/adapter.go:100-103` | `if a.signingSecret == "" { return nil }` — fehlende Konfiguration ⇒ Bypass der Signaturprüfung. |
| `docker-compose.yml:128, 237-238, 437-438, 522-525`, `.env.example:211` | Hardcoded Defaults: `NEO4J_PASSWORD=password`, `MINIO_ROOT_PASSWORD=minioadmin`, ClickHouse `clickhouse/clickhouse`, `LANGFUSE_SALT=…change-me`, `JWT_SECRET=weknora-jwt-secret`. |
| `docker-compose.yml:523`, `docker-compose.dev.yml:333` | `LANGFUSE_ENCRYPTION_KEY=00…00` (64 Nullen). |
| `docker/Dockerfile.app:79`, `scripts/start_all.sh:142` | `curl … \| sh` ohne Checksum/Signaturprüfung — Supply-Chain. |
| `frontend/src/stores/auth.ts:63,77,82,144-152`, `frontend/src/utils/request.ts:32,41,134,146-147` | JWT + Refresh-Token in `localStorage` — bei XSS leicht abgreifbar. |

### 6.2 High

| Datei:Zeile | Befund |
|---|---|
| `internal/im/feishu/adapter.go:746` | `Authorization: Bearer …` in Requests, die durch Logging-Middleware durchlaufen können. |
| `internal/im/wechat/adapter.go:155` | `http.NewRequestWithContext(ctx, GET, msg.FileKey, nil)` — keine SSRF-Validierung der vom Server gelieferten URL. (`internal/infrastructure/docparser/mineru_cloud_converter.go:349` macht das richtig.) |
| `internal/im/wechat/adapter.go:31-32`, Feishu, Telegram | Hardcoded API-Basis-URLs ohne Override-Möglichkeit für Tests/Self-Hosting. |
| `internal/im/slack/adapter.go:100` | Verifikation an `slack.NewSecretsVerifier()` delegiert; nicht klar, ob konstantzeitig. |
| `internal/handler/initialization.go:2372` | `math/rand` für Auswahllogik in HTTP-Handler. |
| `docreader/parser/web_parser.py:39-84` | `scrape(url)` akzeptiert beliebige URL — Playwright lädt auch `file://` etc. SSRF-/LFI-Risiko. |
| `rerank_server_demo.py:46` | `model_path = '/data1/home/lwx/work/Download/rerank_model_weight'` — Hardcoded Pfad, fremder Hostname-Kontext. |
| `rerank_server_demo.py:66` | `/rerank` ohne Auth/Rate-Limit/Body-Size-Check. |
| `frontend/src/components/GlobalCommandPalette.vue:127,154` | `v-html="highlight(...)"` mit Custom-Regex statt DOMPurify. |
| `frontend/src/views/chat/components/AgentStreamDisplay.vue:329,365` | `v-html="floatPopup.content"`, `v-html="wikiDrawerContent"` — Sanitisierung an Setter-Stelle nicht garantiert. |
| `frontend/src/components/document-preview.vue:121,331`, `frontend/src/views/knowledge/wiki/WikiBrowser.vue:1083,1099,1203`, `frontend/src/views/chat/components/AgentStreamDisplay.vue:1290` | Direktes `innerHTML = '...'` (auch wenn Quellen aktuell intern sind, ist es schwerer auditbar als `v-html` + DOMPurify). |
| `frontend/src/views/auth/Login.vue:96,102,111`; `frontend/src/views/chat/components/docInfo.vue:17`; `frontend/src/views/settings/StorageEngineSettings.vue:253-362` | `target="_blank"` ohne `rel="noopener noreferrer"`. |
| `docker-compose.yml:114-115, 290-291` | MinIO-Console (9001) und Neo4j (7474/7687) auf Host exposed; mit Defaults trivial übernehmbar. |
| `scripts/migrate.sh:67-69` | `echo "DB_PASSWORD: ${DB_PASSWORD}"` schreibt Passwort in Logs. |

### 6.3 Medium

| Datei:Zeile | Befund |
|---|---|
| `docreader/parser/docx_parser.py:17` | `python-docx` nutzt `xml.etree` — kein `defusedxml`. Bei nicht-vertrauenswürdigen DOCX XXE-Risiko. |
| `docreader/main.py:115` | `logger.info("Read(URL): url=%s", request.url)` — URL inkl. Query-Parameter in Logs. |
| `internal/handler/user.go` (zu prüfen) | Logging-Pfade auf Token/PII auditieren. |
| `helm/values.yaml:84` | Kommentar „Disabled - official images run as root" — unaufgelöste Security-Anforderung. |
| `helm/`, GitHub-Workflows | `permissions:` nicht restriktiv gesetzt; Third-Party-Actions ohne SHA-Pin (zu verifizieren). |
| `docker-compose.yml:127` | NEO4J-Username hardcoded. |
| `cmd/desktop/main.go:46-100` | `dragHandlerJS` per `DomReady` injiziert; CSP-Konfiguration nicht erkennbar. |
| `miniprogram/utils/config.js:27` | `wx.setStorageSync('weknora_settings', next)` enthält `apiKey` im Klartext. |
| `frontend/vite.config.ts:42-53` | Keine produktions-relevanten Security-Header (`Content-Security-Policy`, `X-Frame-Options`). |

### 6.4 Low / Info — TODO/FIXME/HACK/XXX

| Datei:Zeile | Marker | Inhalt |
|---|---|---|
| `internal/datasource/connector/yuque/connector.go` | TODO | „Serial fetch for v1 (user groups typically <10). TODO(perf): parallelize if slow." |
| `frontend/src/views/agent/AgentEditorModal.vue:2247` | XXX | Kommentar zu Default-Naming „My XXX" (UX-Hinweis). |
| `docker-compose.yml:392-393, 519-521` | Hinweis | „生产部署务必用 openssl rand 重新生成!" — Defaults ausdrücklich als Platzhalter markiert. |
| `rerank_server_demo.py:9-11` | Toter Code | `# import os; # os.environ['CUDA_LAUNCH_BLOCKING']='1'`. |
| `docreader/utils/request.py:97` | Kommentar | „尝试保留格式，例如 test-req-1-XXX". |

---

## 7. Zusammenfassung & Empfehlungen

### Quantitatives Bild

| Bereich | Critical | High | Medium | Low/Info |
|---|---|---|---|---|
| Go (Handler/Service/Repo) | 4 | 6 | 5 | 1 |
| Go (Infra/Agent/IM/Models) | 2 | 12 | 17 | 2 |
| Python DocReader | 3 | 9 | 13 | 12 |
| Frontend (Vue/TS) + Wails + Miniapp | 3 | 5 | 13 | 4 |
| Deployment / DevOps | 6 | 8 | 8 | 5 |
| **Gesamt** | **18** | **40** | **56** | **24** |

### Härtungs-Roadmap

**Sprint 1 — „Stop-the-bleeding"**
1. CORS in `internal/router/router.go:80-87` auf Allowlist umstellen.
2. Slack-Verifikation in `internal/im/slack/adapter.go:100-103` strikt fordern (`return errors.New("signing secret missing")` falls leer).
3. Alle Default-Secrets aus Compose entfernen — Fail-Fast wenn Env unset (siehe `docker-compose.yml`).
4. JWT/Refresh-Token aus `localStorage` raus, in HttpOnly-Cookie umziehen (`frontend/src/stores/auth.ts`, `frontend/src/utils/request.ts`).
5. `asyncio.run()` in `docreader/parser/web_parser.py:100` durch reines async oder synchrones `requests`/`httpx` ersetzen.
6. Mutable Defaults in `docreader/parser/doc_parser.py:234-235` fixen.
7. `resources.limits` für alle Services in `docker-compose.yml`.

**Sprint 2 — „No more silent failures"**
8. Alle `_ = c.ShouldBindJSON(...)`/`_ = json.Unmarshal(...)`/`_ = saveKBCloneProgress(...)` aufräumen.
9. `&http.Client{}` global durch eine Factory ersetzen, die immer Timeout + Transport vorgibt (`internal/models/rerank/*`, `internal/im/*`).
10. SSRF-Validierung auf alle servergelieferten URLs (`internal/im/wechat/adapter.go:155`).
11. `defer resp.Body.Close()` in `internal/infrastructure/docparser/mineru_*.go`.
12. `actions/checkout@v3` → `@v4` und SHA-Pin für alle Third-Party-Actions.
13. `:latest` Tags durch Versionspins ersetzen; Helm auf Digest-Pin.

**Sprint 3 — „Cleanup"**
14. Vue `deep: true`-Watcher reduzieren; `AbortController` in `streame.ts` und `views/chat/index.vue`.
15. Python-Typings auf PEP 585/604 umstellen (alle 18 Dateien).
16. `print()` → `logger` im DocReader.
17. Webhook-Verifikation in IM-Adaptern hinter ein gemeinsames Interface ziehen.
18. `.env.example` ↔ `.env.lite.example` Schema vereinheitlichen.
19. `set -euo pipefail` in `scripts/dev.sh`, `scripts/build_images.sh`.

**Sprint 4 — „Hardening"**
20. CSP-Header für Frontend (`vite.config.ts`) und Wails-Build.
21. Distroless-Migration für `Dockerfile.app`.
22. Container als non-root (USER) in allen Dockerfiles.
23. Migrations idempotent + Down-Counterparts.
24. `defusedxml` für DOCX-/XML-Pfade in DocReader.
25. Healthchecks für Redis, Qdrant, Weaviate; `depends_on: service_healthy` durchziehen.

---

*Erstellt durch fünf parallele statische Analyse-Subagenten am 2026-05-06. Findings basieren auf Code-Lesen ohne Laufzeit-Verifikation; einzelne Befunde (z. B. genaue Sanitisierungs-Wege im Frontend) sollten manuell bestätigt werden.*
