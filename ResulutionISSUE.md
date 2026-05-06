# WeKnora — Resolution Plan

> Response to `ISSUE.md` from 2026-05-06.
> Role: **Resolution Engineer** — minimal-invasive surgery, no refactors, no symptom doctoring.
> Status: **Proposal only.** Patches are formulated as diff sketches; no source files in this commit are modified.
>
> Reading guide:
> - `// CHANGED` = line replaced
> - `// NEW` = line added
> - `// REMOVE` = line dropped
>
> Layout: all **Critical** first, then all **High**, then **Medium** (shorter). Within each, grouped by area like in `ISSUE.md`.

---

## CRITICAL FIXES

### 🔴 Problem 1: CORS accepts `*` together with `AllowCredentials: true`
🔍 **Root cause:** The wildcard origin combined with credentials is invalid per the CORS spec; it signals a missing allowlist instead of a deliberate security choice.
✅ **Fix strategy:** Read allowlist from `ALLOWED_ORIGINS` env (comma list), default to empty list. Use `AllowOriginFunc` so wildcards & subdomain patterns stay cleanly separable. No refactor of the rest of the middleware chain.
💻 **Code change** — `internal/router/router.go:74-87`
```go
// CHANGED: ensure related imports at the top of the file
import (
    "os"      // NEW
    "strings" // NEW
    // ... existing imports unchanged
)

func NewRouter(params RouterParams) *gin.Engine {
    r := gin.New()
    r.ContextWithFallback = true

    // CHANGED: allowlist instead of "*" + AllowCredentials
    allowed := strings.Split(strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")), ",") // NEW
    for i, o := range allowed {                                                    // NEW
        allowed[i] = strings.TrimSpace(o)                                          // NEW
    }                                                                              // NEW
    r.Use(cors.New(cors.Config{
        AllowOriginFunc: func(origin string) bool { // NEW
            for _, o := range allowed {              // NEW
                if o != "" && o == origin {          // NEW
                    return true                      // NEW
                }                                    // NEW
            }                                        // NEW
            return false                             // NEW
        },                                           // NEW
        AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key", "X-Request-ID"},
        ExposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin"},
        AllowCredentials: true,
        MaxAge:           12 * time.Hour,
    }))
    // ...
}
```
🛡️ **No-regression check:** With empty `ALLOWED_ORIGINS`, all browser cross-origin requests are rejected — same-origin calls (Vite proxy in dev, nginx reverse proxy in prod) are not affected. Documentation hint in `.env.example` is a Sprint-2 task.

---

### 🔴 Problem 2: Slack webhook without signing secret = auth bypass
🔍 **Root cause:** The early return on empty `signingSecret` turns a misconfiguration into an auth bypass instead of a hard fail.
✅ **Fix strategy:** Return an error on empty secret instead of `nil`. The webhook endpoint then fails safely on misconfiguration instead of accepting unsigned requests. Nothing else changes.
💻 **Code change** — `internal/im/slack/adapter.go:100-103`
```go
func (a *Adapter) VerifyCallback(c *gin.Context) error {
    if a.signingSecret == "" {
        return fmt.Errorf("slack signing secret not configured") // CHANGED: was return nil
    }
    // remaining body unchanged
}
```
🛡️ **No-regression check:** Adapter constructors that instantiate Slack without a secret will now hit a hard error on the first webhook call — desired. Tests that simulate `signingSecret=""` need updating.

---

### 🔴 Problem 3: Hardcoded default passwords in `docker-compose.yml`
🔍 **Root cause:** Compose var defaults (`${VAR:-default}`) turn "variable not set" into an implicit choice of well-known default credentials.
✅ **Fix strategy:** Remove the default values so compose aborts with a clear error ("variable is not set"). The operator is forced to set real values in `.env`.
💻 **Code change** — `docker-compose.yml:128, 237-238, 437-438, 522-523, 525`, `.env.example:211`
```yaml
# CHANGED: docker-compose.yml — defaults removed
- NEO4J_PASSWORD=${NEO4J_PASSWORD:?NEO4J_PASSWORD must be set}
- NEO4J_AUTH=${NEO4J_USERNAME:-neo4j}/${NEO4J_PASSWORD:?NEO4J_PASSWORD must be set}

- MINIO_ROOT_USER=${MINIO_ACCESS_KEY_ID:?MINIO_ACCESS_KEY_ID must be set}
- MINIO_ROOT_PASSWORD=${MINIO_SECRET_ACCESS_KEY:?MINIO_SECRET_ACCESS_KEY must be set}

CLICKHOUSE_USER: ${LANGFUSE_CLICKHOUSE_USER:?LANGFUSE_CLICKHOUSE_USER must be set}
CLICKHOUSE_PASSWORD: ${LANGFUSE_CLICKHOUSE_PASSWORD:?LANGFUSE_CLICKHOUSE_PASSWORD must be set}

SALT: ${LANGFUSE_SALT:?LANGFUSE_SALT must be set (openssl rand -base64 32)}
NEXTAUTH_SECRET: ${LANGFUSE_NEXTAUTH_SECRET:?LANGFUSE_NEXTAUTH_SECRET must be set}
```
```bash
# CHANGED: .env.example:211 — instruction instead of usable default
JWT_SECRET= # REQUIRED: openssl rand -hex 32
```
🛡️ **No-regression check:** `.env.example` must carry a clear instruction in sync (see Problem 5). Existing `.env` files with values set are unaffected.

---

### 🔴 Problem 4: Langfuse `ENCRYPTION_KEY` defaults to 64 × `0`
🔍 **Root cause:** An all-zeros constant as a default key has zero entropy and is trivially brute-forceable.
✅ **Fix strategy:** Remove the default — compose fails fast. No implementation work, just default stripping.
💻 **Code change** — `docker-compose.yml:523`, `docker-compose.dev.yml:333`
```yaml
# CHANGED
ENCRYPTION_KEY: ${LANGFUSE_ENCRYPTION_KEY:?LANGFUSE_ENCRYPTION_KEY must be set (openssl rand -hex 32)}
```
🛡️ **No-regression check:** Existing installations that used the default are already broken security-wise — they *must* rotate the key once. Reference in CHANGELOG.

---

### 🔴 Problem 5: `curl … | sh` for `uv`/Ollama in build & setup script
🔍 **Root cause:** The pipe strips any tamper-detection from the distribution — content changes upstream land unfiltered in the image.
✅ **Fix strategy:** Pin a concrete release tag and verify SHA-256 checksum in the Dockerfile/script. If `uv` directly via `pip install` is desired, alternatively `pip install --require-hashes`. Minimal-invasive variant with checksum here.
💻 **Code change** — `docker/Dockerfile.app:79`
```dockerfile
# CHANGED: tag pin + checksum verification
ARG UV_VERSION=0.5.14
ARG UV_INSTALL_SHA256=<actual-sha256-from-release>
RUN curl -LsSf -o /tmp/uv-install.sh \
        "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-installer.sh" && \
    echo "${UV_INSTALL_SHA256}  /tmp/uv-install.sh" | sha256sum -c - && \
    CARGO_HOME=/home/appuser/.cargo UV_INSTALL_DIR=/home/appuser/.local/bin \
        sh /tmp/uv-install.sh && \
    rm -f /tmp/uv-install.sh
```
💻 **Code change** — `scripts/start_all.sh:142`
```bash
# CHANGED
OLLAMA_VERSION="0.4.4"   # NEW
OLLAMA_SHA256="<actual-sha256-from-release>"  # NEW
curl -fsSL -o /tmp/ollama-install.sh \
    "https://github.com/ollama/ollama/releases/download/v${OLLAMA_VERSION}/ollama-install.sh" # CHANGED
echo "${OLLAMA_SHA256}  /tmp/ollama-install.sh" | sha256sum -c -                              # NEW
sh /tmp/ollama-install.sh                                                                     # NEW
rm -f /tmp/ollama-install.sh                                                                  # NEW
```
🛡️ **No-regression check:** On upstream update: hash bump required (intentional — explicit approval). The `ARG` values can be overridden as build-args in CI.

---

### 🔴 Problem 6: JWT/refresh token in `localStorage`
🔍 **Root cause:** `localStorage` is readable via XSS and persists tokens longer than necessary — the backend session should be set as an HTTP-only cookie.
✅ **Fix strategy:** Migration is a larger backend/frontend coordination. As a minimal-invasive **interim fix**, store tokens in `sessionStorage` (cleared on tab close, not permanent) and complement with a XSS reduction via `dompurify` audit (Problem 27). Full move to HTTP-only cookies becomes a sprint ticket.
💻 **Code change** — `frontend/src/stores/auth.ts:75-83, 144-152, 162-167`
```ts
// CHANGED: localStorage → sessionStorage for auth tokens
const setToken = (tokenValue: string) => {
  token.value = tokenValue
  sessionStorage.setItem('weknora_token', tokenValue)        // CHANGED
}

const setRefreshToken = (refreshTokenValue: string) => {
  refreshToken.value = refreshTokenValue
  sessionStorage.setItem('weknora_refresh_token', refreshTokenValue) // CHANGED
}

// in logout():
sessionStorage.removeItem('weknora_token')          // CHANGED
sessionStorage.removeItem('weknora_refresh_token')  // CHANGED

// in initFromStorage():
const storedToken = sessionStorage.getItem('weknora_token')               // CHANGED
const storedRefreshToken = sessionStorage.getItem('weknora_refresh_token') // CHANGED
```
💻 **Code change** — `frontend/src/utils/request.ts:32, 41, 134, 146-147`
```ts
// CHANGED: all 4 locations
const token = sessionStorage.getItem('weknora_token')               // CHANGED
const refreshToken = sessionStorage.getItem('weknora_refresh_token') // CHANGED
sessionStorage.setItem('weknora_token', newToken)                    // CHANGED
sessionStorage.setItem('weknora_refresh_token', newRefreshToken)     // CHANGED
```
🛡️ **No-regression check:** Users with an open tab today will have to log in once after deploy. "Remember me" persistence wish must be solved as a follow-up ticket via refresh token in HTTP-only cookie.

---

### 🔴 Problem 7: `asyncio.run()` from synchronous library code
🔍 **Root cause:** `asyncio.run()` must not be called from a function reachable by a running event loop — otherwise `RuntimeError: asyncio.run() cannot be called from a running event loop`.
✅ **Fix strategy:** Instead of creating a new loop per call, keep a dedicated background loop in its own thread and submit tasks via `run_coroutine_threadsafe`. Minimal-invasive: local singleton helper in the module, caller signature unchanged.
💻 **Code change** — `docreader/parser/web_parser.py:1-15, 95-101`
```python
import asyncio
import threading                    # NEW
# ... existing imports

_loop = None                        # NEW
_loop_lock = threading.Lock()       # NEW

def _get_loop():                    # NEW
    global _loop
    with _loop_lock:
        if _loop is None or _loop.is_closed():
            _loop = asyncio.new_event_loop()
            t = threading.Thread(target=_loop.run_forever, daemon=True, name="web-parser-loop")
            t.start()
        return _loop
```
```python
def parse_into_text(self, content: bytes) -> Document:
    url = endecode.decode_bytes(content)
    logger.info(f"Scraping web page: {url}")
    # CHANGED: asyncio.run() replaced with run_coroutine_threadsafe
    fut = asyncio.run_coroutine_threadsafe(self.scrape(url), _get_loop())
    chtml = fut.result(timeout=60)   # CHANGED: explicit timeout
    # ... unchanged
```
🛡️ **No-regression check:** With multiple parallel calls, all coroutines are multiplexed on the same loop (standard asyncio behaviour). `fut.result(timeout=60)` adds protection against hangs.

---

### 🔴 Problem 8: Mutable default arguments in `_try_find_executable_path`
🔍 **Root cause:** Python evaluates default arguments once at `def` time; an `[]` default is shared across all calls and indirectly mutated through `paths.extend(possible_path)`.
✅ **Fix strategy:** `None` as default, materialize the list inside the function — standard idiom, no behaviour change at call sites.
💻 **Code change** — `docreader/parser/doc_parser.py:231-249`
```python
def _try_find_executable_path(
    self,
    executable_name: str,
    possible_path: Optional[List[str]] = None,        # CHANGED
    environment_variable: Optional[List[str]] = None, # CHANGED
) -> Optional[str]:
    """Find executable path …"""
    possible_path = possible_path or []           # NEW
    environment_variable = environment_variable or []  # NEW
    paths: List[str] = []
    paths.extend(possible_path)
    paths.extend(os.environ.get(env_var, "") for env_var in environment_variable)
    paths = list(set(paths))
    # ... rest unchanged
```
🛡️ **No-regression check:** All call sites that explicitly pass `possible_path=[…]` work unchanged. Calls without arguments now get fresh lists per call (desired behaviour).

---

### 🔴 Problem 9: No `resources.limits` on any compose service
🔍 **Root cause:** Without hard memory/CPU limits, a single container can OOM-kill the host kernel.
✅ **Fix strategy:** Conservative default limits via `deploy.resources` block. Compose only honours these with `docker compose --compatibility` or Swarm; in plain compose-up, the top-level `mem_limit`/`cpus` fields take effect. Set both so both paths are covered.
💻 **Code change** — Example `docker-compose.yml:30-50` (app), apply pattern to all services:
```yaml
  app:
    image: wechatopenai/weknora-app:${WEKNORA_VERSION:-latest}
    container_name: WeKnora-app
    # ... existing fields
    mem_limit: 4g                # NEW
    cpus: 2.0                    # NEW
    deploy:                       # NEW
      resources:                  # NEW
        limits:                   # NEW
          memory: 4g              # NEW
          cpus: "2.0"             # NEW
```
Recommended limits (starting points, tune to workload):
- `app`, `docreader`: 4 GiB / 2 CPU
- `postgres`: 2 GiB / 1 CPU
- `redis`: 512 MiB / 0.5 CPU
- `qdrant`/`weaviate`/`milvus`: 4 GiB / 2 CPU
- `neo4j`: 2 GiB / 1 CPU
- `minio`: 1 GiB / 0.5 CPU
- `langfuse-*`: 1 GiB / 1 CPU
- `clickhouse`: 4 GiB / 2 CPU
- `frontend` (nginx): 256 MiB / 0.25 CPU

🛡️ **No-regression check:** Verify values in a load-test environment; a too-tight limit produces OOM restarts instead of host crashes — desired behaviour.

---

### 🔴 Problem 10: SQLite table names via `fmt.Sprintf` in queries
🔍 **Root cause:** `fmt.Sprintf` interpolates the table directly into the SQL string; while the value internally comes from `vecTableName(dim)`, this pattern sets a precedent for later user inputs.
✅ **Fix strategy:** Continue using `fmt.Sprintf` for table names (SQLite does not support parameter binding for DDL names), but filter through an allowlist. *No* user input is possible, even if `dim` were ever sourced externally.
💻 **Code change** — `internal/application/repository/retriever/sqlite/repository.go` (new helper + call sites)
```go
// NEW (module-private helper at top of file)
var allowedVecDims = map[int]bool{384: true, 512: true, 768: true, 1024: true, 1536: true, 2048: true, 3072: true, 4096: true}

func safeVecTableName(dim int) (string, error) { // NEW
    if !allowedVecDims[dim] {                    // NEW
        return "", fmt.Errorf("vec dim %d not allowlisted", dim) // NEW
    }                                            // NEW
    return vecTableName(dim), nil                // NEW
}                                                // NEW
```
```go
// CHANGED: 503, 520, 533 — call helper instead of vecTableName directly
tbl, err := safeVecTableName(dim) // NEW
if err != nil {                   // NEW
    return                        // NEW
}                                 // NEW
sql := fmt.Sprintf("INSERT INTO %s(rowid, embedding) VALUES (?, ?)", tbl) // CHANGED
r.db.Exec(sql, rowID, blob)
```
🛡️ **No-regression check:** Allowlist covers all dimensions used by current embedding models. New models have to be entered explicitly — intentional friction.

---

## HIGH FIXES

### 🔴 Problem 11: `_ = c.ShouldBindJSON(&req)` in `tag.go`
🔍 **Root cause:** Bind error is ignored; a body with the wrong type silently lands in downstream code with zero values.
✅ **Fix strategy:** Treat bind errors only as hard fail when the body is non-empty (endpoint accepts body optionally) — exact business logic preserved.
💻 **Code change** — `internal/handler/tag.go:303-306`
```go
var req DeleteTagRequest
if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF { // CHANGED
    c.Error(errors.NewBadRequestError("invalid request body"))   // NEW
    return                                                       // NEW
}                                                                // CHANGED
```
🛡️ **No-regression check:** Callers without a body still send `EOF` — that is ignored, exactly the current behaviour. Callers with garbage bodies now get 400 instead of phantom success.

---

### 🔴 Problem 12: `math/rand` (global) in `RandomSelect` and standard imports
🔍 **Root cause:** Global `math/rand` is deterministic without explicit seeding, weak for selection logic in HTTP handlers.
✅ **Fix strategy:** Switch to `math/rand/v2` — no API breakage for `RandomSelect` callers.
💻 **Code change** — `internal/handler/initialization.go:8, 2372`
```go
import (
    // ... unchanged
    rand "math/rand/v2"  // CHANGED
    // ...
)
```
```go
func (h *InitializationHandler) FabriTag(c *gin.Context) {
    n := rand.IntN(len(tagOptions)-1) + 1   // CHANGED: rand.IntN instead of rand.Intn
    tagRandom := RandomSelect(tagOptions, n)
    // ... unchanged
}
```
🛡️ **No-regression check:** `math/rand/v2` is stable since Go 1.22; the module declares `go 1.24.11`. Results stay pseudo-random but cryptographically better seeded.

---

### 🔴 Problem 13: Reranker `http.Client{}` without timeout
🔍 **Root cause:** A default `&http.Client{}` has `Timeout: 0` (infinite) — a hung upstream connection blocks a goroutine until the OS connection reset.
✅ **Fix strategy:** Set a shared default timeout (90s, reranker calls are short). Four sites, identical pattern.
💻 **Code change** — `internal/models/rerank/zhipu_reranker.go:74` (analogous to `aliyun_reranker.go:90`, `jina_reranker.go:63`, `remote_api.go:65`)
```go
return &ZhipuReranker{
    modelName: config.ModelName,
    modelID:   config.ModelID,
    apiKey:    apiKey,
    baseURL:   baseURL,
    client:    &http.Client{Timeout: 90 * time.Second}, // CHANGED
}, nil
```
🛡️ **No-regression check:** 90s is generous for reranking. Make tunable via config if needed — follow-up ticket.

---

### 🔴 Problem 14: WeChat file download without SSRF validation
🔍 **Root cause:** `msg.FileKey` is a server-supplied URL (or one in a manipulated webhook payload) — fetched directly without scheme/IP check.
✅ **Fix strategy:** Use the existing `utils.NewSSRFSafeHTTPClient` instead of the module `ilinkHTTPClient`. The pattern already exists in `mineru_cloud_converter.go:172`.
💻 **Code change** — `internal/im/wechat/adapter.go:155-163`
```go
// CHANGED: SSRF validation of the URL
if err := utils.ValidateURLForSSRF(msg.FileKey); err != nil { // NEW
    return nil, "", fmt.Errorf("reject unsafe file URL: %w", err) // NEW
}                                                                  // NEW

req, err := http.NewRequestWithContext(ctx, http.MethodGet, msg.FileKey, nil)
if err != nil {
    return nil, "", fmt.Errorf("create download request: %w", err)
}

client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{ // CHANGED
    Timeout:      120 * time.Second,                                  // CHANGED
    MaxRedirects: 5,                                                  // CHANGED
})                                                                    // CHANGED
resp, err := client.Do(req)                                           // CHANGED (was: ilinkHTTPClient)
```
🛡️ **No-regression check:** WeChat CDN domains usually live in public address space — the SSRF allowlist in `utils.IsSSRFWhitelisted` may need extension with the WeChat domain if private/special routes are required.

---

### 🔴 Problem 15: 13× ignored `saveKBCloneProgress` errors
🔍 **Root cause:** `_ = s.saveKBCloneProgress(...)` silently discards persistence errors; a DB glitch leaves the clone job running with no visible status.
✅ **Fix strategy:** Switch to a unified `logger.Errorf(...)` error path — no operation abort, but visibility.
💻 **Code change** — `internal/application/service/knowledge_clone_move.go:260, 310, 342, 368, 413, 430, 440, 484, 579, 743, 758, 797, 810`
```go
// All 13 sites — pattern identical:
if err := s.saveKBCloneProgress(ctx, progress); err != nil {       // CHANGED
    logger.Errorf(ctx, "Failed to persist KB clone progress: %v", err) // NEW
}
```
🛡️ **No-regression check:** The function is called frequently in fire-and-forget paths (e.g. `handleError` lines 254-261). The added log line does not cause any function to fail.

---

### 🔴 Problem 16: `actions/checkout@v3` (deprecated)
🔍 **Root cause:** GitHub Actions v3 receives no further patches; v4 is drop-in.
✅ **Fix strategy:** Pattern replace in the workflow.
💻 **Code change** — `.github/workflows/docker-image.yml:18, 67, 100, 143`
```yaml
- uses: actions/checkout@v4   # CHANGED (all 4 locations)
```
🛡️ **No-regression check:** v4 requires Node 20 runner — `ubuntu-latest` already provides it.

---

### 🔴 Problem 17: `:latest` image tags
🔍 **Root cause:** `:latest` is non-reproducible — an upstream major bump can break compose-up.
✅ **Fix strategy:** Pin specific versions. Suggesting only default values here; keep `WEKNORA_VERSION` from env.
💻 **Code change** — `docker-compose.yml:3, 29, 144, 174, 290, 366` (analogous `docker-compose.dev.yml:39, 106, 165, 190`)
```yaml
# CHANGED — example, verify versions before commit
image: wechatopenai/weknora-ui:${WEKNORA_VERSION:-v0.5.1}
image: wechatopenai/weknora-app:${WEKNORA_VERSION:-v0.5.1}
image: wechatopenai/weknora-docreader:${WEKNORA_VERSION:-v0.5.1}
image: dexidp/dex:v2.41.1
image: neo4j:5.24-community
image: jaegertracing/all-in-one:1.76.0
```
🛡️ **No-regression check:** All tags must really exist — `docker pull <image>:<tag>` test before merge.

---

### 🔴 Problem 18: `migrate.sh` logs DB password in plaintext
🔍 **Root cause:** `echo "DB_PASSWORD: ${DB_PASSWORD}"` runs on every migration and lands in CI logs / container logs.
✅ **Fix strategy:** Remove the line; remaining logs unchanged (DB URL without password as hint suffices).
💻 **Code change** — `scripts/migrate.sh:67-69`
```bash
# CHANGED
echo "DB_USER: ${DB_USER}"
# REMOVE: echo "DB_PASSWORD: ${DB_PASSWORD}"
echo "DB_HOST: ${DB_HOST}"
```
Additionally sanitize the `DB_URL` echo:
```bash
# NEW before "echo DB_URL"
SANITIZED_URL="${DB_URL//${DB_PASSWORD}/***}"
echo "DB_URL: ${SANITIZED_URL}"
```
🛡️ **No-regression check:** Migration call still uses `${DB_URL}` — only the *echo* is redacted.

---

### 🔴 Problem 19: 13 Vue `{ deep: true }` watchers
🔍 **Root cause:** `deep: true` traverses the whole object tree on every re-set — O(n) per change on large lists.
✅ **Fix strategy:** Where possible switch to a surrogate (`computed` with `JSON.stringify` hash or selector). Minimal: for lists use `() => list.length` as the watcher source. Per-file individual patch — representative example for `views/chat/index.vue:213-218`:
💻 **Code change**
```ts
// CHANGED: messagesList → messagesList.length + selector
watch(
  () => messagesList.value.length,                        // CHANGED
  () => { /* current logic unchanged */ },
)
```
🛡️ **No-regression check:** Changes trigger frequency: watcher fires only on length change, not on content mutation. If content mutation must be observed, watch the specific field, not the whole tree. Per-site review required.

---

### 🔴 Problem 20: `context.Background()` in 13 places (queue, service startup, goroutines)
🔍 **Root cause:** Detached `context.Background()` ignores shutdown signal and request deadline — goroutines outlive server stop.
✅ **Fix strategy:** Where parent ctx is reachable: thread it through. Where not: inject a long-lived `serviceCtx` from the container that cancels on `os.Signal`.

💻 **Code change 1** — `internal/handler/initialization.go:1069-1073`
```go
// CHANGED: take parent ctx from handler, but timeout beyond the handler's lifetime
parentCtx := h.shutdownCtx                                             // NEW (field, set in constructor)
newCtx, cancel := context.WithTimeout(parentCtx, 12*time.Hour)         // CHANGED
go func() {
    defer cancel()
    h.downloadModelAsync(newCtx, taskID, req.ModelName)
}()
```
Constructor change in `cmd/server/main.go` etc. — pass `shutdownCtx` from `signal.NotifyContext(...)` through to handlers.

💻 **Code change 2** — `internal/handler/session/agent_stream_handler.go:391-405`
```go
// CHANGED: no own bgCtx; use parent ctx (with short extension)
ctxAppend, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second) // NEW
defer cancel()                                                                       // NEW
if err := h.streamManager.AppendEvent(ctxAppend, h.sessionID, h.assistantMessageID, // CHANGED
    interfaces.StreamEvent{ /* unchanged */ }); err != nil {
    logger.GetLogger(h.ctx).Warn("…", "error", err)
}
```
*(`context.WithoutCancel` strips cancel propagation but keeps values like TenantID — Go 1.21+.)*

💻 **Code change 3** — `internal/application/service/session.go:507-520`
```go
go func() {
    bgCtx := context.WithoutCancel(ctx) // CHANGED: keep values, strip cancel
    // tenantID/requestID/language/langfuseTrace propagation drops out — context.WithoutCancel preserves them
    // … remaining logic unchanged
}()
```
🛡️ **No-regression check:** `context.WithoutCancel` is Go 1.21+ — `go.mod` declares 1.24, so fine. Values continue to propagate; only cancel signals don't.

---

## MEDIUM / OTHER FIXES (compact)

### 🔴 Problem 21: `json.Marshal` without error check in Feishu (`adapter.go:735, 791, 796, 837, 873, 876, 918`)
🔍 **Root cause:** `payload, _ := json.Marshal(map[...])` ignores marshalling errors that can realistically occur with `map[string]interface{}` containing foreign values.
✅ **Fix strategy:** Replace `_` with a real variable + early return.
💻 **Code change** — example line 735:
```go
payload, err := json.Marshal(map[string]interface{}{ // CHANGED
    "type": "card_json",
    "data": cardJSON,
})
if err != nil {                                       // NEW
    return "", fmt.Errorf("marshal card payload: %w", err) // NEW
}                                                     // NEW
```
🛡️ Apply the same pattern to all 9 occurrences.

---

### 🔴 Problem 22: Slack `json.Unmarshal` error ignored (`adapter.go:143`)
🔍 **Root cause:** If the body is not JSON, `rawEvent.Event.Files` stays `nil` — no symptom, but silent data loss.
✅ **Fix strategy:** Log the error but let the adapter continue (files are optional).
💻 **Code change**
```go
if err := json.Unmarshal(bodyBytes, &rawEvent); err != nil {                    // CHANGED
    logger.GetLogger(c.Request.Context()).Warn("slack: parse files failed", "error", err) // NEW
}
```

---

### 🔴 Problem 23: Ollama tool param unmarshal ignored (`models/chat/ollama.go:193, 200`)
🔍 **Root cause:** Silent data loss on tool calls that are then executed with an empty `Parameters` map.
✅ **Fix strategy:** Log the error and skip the affected tool call instead of running with empty params.
💻 **Code change** (pseudo pattern):
```go
if err := json.Unmarshal(tool.Function.Parameters, &function.Parameters); err != nil { // CHANGED
    logger.Warnf(ctx, "skip tool with bad params: %v", err)                            // NEW
    continue                                                                           // NEW
}
```

---

### 🔴 Problem 24: `resp.Body.Close()` without `defer` in MinerU converters
🔍 **Root cause:** On error between `resp` and explicit `Close()` the body reader leaks.
✅ **Fix strategy:** Switch to `defer` — no logic change.
💻 **Code change** — `internal/infrastructure/docparser/mineru_cloud_converter.go:177`, `mineru_converter.go:263`
```go
resp, err := client.Do(httpReq)
if err != nil {
    return fmt.Errorf("PUT upload: %w", err)
}
defer resp.Body.Close()           // CHANGED (was direct resp.Body.Close())
```

---

### 🔴 Problem 25: `defer file.Close()` without error check (`initialization.go:2038`)
🔍 **Root cause:** On upload streams, a swallowed close error masks an incomplete write flush.
✅ **Fix strategy:** Use a wrapper function that logs the result — no logic change.
💻 **Code change**
```go
defer func() {                                                              // CHANGED
    if cerr := file.Close(); cerr != nil {                                  // NEW
        logger.Warnf(ctx, "close uploaded file: %v", cerr)                  // NEW
    }                                                                       // NEW
}()                                                                         // CHANGED
```

---

### 🔴 Problem 26: `asyncio` / DNS lookup with `context.Background()`
🔍 **Root cause:** `net.DefaultResolver.LookupIP(context.Background(), …)` ignores caller timeout.
✅ **Fix strategy:** Thread the caller's ctx through (already exists in the surrounding function).
💻 **Code change** — `internal/agent/tools/web_fetch.go:286`, `internal/infrastructure/web_fetch/fetcher.go:53`
```go
ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname) // CHANGED
```

---

### 🔴 Problem 27: `v-html` without consistent sanitization (frontend)
🔍 **Root cause:** Several `v-html` sites (`AgentStreamDisplay.vue:329, 365`; `GlobalCommandPalette.vue:127, 154`) rely on the caller having already left the content "safe".
✅ **Fix strategy:** Enforce a single `safeHtml(...)` helper that calls `dompurify.sanitize` with a conservative profile. All `v-html` bindings only via this helper. No refactor of surrounding components.
💻 **Code change** — `frontend/src/utils/security.ts` (helper already exists, but not used everywhere):
```ts
// In each remaining component:
import { safeHtml } from '@/utils/security'  // NEW
```
```vue
<!-- CHANGED in the four affected templates -->
<div v-html="safeHtml(floatPopup.content)" />
<div v-html="safeHtml(wikiDrawerContent)" />
<div v-html="safeHtml(highlight(item.label, query))" />
```
🛡️ With `highlight(...)` additionally verify that the HTML construction only inserts `<mark>` tags — set the DOMPurify profile whitelist accordingly.

---

### 🔴 Problem 28: SSE/stream not aborted on route change
🔍 **Root cause:** The `AbortController` is only invoked in `stopStream()` & `onUnmounted` — a route change without composable unmount lets the stream continue.
✅ **Fix strategy:** Add an `onBeforeRouteLeave` hook in the consuming view.
💻 **Code change** — `frontend/src/views/chat/index.vue` (placed appropriately in the Composition API section)
```ts
import { onBeforeRouteLeave } from 'vue-router' // NEW

onBeforeRouteLeave(() => {                       // NEW
  stopStream?.()                                  // NEW
})                                                // NEW
```

---

### 🔴 Problem 29: WeChat polling timer not guaranteed cleared
🔍 **Root cause:** `wechatPollTimer = setTimeout(pollOnce, 500)` re-arms recursively; if `stopWeChatPolling()` is called mid-`pollOnce()`, an in-flight promise has already queued the next `setTimeout`.
✅ **Fix strategy:** Force the `wechatPollActive` check before `setTimeout` — current code already does that (line 574). Additionally call `stopWeChatPolling` safely in the finally block of pollOnce, in case the backend reports a final status.
💻 **Code change** — `frontend/src/components/IMChannelPanel.vue:573-577`
```ts
} catch {
  // transient error
} finally {                                                  // NEW
  if (!wechatPollActive && wechatPollTimer) {                // NEW
    clearTimeout(wechatPollTimer)                            // NEW
    wechatPollTimer = null                                   // NEW
  }                                                          // NEW
}
if (wechatPollActive) {
  wechatPollTimer = setTimeout(pollOnce, 500)
}
```

---

### 🔴 Problem 30: `subprocess.run(["which", …])` without `timeout=`
🔍 **Root cause:** `which` with a corrupted `PATH` can hang.
✅ **Fix strategy:** Set a constant 5s timeout.
💻 **Code change** — `docreader/parser/doc_parser.py:258-260`
```python
result = subprocess.run(
    ["which", executable_name],
    capture_output=True, text=True,
    timeout=5,  # NEW
)
```

---

### 🔴 Problem 31: `print()` in library code (DocReader)
🔍 **Root cause:** Library output ends up on stdout, the structured logger never sees it.
✅ **Fix strategy:** 1:1 replace `print(x)` → `logger.info(x)`. Apply the pattern to all occurrences (`chain_parser.py:179`, `markdown_parser.py:124`, `web_parser.py:149-162`, `excel_parser.py:114-118`).
💻 **Code change** (example)
```python
# CHANGED
logger.info(format_content)
```

---

### 🔴 Problem 32: `chain_parser.py:62` — bare `except Exception:`
🔍 **Root cause:** Catches too broadly (incl. programming errors), hides diagnostics.
✅ **Fix strategy:** Narrow to `(IOError, ValueError, RuntimeError)` — known parser error classes. `KeyboardInterrupt`/`SystemExit` continue to propagate.
💻 **Code change**
```python
try:
    document = p.parse_into_text(content)
except (IOError, ValueError, RuntimeError) as e:    # CHANGED
    logger.exception(
        "FirstParser: parser %s failed: %s; trying next",
        p.__class__.__name__, e,
    )
    continue
```

---

### 🔴 Problem 33: `target="_blank"` without `rel="noopener noreferrer"`
🔍 **Root cause:** Without `rel`, the target page can manipulate via `window.opener`.
✅ **Fix strategy:** Add the `rel` attribute — pure template change.
💻 **Code change** — `frontend/src/views/auth/Login.vue:96, 102, 111`, `frontend/src/views/chat/components/docInfo.vue:17`, `frontend/src/views/settings/StorageEngineSettings.vue:253-362`
```html
<a href="…" target="_blank" rel="noopener noreferrer">…</a> <!-- CHANGED -->
```

---

### 🔴 Problem 34: `.catch(() => {})` (6×)
🔍 **Root cause:** Errors are swallowed without log/UI.
✅ **Fix strategy:** `console.warn` as minimum; for critical operations (`saveConfig`) additionally a toast.
💻 **Code change** — `frontend/src/components/Input-field.vue:549` (analog 5 other sites)
```ts
orgStore.fetchSharedKnowledgeBases()
  .catch((err) => console.warn('[fetchSharedKnowledgeBases] failed:', err)) // CHANGED
```

---

### 🔴 Problem 35: `set -euo pipefail` missing in shell scripts
🔍 **Root cause:** Unset vars become `""`, broken pipes fail silently.
✅ **Fix strategy:** Insert directly after the shebang.
💻 **Code change** — `scripts/dev.sh:2`, `scripts/build_images.sh:2`
```bash
#!/bin/bash
set -euo pipefail   # NEW
```
🛡️ **No-regression check:** Existing optional vars (`${VAR:-default}`) are still fine; unset ones without fallback now break early — desired.

---

### 🔴 Problem 36: Old Python typings (`List`, `Dict`, `Optional`)
🔍 **Root cause:** PEP 585/604 is the standard since 3.10; `from typing import List, Dict, Optional` causes no bugs but is bit-rot ballast.
✅ **Fix strategy:** Search-and-replace per file (`List[X]` → `list[X]`, `Optional[X]` → `X | None`). Shorten imports accordingly. No behaviour change.
💻 **Code change** — example `docreader/parser/doc_parser.py:1-15, 234-236`
```python
# CHANGED: imports
from typing import Any  # NEW (if still used)
# REMOVE: from typing import List, Optional, Dict
```
```python
def _try_find_executable_path(
    self,
    executable_name: str,
    possible_path: list[str] | None = None,        # CHANGED
    environment_variable: list[str] | None = None, # CHANGED
) -> str | None:                                   # CHANGED
```

---

### 🔴 Problem 37: Healthchecks missing for Redis/Qdrant/Weaviate
🔍 **Root cause:** Without healthchecks, `depends_on: service_started` triggers a startup race; `app` starts too early.
✅ **Fix strategy:** Add per-service healthcheck + switch `app.depends_on` to `service_healthy`.
💻 **Code change** — `docker-compose.yml`
```yaml
  redis:
    # ... existing
    healthcheck:                                   # NEW
      test: ["CMD", "redis-cli", "ping"]           # NEW
      interval: 10s                                # NEW
      timeout: 5s                                  # NEW
      retries: 5                                   # NEW

  qdrant:
    # ... existing
    healthcheck:                                   # NEW
      test: ["CMD-SHELL", "wget -qO- http://localhost:6333/healthz || exit 1"] # NEW
      interval: 10s                                # NEW
      timeout: 5s                                  # NEW
      retries: 5                                   # NEW
      start_period: 20s                            # NEW

  weaviate:
    # ... existing
    healthcheck:                                   # NEW
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/v1/.well-known/ready || exit 1"] # NEW
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s
```
```yaml
  app:
    depends_on:
      redis:
        condition: service_healthy   # CHANGED
      postgres:
        condition: service_healthy
```

---

### 🔴 Problem 38: `.env.example` ↔ `.env.lite.example` schema drift
🔍 **Root cause:** Keys diverge (`DB_HOST` vs `DB_PATH`); copy-use leads to undefined variables.
✅ **Fix strategy:** A commented header in both files referencing the other variant; common keys aligned to identical names.
💻 **Code change** — `.env.example:1` and `.env.lite.example:1`
```bash
# WeKnora environment — full version (Postgres + all services).
# For SQLite-Lite mode: see .env.lite.example. Key schemas are NOT identical.
# Required vars: JWT_SECRET, DB_*, MINIO_*, NEO4J_PASSWORD, LANGFUSE_*  # NEW
```

---

### 🔴 Problem 39: Containers possibly running as root
🔍 **Root cause:** Multiple Dockerfiles have no `USER` at the end; a process compromise grants root in the container.
✅ **Fix strategy:** Set `USER appuser` in the final stage. Pattern already exists in `Dockerfile.app` as a template — extend to `Dockerfile.docreader`/`frontend/Dockerfile`.
💻 **Code change**
```dockerfile
# At the end of the final stage:
USER appuser   # NEW
```

---

### 🔴 Problem 40: Race in `fetchSuggestedQuestions`
🔍 **Root cause:** The `setTimeout` debounce does *not* prevent multiple requests, only delays them; an interim route switch can deliver to the wrong session.
✅ **Fix strategy:** Add an `AbortController` that cancels the previous call on each new invocation.
💻 **Code change** — `frontend/src/views/chat/index.vue:159-189`
```ts
let suggestionsAbort: AbortController | null = null  // NEW

const fetchSuggestedQuestions = async () => {
  suggestionsAbort?.abort()                          // NEW
  suggestionsAbort = new AbortController()           // NEW
  const fetchId = ++suggestedQuestionsFetchId
  // ...
  try {
    const res = await getSuggestedQuestions(agentId, {
      knowledge_base_ids: …,
      knowledge_ids: …,
      limit: 6,
      signal: suggestionsAbort.signal,               // NEW
    })
    // ...
  } catch (err) {
    if (err.name === 'AbortError') return            // NEW
    // ... existing catch path
  }
}
```
🛡️ **No-regression check:** `getSuggestedQuestions` must forward `signal` to axios — small API extension, no breaking change.

---

## SUMMARY

- **Critical findings addressed (1–10):** CORS, Slack bypass, compose defaults, encryption key, curl-pipe, JWT storage, asyncio.run, mutable defaults, resource limits, SQL templates.
- **High findings addressed (11–20):** ShouldBindJSON, math/rand, reranker timeout, WeChat SSRF, KB clone logging, GitHub Action pin, image pin, migrate.sh logging, deep watcher, context.Background.
- **Medium / remaining (21–40):** Marshal/unmarshal errors, Body.Close defers, file.Close logging, DNS ctx, v-html sanitization, stream abort on route leave, polling timer, subprocess timeout, print → logger, except refactor, rel attributes, .catch logging, set-euo, PEP585 typings, healthchecks, env drift, USER directive, AbortController.

**Remaining for separate tickets** (too large for minimal-invasive patches):
- HTTP-only cookies for JWT (backend + frontend roundtrip).
- Unified IM webhook verification behind a common interface.
- `defusedxml` in DocReader.
- `setupTLS` with `mTLS` between app and DocReader.
- Distroless migration of runtime images.

*Created 2026-05-06. Patches are proposals — verify per file manually and write tests before merge.*

---

## ADDITIONAL CRITICAL FIXES (Follow-up audit)

### 🔴 Problem 41: AES-128-ECB for WeChat media files
🔍 **Root cause:** ECB encrypts every 16-byte block independently with the same key, leaking patterns; PKCS#7 unpadding additionally exits early on byte mismatch (padding-oracle vector).
✅ **Fix strategy:** Replace with AES-GCM (AEAD, random IV per blob) where the WeKnora side controls both encrypt and decrypt. If the iLink wire-protocol *forces* ECB on egress, isolate that one direction and switch the local cache layer to GCM. Constant-time padding check in either case.
💻 **Code change** — `internal/im/wechat/crypto.go` (replace both functions)
```go
// CHANGED: AES-GCM replaces AES-128-ECB
import (
    "crypto/aes"
    "crypto/cipher"   // NEW
    "crypto/rand"     // NEW
    "crypto/subtle"   // NEW (constant-time padding check, fallback path only)
    "fmt"
    "io"              // NEW
)

func encryptAESGCM(plaintext, key []byte) ([]byte, error) { // CHANGED
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new aes cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)                         // NEW
    if err != nil {                                          // NEW
        return nil, err                                      // NEW
    }                                                        // NEW
    nonce := make([]byte, gcm.NonceSize())                   // NEW
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil { // NEW
        return nil, err                                      // NEW
    }                                                        // NEW
    return gcm.Seal(nonce, nonce, plaintext, nil), nil       // NEW
}

func decryptAESGCM(ciphertext, key []byte) ([]byte, error) { // CHANGED
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("new aes cipher: %w", err)
    }
    gcm, err := cipher.NewGCM(block)                         // NEW
    if err != nil {                                          // NEW
        return nil, err                                      // NEW
    }                                                        // NEW
    if len(ciphertext) < gcm.NonceSize() {                   // NEW
        return nil, fmt.Errorf("ciphertext too short")       // NEW
    }                                                        // NEW
    return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil) // NEW
}
```
**If iLink mandates ECB on egress** — keep ECB only as `decryptIlinkLegacy`, fix the unpadding to constant-time:
```go
// CHANGED: replace the if-break loop with subtle.ConstantTimeCompare
expected := bytes.Repeat([]byte{byte(padLen)}, padLen)                                 // NEW
if subtle.ConstantTimeCompare(plaintext[len(plaintext)-padLen:], expected) == 1 {     // NEW
    plaintext = plaintext[:len(plaintext)-padLen]                                     // NEW
}                                                                                     // NEW
```
🛡️ **No-regression check:** GCM ciphertext is non-compatible with ECB; this needs coordinated rollout (re-encrypt at rest or version-prefix). Adapter call sites at `internal/im/wechat/adapter.go:155-200` need the `Open(...)` signature shift. If the iLink CDN strictly returns ECB, only the *outbound* path (uploads) can move to GCM unilaterally.

---

### 🔴 Problem 42: OIDC nonce never validated
🔍 **Root cause:** `decodeOIDCState` only parses base64+JSON and checks `RedirectURI` non-empty — the `Nonce` field is parsed but never compared against any server-side store.
✅ **Fix strategy:** Bind the nonce to the user's browser session via an HTTP-only, SameSite=Strict cookie set in `/oidc/start`. Compare in `OIDCRedirectCallback` and reject on mismatch. No refactor of the user-service login flow.
💻 **Code change 1** — start endpoint (in same file as `OIDCRedirectCallback`, where the state is *issued*; if not present, add to `auth.go`):
```go
// At the OIDC start endpoint:
nonce := generateRandomNonce()                                           // NEW (helper using crypto/rand)
c.SetCookie("oidc_nonce", nonce, 600 /*10 min*/, "/", "", true, true)    // NEW (Secure, HttpOnly)
c.SetSameSite(http.SameSiteStrictMode)                                   // NEW
state := encodeOIDCState(&oidcStatePayload{Nonce: nonce, RedirectURI: redirectURI}) // existing call
```
💻 **Code change 2** — `internal/handler/auth.go:243-257`
```go
state := strings.TrimSpace(c.Query("state"))
decodedState, err := decodeOIDCState(state)
if err != nil {
    logger.Errorf(ctx, "Failed to decode OIDC state: %v", err)
    c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("invalid_state"))
    return
}

// NEW: nonce must match the cookie issued at /oidc/start
cookieNonce, _ := c.Cookie("oidc_nonce")                                   // NEW
if cookieNonce == "" || subtle.ConstantTimeCompare([]byte(cookieNonce),   // NEW
    []byte(decodedState.Nonce)) != 1 {                                     // NEW
    logger.Errorf(ctx, "OIDC nonce mismatch")                              // NEW
    c.Redirect(http.StatusFound, frontendRedirectURI+"#oidc_error="+urlQueryEscape("nonce_mismatch")) // NEW
    return                                                                 // NEW
}                                                                          // NEW
c.SetCookie("oidc_nonce", "", -1, "/", "", true, true)                     // NEW (clear cookie)
```
🛡️ **No-regression check:** Existing in-flight OIDC flows (state issued before deploy) will fail once — acceptable security trade-off. Add `crypto/subtle` import; the helper `generateRandomNonce` should call `crypto/rand`, NOT `math/rand` (see Problem 45).

---

### 🔴 Problem 43: `TENANT_AES_KEY` panics on misconfiguration
🔍 **Root cause:** `apiKeySecret()` returns raw env bytes; `aes.NewCipher` panics on lengths other than 16/24/32, killing the request goroutine.
✅ **Fix strategy:** Validate length at startup, fail fast with a clear error message. Mirror the pattern from `utils.GetAESKey()` (`internal/utils/crypto.go:19-25`) which returns `nil` for invalid lengths.
💻 **Code change** — `internal/application/service/tenant.go:23-25`
```go
var apiKeySecret = func() []byte {                                        // CHANGED
    key := []byte(os.Getenv("TENANT_AES_KEY"))                            // CHANGED
    switch len(key) {                                                     // NEW
    case 16, 24, 32:                                                      // NEW
        return key                                                        // NEW
    case 0:                                                               // NEW
        panic("TENANT_AES_KEY environment variable is required (16/24/32 bytes)") // NEW
    default:                                                              // NEW
        panic(fmt.Sprintf("TENANT_AES_KEY length %d invalid (must be 16/24/32 bytes)", len(key))) // NEW
    }                                                                     // NEW
}                                                                         // CHANGED
```
**Plus** — call this once at startup so the panic is at boot, not at first request:
```go
// In cmd/server/main.go, near other config validation:
_ = service.MustValidateTenantAESKey()   // NEW (small wrapper that calls apiKeySecret() during init)
```
🛡️ **No-regression check:** Servers that booted with an invalid key were already hitting the panic on first user-create — this just moves the failure forward in time. Operator gets a clear error message instead of a confusing stack trace at a random later moment.

---

### 🔴 Problem 44: Init migrations `DROP TABLE` user data unconditionally
🔍 **Root cause:** The `00-init-db.sql` (MySQL) and `000000_init` family begin with `DROP TABLE IF EXISTS …` for seven user-data tables; running the init step on a populated DB wipes everything irreversibly.
✅ **Fix strategy:** Remove the `DROP TABLE` lines from the up-migration; rely on `CREATE TABLE IF NOT EXISTS` (already implicit for the up-path). Move the destructive cleanup into a separate, opt-in `reset/`-flagged path.
💻 **Code change 1** — `migrations/mysql/00-init-db.sql:1-7`
```sql
-- REMOVE these 7 lines:
-- DROP TABLE IF EXISTS tenants;
-- DROP TABLE IF EXISTS models;
-- DROP TABLE IF EXISTS knowledge_bases;
-- DROP TABLE IF EXISTS knowledges;
-- DROP TABLE IF EXISTS sessions;
-- DROP TABLE IF EXISTS messages;
-- DROP TABLE IF EXISTS chunks;

CREATE TABLE IF NOT EXISTS tenants (   -- CHANGED: add IF NOT EXISTS
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    -- ... unchanged
);
-- repeat the IF NOT EXISTS guard on each remaining CREATE TABLE
```
💻 **Code change 2** — extract cleanup into `migrations/reset/00-drop-all.sql` (NEW file), gated by an explicit operator command:
```sql
-- NEW FILE: migrations/reset/00-drop-all.sql
-- DANGEROUS: only run via `make db-reset CONFIRM=YES` — never by accident.
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS knowledges;
DROP TABLE IF EXISTS knowledge_bases;
DROP TABLE IF EXISTS models;
DROP TABLE IF EXISTS tenants;
```
And in `Makefile`:
```make
db-reset:                                                                 # NEW
ifndef CONFIRM                                                            # NEW
	@echo "Refusing to drop tables without CONFIRM=YES"                  # NEW
	@exit 1                                                              # NEW
endif                                                                     # NEW
	@./scripts/migrate.sh apply migrations/reset/00-drop-all.sql         # NEW
```
🛡️ **No-regression check:** Fresh installs still work because tables don't exist yet → `CREATE TABLE IF NOT EXISTS` succeeds. Existing databases are now safe from accidental wipe via re-running the init migration. `migrate force 0` is a separate concern — document it as DEV-only.

---

### 🔴 Problem 45: `signer.go` uses `math/rand` for nonce + MD5 for signature
🔍 **Root cause:** Two compounding crypto mistakes — predictable nonces (via `math/rand`) plus collision-vulnerable hash (MD5) — together defeat the integrity guarantee.
✅ **Fix strategy:** Replace nonce source with `crypto/rand` (always safe). For the hash, MD5 is wire-mandated by WeKnoraCloud upstream — keep it but document the limitation. The nonce fix alone restores replay protection.
💻 **Code change** — `internal/models/utils/signer.go:1-12, 71-77`
```go
import (
    "bytes"
    "crypto/md5"            // unchanged (upstream-mandated)
    "crypto/rand"           // NEW
    "encoding/binary"       // NEW
    "fmt"
    // REMOVE: "math/rand"
    "sort"
    "strconv"
    "strings"
    "time"
)

// CHANGED: cryptographically secure nonce
func generateNonce(length int) string {
    b := make([]byte, length)                                              // unchanged
    for i := range b {                                                     // CHANGED
        var idx [1]byte                                                    // NEW
        if _, err := rand.Read(idx[:]); err != nil {                       // NEW
            // crypto/rand on a sane OS never fails; if it does, panic is the only safe option
            panic(fmt.Errorf("crypto/rand failed: %w", err))               // NEW
        }                                                                  // NEW
        b[i] = nonceChars[int(idx[0])%len(nonceChars)]                     // CHANGED
    }
    return string(b)
}
```
*(`encoding/binary` is imported in case future code uses `binary.LittleEndian.Uint32(...)` for index derivation; can be omitted if only the byte-modulo form above is used.)*

🛡️ **No-regression check:** Signature shape and length are unchanged — wire-compatible with the upstream API. `crypto/rand` reading 1 byte per char is fast (~16 calls per signature; modern OS RNGs handle millions/sec). MD5 is left as-is intentionally because WeKnoraCloud's verification side does the same; flagging that limitation as a separate vendor-side ticket.

---

## UPDATED SUMMARY

- **Critical findings now addressed (1–10, 41–45):** original 10 + WeChat ECB, OIDC nonce, TENANT_AES_KEY validation, init-migration drop, signer nonce/MD5.
- **Total resolutions:** 45.
- **Remaining for separate tickets:** as before, plus: WeKnoraCloud upstream MD5 (vendor coordination), iLink CDN ECB protocol (vendor coordination — only WeKnora-side mitigation possible).
