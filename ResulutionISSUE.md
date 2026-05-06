# WeKnora — Resolution Plan

> Reaktion auf `ISSUE.md` vom 2026-05-06.
> Rolle: **Resolution Engineer** — minimal-invasive Chirurgie, keine Refactorings, keine Symptom-Doctoring.
> Status: **Nur Lösungs-Vorschlag.** Patches sind als Diff-Skizzen formuliert; keine Quelldateien dieses Commits werden geändert.
>
> Lesehinweis:
> - `// GEÄNDERT` = Zeile ersetzt
> - `// NEU` = Zeile hinzugefügt
> - `// ENTFERNEN` = Zeile entfällt
>
> Aufbau: erst alle **Critical**, dann alle **High**, dann **Medium** (kürzer). Innerhalb identisch nach Bereichen wie in `ISSUE.md`.

---

## CRITICAL FIXES

### 🔴 Problem 1: CORS akzeptiert `*` zusammen mit `AllowCredentials: true`
🔍 **Ursache:** Die Wildcard-Origin in Kombination mit Credentials ist laut CORS-Spezifikation ungültig; sie signalisiert eine fehlende Allowlist statt einer bewussten Sicherheits­entscheidung.
✅ **Fix-Strategie:** Allowlist aus `ALLOWED_ORIGINS`-Env (Komma-Liste) lesen, Fallback auf leere Liste. `AllowOriginFunc` nutzen, damit Wildcards & Subdomain-Patterns sauber trennbar bleiben. Kein Refactoring der restlichen Middleware-Kette.
💻 **Code-Änderung** — `internal/router/router.go:74-87`
```go
// GEÄNDERT: bezogene Imports oben in der Datei sicherstellen
import (
    "os"      // NEU
    "strings" // NEU
    // ... bisherige Imports unverändert
)

func NewRouter(params RouterParams) *gin.Engine {
    r := gin.New()
    r.ContextWithFallback = true

    // GEÄNDERT: Allowlist statt "*" + AllowCredentials
    allowed := strings.Split(strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")), ",") // NEU
    for i, o := range allowed {                                                    // NEU
        allowed[i] = strings.TrimSpace(o)                                          // NEU
    }                                                                              // NEU
    r.Use(cors.New(cors.Config{
        AllowOriginFunc: func(origin string) bool { // NEU
            for _, o := range allowed {              // NEU
                if o != "" && o == origin {          // NEU
                    return true                      // NEU
                }                                    // NEU
            }                                        // NEU
            return false                             // NEU
        },                                           // NEU
        AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key", "X-Request-ID"},
        ExposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin"},
        AllowCredentials: true,
        MaxAge:           12 * time.Hour,
    }))
    // ...
}
```
🛡️ **No-Regression-Check:** Bei leerem `ALLOWED_ORIGINS` werden alle Browser-Cross-Origin-Requests abgelehnt — Same-Origin-Aufrufe (Vite-Proxy in Dev, nginx-Reverse-Proxy in Prod) sind nicht betroffen. Dokumentations-Hinweis im `.env.example` ist Sprint-2-Aufgabe.

---

### 🔴 Problem 2: Slack-Webhook ohne Signing-Secret = Auth-Bypass
🔍 **Ursache:** Der Early-Return bei leerem `signingSecret` macht aus einer Fehlkonfiguration einen Auth-Bypass statt eines Hard-Fail.
✅ **Fix-Strategie:** Bei leerem Secret Fehler werfen statt `nil`. So fällt der Webhook-Endpoint bei Fehl­konfiguration sicher um, statt unsignierte Requests zu akzeptieren. Kein anderes Verhalten verändert.
💻 **Code-Änderung** — `internal/im/slack/adapter.go:100-103`
```go
func (a *Adapter) VerifyCallback(c *gin.Context) error {
    if a.signingSecret == "" {
        return fmt.Errorf("slack signing secret not configured") // GEÄNDERT: vorher return nil
    }
    // restlicher Body unverändert
}
```
🛡️ **No-Regression-Check:** Adapter-Konstruktoren, die Slack ohne Secret instanziieren, bekommen jetzt eine harte Fehlermeldung beim ersten Webhook-Hit — gewünscht. Tests, die `signingSecret=""` simulieren, müssen aktualisiert werden.

---

### 🔴 Problem 3: Hardcoded Default-Passwörter in `docker-compose.yml`
🔍 **Ursache:** Compose-Var-Defaults (`${VAR:-default}`) machen aus „Variable nicht gesetzt" eine implizite Wahl bekannter Default-Credentials.
✅ **Fix-Strategie:** Default-Werte entfernen, sodass Compose mit klarer Fehlermeldung abbricht („variable is not set"). Anwender wird gezwungen, im `.env` echte Werte zu setzen.
💻 **Code-Änderung** — `docker-compose.yml:128, 237-238, 437-438, 522-523, 525`, `.env.example:211`
```yaml
# GEÄNDERT: docker-compose.yml — Default-Werte entfernt
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
# GEÄNDERT: .env.example:211 — Hinweis statt verwendbarer Default-Wert
JWT_SECRET= # REQUIRED: openssl rand -hex 32
```
🛡️ **No-Regression-Check:** `.env.example` muss synchron eine deutliche Anweisung haben (siehe Problem 5). Bestehende `.env` mit gesetzten Werten sind unberührt.

---

### 🔴 Problem 4: Langfuse `ENCRYPTION_KEY` defaultet auf 64 × `0`
🔍 **Ursache:** Eine all-zeros-Konstante als Default-Schlüssel hat Null-Entropie und ist trivial brute-forcebar.
✅ **Fix-Strategie:** Default entfernen — Compose schlägt mit Fail-Fast fehl. Kein Implementierungs­aufwand, nur Default-Stripping.
💻 **Code-Änderung** — `docker-compose.yml:523`, `docker-compose.dev.yml:333`
```yaml
# GEÄNDERT
ENCRYPTION_KEY: ${LANGFUSE_ENCRYPTION_KEY:?LANGFUSE_ENCRYPTION_KEY must be set (openssl rand -hex 32)}
```
🛡️ **No-Regression-Check:** Bestehende Installationen, die den Default benutzten, sind ohnehin sicherheitstechnisch defekt — sie *müssen* einmalig den Schlüssel rotieren. Dokumentation in CHANGELOG verlinken.

---

### 🔴 Problem 5: `curl … | sh` für `uv`/Ollama im Build und im Setup-Skript
🔍 **Ursache:** Die Pipe entzieht der Distribution jede Manipulationsprüfung — Inhaltliche Änderung beim Upstream landet ungefiltert im Image.
✅ **Fix-Strategie:** Konkreten Release-Tag pinnen + SHA-256-Checksum in Dockerfile/Skript verifizieren. Falls `uv` direkt in `pip install` gewünscht ist, alternativ `pip install --require-hashes`. Hier minimal-invasive Variante mit Checksum.
💻 **Code-Änderung** — `docker/Dockerfile.app:79`
```dockerfile
# GEÄNDERT: Tag-Pin + Checksum-Verifikation
ARG UV_VERSION=0.5.14
ARG UV_INSTALL_SHA256=<echte-sha256-vom-Release-eintragen>
RUN curl -LsSf -o /tmp/uv-install.sh \
        "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-installer.sh" && \
    echo "${UV_INSTALL_SHA256}  /tmp/uv-install.sh" | sha256sum -c - && \
    CARGO_HOME=/home/appuser/.cargo UV_INSTALL_DIR=/home/appuser/.local/bin \
        sh /tmp/uv-install.sh && \
    rm -f /tmp/uv-install.sh
```
💻 **Code-Änderung** — `scripts/start_all.sh:142`
```bash
# GEÄNDERT
OLLAMA_VERSION="0.4.4"   # NEU
OLLAMA_SHA256="<echte-sha256-vom-Release>"  # NEU
curl -fsSL -o /tmp/ollama-install.sh \
    "https://github.com/ollama/ollama/releases/download/v${OLLAMA_VERSION}/ollama-install.sh" # GEÄNDERT
echo "${OLLAMA_SHA256}  /tmp/ollama-install.sh" | sha256sum -c -                              # NEU
sh /tmp/ollama-install.sh                                                                     # NEU
rm -f /tmp/ollama-install.sh                                                                  # NEU
```
🛡️ **No-Regression-Check:** Bei Upstream-Update: Hash-Bumping erforderlich (gewolltes Verhalten — explizite Freigabe). Die `ARG`-Werte können in CI als Build-Args überschrieben werden.

---

### 🔴 Problem 6: JWT/Refresh-Token in `localStorage`
🔍 **Ursache:** `localStorage` ist via XSS lesbar und persistiert Tokens länger als nötig — die Backend-Session sollte als HTTP-only-Cookie gesetzt werden.
✅ **Fix-Strategie:** Migration ist eine größere Backend/Frontend-Koordination. Als minimal-invasiver **Zwischenfix** speichern wir Tokens in `sessionStorage` (geht beim Tab-Schluss verloren, nicht permanent), und ergänzen XSS-Reduktion über `dompurify`-Audit (Problem 27). Vollumzug auf HTTP-only-Cookies wird Sprint-Ticket.
💻 **Code-Änderung** — `frontend/src/stores/auth.ts:75-83, 144-152, 162-167`
```ts
// GEÄNDERT: localStorage → sessionStorage für Auth-Tokens
const setToken = (tokenValue: string) => {
  token.value = tokenValue
  sessionStorage.setItem('weknora_token', tokenValue)        // GEÄNDERT
}

const setRefreshToken = (refreshTokenValue: string) => {
  refreshToken.value = refreshTokenValue
  sessionStorage.setItem('weknora_refresh_token', refreshTokenValue) // GEÄNDERT
}

// in logout():
sessionStorage.removeItem('weknora_token')          // GEÄNDERT
sessionStorage.removeItem('weknora_refresh_token')  // GEÄNDERT

// in initFromStorage():
const storedToken = sessionStorage.getItem('weknora_token')               // GEÄNDERT
const storedRefreshToken = sessionStorage.getItem('weknora_refresh_token') // GEÄNDERT
```
💻 **Code-Änderung** — `frontend/src/utils/request.ts:32, 41, 134, 146-147`
```ts
// GEÄNDERT: alle 4 Stellen
const token = sessionStorage.getItem('weknora_token')               // GEÄNDERT
const refreshToken = sessionStorage.getItem('weknora_refresh_token') // GEÄNDERT
sessionStorage.setItem('weknora_token', newToken)                    // GEÄNDERT
sessionStorage.setItem('weknora_refresh_token', newRefreshToken)     // GEÄNDERT
```
🛡️ **No-Regression-Check:** Nutzer, die heute einen offenen Tab haben, müssen sich nach dem Deploy einmal neu anmelden. Persistenz-Wunsch („remember me") muss als Folge-Ticket über Refresh-Token im HTTP-only-Cookie gelöst werden.

---

### 🔴 Problem 7: `asyncio.run()` aus synchronem Library-Code
🔍 **Ursache:** `asyncio.run()` darf nicht aus einer Funktion aufgerufen werden, die selbst von einem laufenden Event-Loop erreicht wird — sonst `RuntimeError: asyncio.run() cannot be called from a running event loop`.
✅ **Fix-Strategie:** Statt einen neuen Loop pro Aufruf zu erstellen, einen dedizierten Background-Loop in einem eigenen Thread halten und Tasks per `run_coroutine_threadsafe` einreihen. Minimal-invasiv: lokaler Singleton-Helper im Modul, Aufrufer-Signatur unverändert.
💻 **Code-Änderung** — `docreader/parser/web_parser.py:1-15, 95-101`
```python
import asyncio
import threading                    # NEU
# ... bisherige Imports

_loop = None                        # NEU
_loop_lock = threading.Lock()       # NEU

def _get_loop():                    # NEU
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
    # GEÄNDERT: asyncio.run() durch run_coroutine_threadsafe ersetzt
    fut = asyncio.run_coroutine_threadsafe(self.scrape(url), _get_loop())
    chtml = fut.result(timeout=60)   # GEÄNDERT: explizites Timeout
    # ... unverändert
```
🛡️ **No-Regression-Check:** Bei mehreren parallelen Aufrufen werden alle Coroutinen im selben Loop gemultiplext (Standard-asyncio-Verhalten). `fut.result(timeout=60)` schützt zusätzlich vor Hängern.

---

### 🔴 Problem 8: Mutable Default-Argumente in `_try_find_executable_path`
🔍 **Ursache:** Python evaluiert Default-Argumente einmal beim `def`; ein `[]` als Default wird zwischen allen Aufrufen geteilt und durch `paths.extend(possible_path)` indirekt mutiert.
✅ **Fix-Strategie:** `None` als Default, Liste innerhalb der Funktion materialisieren — Standard-Idiom, kein Verhaltens-Wechsel an Aufrufstellen.
💻 **Code-Änderung** — `docreader/parser/doc_parser.py:231-249`
```python
def _try_find_executable_path(
    self,
    executable_name: str,
    possible_path: Optional[List[str]] = None,        # GEÄNDERT
    environment_variable: Optional[List[str]] = None, # GEÄNDERT
) -> Optional[str]:
    """Find executable path …"""
    possible_path = possible_path or []           # NEU
    environment_variable = environment_variable or []  # NEU
    paths: List[str] = []
    paths.extend(possible_path)
    paths.extend(os.environ.get(env_var, "") for env_var in environment_variable)
    paths = list(set(paths))
    # ... Rest unverändert
```
🛡️ **No-Regression-Check:** Alle Call-Sites, die explizit `possible_path=[…]` übergeben, funktionieren unverändert. Aufrufe ohne Argument bekommen jetzt frische Listen pro Call (gewünschtes Verhalten).

---

### 🔴 Problem 9: Keine `resources.limits` in irgendeinem Compose-Service
🔍 **Ursache:** Ohne harte Memory/CPU-Limits kann ein einzelner Container den Host-Kernel in OOM kippen.
✅ **Fix-Strategie:** Konservative Default-Limits über `deploy.resources`-Block. Compose nutzt diese Limits nur mit `docker compose --compatibility` oder Swarm; in reinem Compose-Up wirken die `mem_limit`/`cpus`-Top-Level-Felder. Beide setzen, damit beide Pfade abgesichert sind.
💻 **Code-Änderung** — Beispiel `docker-compose.yml:30-50` (App), Pattern auf alle Services anwenden:
```yaml
  app:
    image: wechatopenai/weknora-app:${WEKNORA_VERSION:-latest}
    container_name: WeKnora-app
    # ... bestehende Felder
    mem_limit: 4g                # NEU
    cpus: 2.0                    # NEU
    deploy:                       # NEU
      resources:                  # NEU
        limits:                   # NEU
          memory: 4g              # NEU
          cpus: "2.0"             # NEU
```
Empfohlene Limits (Startwerte, je nach Workload zu tunen):
- `app`, `docreader`: 4 GiB / 2 CPU
- `postgres`: 2 GiB / 1 CPU
- `redis`: 512 MiB / 0.5 CPU
- `qdrant`/`weaviate`/`milvus`: 4 GiB / 2 CPU
- `neo4j`: 2 GiB / 1 CPU
- `minio`: 1 GiB / 0.5 CPU
- `langfuse-*`: 1 GiB / 1 CPU
- `clickhouse`: 4 GiB / 2 CPU
- `frontend` (nginx): 256 MiB / 0.25 CPU

🛡️ **No-Regression-Check:** Werte ggf. in Test-Umgebung mit Last-Tests verifizieren; ein zu enges Limit erzeugt OOM-Restarts statt Host-Crash — gewünschtes Verhalten.

---

### 🔴 Problem 10: SQLite-Tabellennamen via `fmt.Sprintf` in Queries
🔍 **Ursache:** `fmt.Sprintf` interpoliert die Tabelle direkt in den SQL-String; obwohl der Wert intern aus `vecTableName(dim)` stammt, etabliert das Pattern Präzedenz für spätere User-Inputs.
✅ **Fix-Strategie:** Tabelennamen weiter via `fmt.Sprintf` (Parameter-Bind unterstützt SQLite für DDL-Names nicht), aber durch eine Allowlist filtern. So ist *kein* User-Input möglich, selbst wenn `dim` versehentlich aus außen gespeist würde.
💻 **Code-Änderung** — `internal/application/repository/retriever/sqlite/repository.go` (neue Helper-Funktion + Aufrufstellen)
```go
// NEU (modul-private Helper-Funktion oben in der Datei)
var allowedVecDims = map[int]bool{384: true, 512: true, 768: true, 1024: true, 1536: true, 2048: true, 3072: true, 4096: true}

func safeVecTableName(dim int) (string, error) { // NEU
    if !allowedVecDims[dim] {                    // NEU
        return "", fmt.Errorf("vec dim %d not allowlisted", dim) // NEU
    }                                            // NEU
    return vecTableName(dim), nil                // NEU
}                                                // NEU
```
```go
// GEÄNDERT: 503, 520, 533 — Aufruf statt direktes vecTableName
tbl, err := safeVecTableName(dim) // NEU
if err != nil {                   // NEU
    return                        // NEU
}                                 // NEU
sql := fmt.Sprintf("INSERT INTO %s(rowid, embedding) VALUES (?, ?)", tbl) // GEÄNDERT
r.db.Exec(sql, rowID, blob)
```
🛡️ **No-Regression-Check:** Allowlist deckt alle aktuell von Embedding-Modellen genutzten Dimensionen ab. Neue Modelle müssen explizit eingetragen werden — gewollte Reibung.

---

## HIGH FIXES

### 🔴 Problem 11: `_ = c.ShouldBindJSON(&req)` in `tag.go`
🔍 **Ursache:** Bind-Fehler wird ignoriert; ein Body mit falschem Typ landet stillschweigend mit Zero-Values im weiteren Code.
✅ **Fix-Strategie:** Bind-Fehler nur bei nicht-leerem Body als Hard-Fail werten (Endpoint nimmt Body optional) — exakte Geschäftslogik bleibt.
💻 **Code-Änderung** — `internal/handler/tag.go:303-306`
```go
var req DeleteTagRequest
if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF { // GEÄNDERT
    c.Error(errors.NewBadRequestError("invalid request body"))   // NEU
    return                                                       // NEU
}                                                                // GEÄNDERT
```
🛡️ **No-Regression-Check:** Aufrufer ohne Body senden weiter `EOF` — das wird ignoriert, exakt das aktuelle Verhalten. Aufrufer mit Garbage-Body bekommen jetzt 400 statt eines Phantom-Erfolgs.

---

### 🔴 Problem 12: `math/rand` (global) in `RandomSelect` und Standard-Imports
🔍 **Ursache:** Globaler `math/rand` ist ohne explizites Seeding deterministisch, ohne Seed schwach für Auswahl­logik in HTTP-Handlern.
✅ **Fix-Strategie:** Auf `math/rand/v2` umstellen — keine API-Bruchstelle für `RandomSelect`-Aufrufer.
💻 **Code-Änderung** — `internal/handler/initialization.go:8, 2372`
```go
import (
    // ... unverändert
    rand "math/rand/v2"  // GEÄNDERT
    // ...
)
```
```go
func (h *InitializationHandler) FabriTag(c *gin.Context) {
    n := rand.IntN(len(tagOptions)-1) + 1   // GEÄNDERT: rand.IntN statt rand.Intn
    tagRandom := RandomSelect(tagOptions, n)
    // ... unverändert
}
```
🛡️ **No-Regression-Check:** `math/rand/v2` ist seit Go 1.22 stabil; das Modul deklariert `go 1.24.11`. Ergebnisse bleiben pseudo-zufällig, aber kryptographisch besser geseedet.

---

### 🔴 Problem 13: Reranker-`http.Client{}` ohne Timeout
🔍 **Ursache:** Ein Default-`&http.Client{}` hat `Timeout: 0` (unendlich) — eine hängende Upstream-Verbindung blockiert eine Goroutine bis zum OS-Connection-Reset.
✅ **Fix-Strategie:** Ein gemeinsames Default-Timeout setzen (90 s, Reranker-Aufrufe sind kurz). Vier Stellen, identisches Pattern.
💻 **Code-Änderung** — `internal/models/rerank/zhipu_reranker.go:74` (analog `aliyun_reranker.go:90`, `jina_reranker.go:63`, `remote_api.go:65`)
```go
return &ZhipuReranker{
    modelName: config.ModelName,
    modelID:   config.ModelID,
    apiKey:    apiKey,
    baseURL:   baseURL,
    client:    &http.Client{Timeout: 90 * time.Second}, // GEÄNDERT
}, nil
```
🛡️ **No-Regression-Check:** 90 s ist großzügig für Reranking. Bei Bedarf via Config tun­able machen — Folge-Ticket.

---

### 🔴 Problem 14: WeChat-Datei-Download ohne SSRF-Validierung
🔍 **Ursache:** `msg.FileKey` ist eine vom Server (oder einem manipulierten Webhook-Payload) gelieferte URL — sie wird ohne Schema-/IP-Check direkt gefetcht.
✅ **Fix-Strategie:** Vorhandene `utils.NewSSRFSafeHTTPClient` nutzen statt des Modul-`ilinkHTTPClient`. Pattern existiert bereits (`mineru_cloud_converter.go:172`).
💻 **Code-Änderung** — `internal/im/wechat/adapter.go:155-163`
```go
// GEÄNDERT: SSRF-Validierung der URL
if err := utils.ValidateURLForSSRF(msg.FileKey); err != nil { // NEU
    return nil, "", fmt.Errorf("reject unsafe file URL: %w", err) // NEU
}                                                                  // NEU

req, err := http.NewRequestWithContext(ctx, http.MethodGet, msg.FileKey, nil)
if err != nil {
    return nil, "", fmt.Errorf("create download request: %w", err)
}

client := utils.NewSSRFSafeHTTPClient(utils.SSRFSafeHTTPClientConfig{ // GEÄNDERT
    Timeout:      120 * time.Second,                                  // GEÄNDERT
    MaxRedirects: 5,                                                  // GEÄNDERT
})                                                                    // GEÄNDERT
resp, err := client.Do(req)                                           // GEÄNDERT (war: ilinkHTTPClient)
```
🛡️ **No-Regression-Check:** WeChat-CDN-Domains liegen üblicherweise im öffentlichen Adressraum — die SSRF-Allowlist im `utils.IsSSRFWhitelisted` muss ggf. um die WeChat-Domain ergänzt werden, falls private/spezielle Routen nötig sind.

---

### 🔴 Problem 15: 13× ignorierte `saveKBCloneProgress`-Fehler
🔍 **Ursache:** `_ = s.saveKBCloneProgress(...)` verwirft Persistenz-Fehler still; ein DB-Glitch lässt das Klon-Job ohne sichtbaren Status weiterlaufen.
✅ **Fix-Strategie:** Auf einen einheitlichen `logger.Errorf(...)`-Fehlerpfad umstellen — kein Abbruch der Operation, aber Sichtbarkeit.
💻 **Code-Änderung** — `internal/application/service/knowledge_clone_move.go:260, 310, 342, 368, 413, 430, 440, 484, 579, 743, 758, 797, 810`
```go
// Alle 13 Stellen — Pattern identisch:
if err := s.saveKBCloneProgress(ctx, progress); err != nil {       // GEÄNDERT
    logger.Errorf(ctx, "Failed to persist KB clone progress: %v", err) // NEU
}
```
🛡️ **No-Regression-Check:** Die Funktion wird häufig in Try-und-Hopp-Pfaden gerufen (z. B. `handleError` Z. 254-261). Die zusätzliche Log-Zeile macht keine Funktion fehlschlagen.

---

### 🔴 Problem 16: `actions/checkout@v3` (deprecated)
🔍 **Ursache:** GitHub Actions v3 erhält keine Patches mehr; v4 ist drop-in.
✅ **Fix-Strategie:** Pattern-Replace im Workflow.
💻 **Code-Änderung** — `.github/workflows/docker-image.yml:18, 67, 100, 143`
```yaml
- uses: actions/checkout@v4   # GEÄNDERT (alle 4 Stellen)
```
🛡️ **No-Regression-Check:** v4 erfordert Node 20-Runner — `ubuntu-latest` liefert das bereits.

---

### 🔴 Problem 17: `:latest`-Image-Tags
🔍 **Ursache:** `:latest` ist nicht reproduzierbar — Upstream-Major-Bump kann Compose-Up brechen.
✅ **Fix-Strategie:** Konkrete Versionen pinnen. Hier nur Default-Werte vorschlagen; `WEKNORA_VERSION` aus Env behalten.
💻 **Code-Änderung** — `docker-compose.yml:3, 29, 144, 174, 290, 366` (analog `docker-compose.dev.yml:39, 106, 165, 190`)
```yaml
# GEÄNDERT — beispielhaft, Versionen vor Commit verifizieren
image: wechatopenai/weknora-ui:${WEKNORA_VERSION:-v0.5.1}
image: wechatopenai/weknora-app:${WEKNORA_VERSION:-v0.5.1}
image: wechatopenai/weknora-docreader:${WEKNORA_VERSION:-v0.5.1}
image: dexidp/dex:v2.41.1
image: neo4j:5.24-community
image: jaegertracing/all-in-one:1.76.0
```
🛡️ **No-Regression-Check:** Alle Tags müssen real existieren — vor Merge `docker pull <image>:<tag>` testen.

---

### 🔴 Problem 18: `migrate.sh` loggt DB-Passwort in Klartext
🔍 **Ursache:** `echo "DB_PASSWORD: ${DB_PASSWORD}"` läuft in jeder Migration und landet in CI-Logs / Container-Logs.
✅ **Fix-Strategie:** Zeile entfernen; restliche Logs unverändert (DB-URL ohne Passwort als Hint reicht).
💻 **Code-Änderung** — `scripts/migrate.sh:67-69`
```bash
# GEÄNDERT
echo "DB_USER: ${DB_USER}"
# ENTFERNEN: echo "DB_PASSWORD: ${DB_PASSWORD}"
echo "DB_HOST: ${DB_HOST}"
```
Zusätzlich `DB_URL`-Output sanitisieren:
```bash
# NEU vor "echo DB_URL"
SANITIZED_URL="${DB_URL//${DB_PASSWORD}/***}"
echo "DB_URL: ${SANITIZED_URL}"
```
🛡️ **No-Regression-Check:** Migrations-Aufruf nutzt `${DB_URL}` weiter — nur das *Echo* wird redacted.

---

### 🔴 Problem 19: 13 Vue-`{ deep: true }`-Watcher
🔍 **Ursache:** `deep: true` traversiert bei jedem Re-Set den gesamten Objekt-Baum — O(n) pro Change auf großen Listen.
✅ **Fix-Strategie:** Wo möglich auf Surrogat (`computed` mit `JSON.stringify`-Hash oder Selector) wechseln. Minimal: bei Listen via `() => list.length` als Watcher-Source. Pro Datei einzelner Patch — hier ein repräsentatives Beispiel für `views/chat/index.vue:213-218`:
💻 **Code-Änderung**
```ts
// GEÄNDERT: messagesList → messagesList.length + Selector
watch(
  () => messagesList.value.length,                        // GEÄNDERT
  () => { /* aktuelle Logik unverändert */ },
)
```
🛡️ **No-Regression-Check:** Ändert Trigger-Frequenz: Watcher feuert nur bei Längen­änderung, nicht bei Inhalts-Mutation. Wenn Inhalts-Mutation beobachtet werden muss, gezielt auf das Feld watchen, nicht auf den ganzen Baum. Pro Stelle einzeln zu prüfen.

---

### 🔴 Problem 20: `context.Background()` in 13 Stellen (Queue, Service-Startup, Goroutines)
🔍 **Ursache:** Detached `context.Background()` ignoriert Shutdown-Signal und Request-Deadline — Goroutines überleben Server-Stop.
✅ **Fix-Strategie:** Wo Parent-Ctx erreichbar: durchreichen. Wo nicht: einen langlebigen `serviceCtx` aus dem Container injizieren, der auf `os.Signal` cancelt.

💻 **Code-Änderung 1** — `internal/handler/initialization.go:1069-1073`
```go
// GEÄNDERT: Parent-Ctx aus Handler nehmen, aber Timeout über das Lifetime des Handlers hinaus
parentCtx := h.shutdownCtx                                             // NEU (Field, im Constructor gesetzt)
newCtx, cancel := context.WithTimeout(parentCtx, 12*time.Hour)         // GEÄNDERT
go func() {
    defer cancel()
    h.downloadModelAsync(newCtx, taskID, req.ModelName)
}()
```
Konstruktor-Änderung in `cmd/server/main.go` o. ä. — `shutdownCtx` aus `signal.NotifyContext(...)` an Handler durchreichen.

💻 **Code-Änderung 2** — `internal/handler/session/agent_stream_handler.go:391-405`
```go
// GEÄNDERT: keine eigene bgCtx mehr; Parent-Ctx (mit kurzer Verlängerung) verwenden
ctxAppend, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second) // NEU
defer cancel()                                                                       // NEU
if err := h.streamManager.AppendEvent(ctxAppend, h.sessionID, h.assistantMessageID, // GEÄNDERT
    interfaces.StreamEvent{ /* unverändert */ }); err != nil {
    logger.GetLogger(h.ctx).Warn("…", "error", err)
}
```
*(`context.WithoutCancel` kappt Cancel-Propagation, behält aber Werte wie TenantID — Go 1.21+.)*

💻 **Code-Änderung 3** — `internal/application/service/session.go:507-520`
```go
go func() {
    bgCtx := context.WithoutCancel(ctx) // GEÄNDERT: Werte erhalten, Cancel kappen
    // tenantID/requestID/language/langfuseTrace-Propagation entfällt — context.WithoutCancel macht sie beibehalten
    // … restliche Logik unverändert
}()
```
🛡️ **No-Regression-Check:** `context.WithoutCancel` ist Go 1.21+ — `go.mod` deklariert 1.24, also ok. Werte werden weiterhin propagiert; nur Cancel-Signale nicht.

---

## MEDIUM / WEITERE FIXES (kompakt)

### 🔴 Problem 21: `json.Marshal` ohne Fehlerprüfung in Feishu (`adapter.go:735, 791, 796, 837, 873, 876, 918`)
🔍 **Ursache:** `payload, _ := json.Marshal(map[...])` ignoriert Marshalling-Fehler, die für `map[string]interface{}` mit fremden Werten real auftreten können.
✅ **Fix-Strategie:** `_` durch echte Variable ersetzen + Frühreturn.
💻 **Code-Änderung** — Beispiel Z. 735:
```go
payload, err := json.Marshal(map[string]interface{}{ // GEÄNDERT
    "type": "card_json",
    "data": cardJSON,
})
if err != nil {                                       // NEU
    return "", fmt.Errorf("marshal card payload: %w", err) // NEU
}                                                     // NEU
```
🛡️ Selber Pattern auf alle 9 Vorkommen anwenden.

---

### 🔴 Problem 22: Slack `json.Unmarshal`-Fehler ignoriert (`adapter.go:143`)
🔍 **Ursache:** Wenn der Body kein JSON ist, bleibt `rawEvent.Event.Files` `nil` — keine Symptomatik, aber stiller Datenverlust.
✅ **Fix-Strategie:** Fehler loggen, aber den Adapter weiterlaufen lassen (Files sind optional).
💻 **Code-Änderung**
```go
if err := json.Unmarshal(bodyBytes, &rawEvent); err != nil {                    // GEÄNDERT
    logger.GetLogger(c.Request.Context()).Warn("slack: parse files failed", "error", err) // NEU
}
```

---

### 🔴 Problem 23: Ollama-Tool-Param-Unmarshal ignoriert (`models/chat/ollama.go:193, 200`)
🔍 **Ursache:** Stiller Datenverlust bei Tool-Calls, die nachher mit leerem `Parameters`-Map ausgeführt werden.
✅ **Fix-Strategie:** Fehler loggen + den betroffenen Tool-Call überspringen statt mit leeren Params auszuführen.
💻 **Code-Änderung** (Pseudo-Pattern):
```go
if err := json.Unmarshal(tool.Function.Parameters, &function.Parameters); err != nil { // GEÄNDERT
    logger.Warnf(ctx, "skip tool with bad params: %v", err)                            // NEU
    continue                                                                           // NEU
}
```

---

### 🔴 Problem 24: `resp.Body.Close()` ohne `defer` in MinerU-Convertern
🔍 **Ursache:** Bei Fehler zwischen `resp` und expliziter `Close()` leakt der Body-Reader.
✅ **Fix-Strategie:** Auf `defer` umstellen — keine Logik­änderung.
💻 **Code-Änderung** — `internal/infrastructure/docparser/mineru_cloud_converter.go:177`, `mineru_converter.go:263`
```go
resp, err := client.Do(httpReq)
if err != nil {
    return fmt.Errorf("PUT upload: %w", err)
}
defer resp.Body.Close()           // GEÄNDERT (vorher direkter resp.Body.Close())
```

---

### 🔴 Problem 25: `defer file.Close()` ohne Fehlerprüfung (`initialization.go:2038`)
🔍 **Ursache:** Bei Upload-Streams maskiert ein verschluckter Close-Fehler einen unvollständigen Schreib-Flush.
✅ **Fix-Strategie:** Wrapper-Funktion einsetzen, die das Ergebnis loggt — keine Logik­änderung.
💻 **Code-Änderung**
```go
defer func() {                                                              // GEÄNDERT
    if cerr := file.Close(); cerr != nil {                                  // NEU
        logger.Warnf(ctx, "close uploaded file: %v", cerr)                  // NEU
    }                                                                       // NEU
}()                                                                         // GEÄNDERT
```

---

### 🔴 Problem 26: `asyncio` / DNS-Lookup mit `context.Background()`
🔍 **Ursache:** `net.DefaultResolver.LookupIP(context.Background(), …)` ignoriert Aufrufer-Timeout.
✅ **Fix-Strategie:** Aufrufer-Ctx durchreichen (existiert bereits in der umgebenden Funktion).
💻 **Code-Änderung** — `internal/agent/tools/web_fetch.go:286`, `internal/infrastructure/web_fetch/fetcher.go:53`
```go
ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname) // GEÄNDERT
```

---

### 🔴 Problem 27: `v-html` ohne durchgängige Sanitisierung (Frontend)
🔍 **Ursache:** Mehrere `v-html`-Sites (`AgentStreamDisplay.vue:329, 365`; `GlobalCommandPalette.vue:127, 154`) verlassen sich darauf, dass Aufrufer den Content schon „sicher" hinterlassen haben.
✅ **Fix-Strategie:** Einen einzigen `safeHtml(...)`-Helper erzwingen, der `dompurify.sanitize` mit konservativem Profile aufruft. Alle `v-html`-Bindings nur über diesen Helper. Kein Refactoring der umgebenden Komponenten.
💻 **Code-Änderung** — `frontend/src/utils/security.ts` (Helper bereits vorhanden, aber nicht überall genutzt):
```ts
// In jeder verbleibenden Komponente:
import { safeHtml } from '@/utils/security'  // NEU
```
```vue
<!-- GEÄNDERT in den vier betroffenen Templates -->
<div v-html="safeHtml(floatPopup.content)" />
<div v-html="safeHtml(wikiDrawerContent)" />
<div v-html="safeHtml(highlight(item.label, query))" />
```
🛡️ Bei `highlight(...)` zusätzlich verifizieren, dass die HTML-Konstruktion nur `<mark>`-Tags einfügt — Whitelist im DOMPurify-Profil entsprechend setzen.

---

### 🔴 Problem 28: SSE/Stream wird beim Routenwechsel nicht abgebrochen
🔍 **Ursache:** Der `AbortController` wird nur in `stopStream()` & `onUnmounted` aufgerufen — Routenwechsel ohne Unmount der Composable lässt den Stream weiterlaufen.
✅ **Fix-Strategie:** In der konsumierenden View einen `onBeforeRouteLeave`-Hook ergänzen.
💻 **Code-Änderung** — `frontend/src/views/chat/index.vue` (in passender Position der Composition-API-Section)
```ts
import { onBeforeRouteLeave } from 'vue-router' // NEU

onBeforeRouteLeave(() => {                       // NEU
  stopStream?.()                                  // NEU
})                                                // NEU
```

---

### 🔴 Problem 29: WeChat-Polling-Timer nicht garantiert geleert
🔍 **Ursache:** `wechatPollTimer = setTimeout(pollOnce, 500)` setzt sich rekursiv neu; falls `stopWeChatPolling()` mitten im `pollOnce()`-Lauf aufgerufen wird, hat ein in-flight Promise bereits den nächsten `setTimeout` queue'd.
✅ **Fix-Strategie:** Vor dem `setTimeout` `wechatPollActive`-Check zwingen — bestehender Code hat das schon (Z. 574). Zusätzlich `stopWeChatPolling` im finally-Block der pollOnce-Funktion sicher abrufen, falls Backend einen Final-Status meldet.
💻 **Code-Änderung** — `frontend/src/components/IMChannelPanel.vue:573-577`
```ts
} catch {
  // transient error
} finally {                                                  // NEU
  if (!wechatPollActive && wechatPollTimer) {                // NEU
    clearTimeout(wechatPollTimer)                            // NEU
    wechatPollTimer = null                                   // NEU
  }                                                          // NEU
}
if (wechatPollActive) {
  wechatPollTimer = setTimeout(pollOnce, 500)
}
```

---

### 🔴 Problem 30: `subprocess.run(["which", …])` ohne `timeout=`
🔍 **Ursache:** `which` mit korruptem `PATH` kann hängen.
✅ **Fix-Strategie:** Konstantes 5 s-Timeout setzen.
💻 **Code-Änderung** — `docreader/parser/doc_parser.py:258-260`
```python
result = subprocess.run(
    ["which", executable_name],
    capture_output=True, text=True,
    timeout=5,  # NEU
)
```

---

### 🔴 Problem 31: `print()` in Library-Code (DocReader)
🔍 **Ursache:** Library-Output landet auf stdout, wird vom strukturierten Logger nicht erfasst.
✅ **Fix-Strategie:** 1:1-Ersatz `print(x)` → `logger.info(x)`. Pattern auf alle Vorkommen (`chain_parser.py:179`, `markdown_parser.py:124`, `web_parser.py:149-162`, `excel_parser.py:114-118`).
💻 **Code-Änderung** (Beispiel)
```python
# GEÄNDERT
logger.info(format_content)
```

---

### 🔴 Problem 32: `chain_parser.py:62` — bare `except Exception:`
🔍 **Ursache:** Fängt zu breit (inkl. Programmfehler), versteckt Diagnose.
✅ **Fix-Strategie:** Auf `(IOError, ValueError, RuntimeError)` einengen — bekannte Parser-Fehlerklassen. `KeyboardInterrupt`/`SystemExit` propagieren weiter.
💻 **Code-Änderung**
```python
try:
    document = p.parse_into_text(content)
except (IOError, ValueError, RuntimeError) as e:    # GEÄNDERT
    logger.exception(
        "FirstParser: parser %s failed: %s; trying next",
        p.__class__.__name__, e,
    )
    continue
```

---

### 🔴 Problem 33: `target="_blank"` ohne `rel="noopener noreferrer"`
🔍 **Ursache:** Ohne `rel` kann die Zielseite via `window.opener` zurück-manipulieren.
✅ **Fix-Strategie:** `rel`-Attribut nachpflegen — reine Template-Änderungen.
💻 **Code-Änderung** — `frontend/src/views/auth/Login.vue:96, 102, 111`, `frontend/src/views/chat/components/docInfo.vue:17`, `frontend/src/views/settings/StorageEngineSettings.vue:253-362`
```html
<a href="…" target="_blank" rel="noopener noreferrer">…</a> <!-- GEÄNDERT -->
```

---

### 🔴 Problem 34: `.catch(() => {})` (6×)
🔍 **Ursache:** Fehler werden ohne Log/UI verschluckt.
✅ **Fix-Strategie:** `console.warn` als Mindestniveau; bei kritischen Operationen (`saveConfig`) zusätzlich Toast.
💻 **Code-Änderung** — `frontend/src/components/Input-field.vue:549` (analog 5 weitere Stellen)
```ts
orgStore.fetchSharedKnowledgeBases()
  .catch((err) => console.warn('[fetchSharedKnowledgeBases] failed:', err)) // GEÄNDERT
```

---

### 🔴 Problem 35: `set -euo pipefail` fehlt in Shell-Skripten
🔍 **Ursache:** Unset-Vars werden zu `""`, kaputte Pipes failen still.
✅ **Fix-Strategie:** Direkt nach Shebang einfügen.
💻 **Code-Änderung** — `scripts/dev.sh:2`, `scripts/build_images.sh:2`
```bash
#!/bin/bash
set -euo pipefail   # NEU
```
🛡️ **No-Regression-Check:** Vorhandene optionale Variablen (`${VAR:-default}`) sind weiter ok; ungesetzte ohne Fallback brechen jetzt früh — gewünscht.

---

### 🔴 Problem 36: Alte Python-Typings (`List`, `Dict`, `Optional`)
🔍 **Ursache:** PEP 585/604 sind seit 3.10 Standard; `from typing import List, Dict, Optional` verursacht keine Bugs, ist aber Bit-Rot-Ballast.
✅ **Fix-Strategie:** Suchen-und-Ersetzen pro Datei (`List[X]` → `list[X]`, `Optional[X]` → `X | None`). Importe entsprechend kürzen. Keine Verhaltens­änderung.
💻 **Code-Änderung** — Beispiel `docreader/parser/doc_parser.py:1-15, 234-236`
```python
# GEÄNDERT: Imports
from typing import Any  # NEU (falls noch genutzt)
# ENTFERNEN: from typing import List, Optional, Dict
```
```python
def _try_find_executable_path(
    self,
    executable_name: str,
    possible_path: list[str] | None = None,        # GEÄNDERT
    environment_variable: list[str] | None = None, # GEÄNDERT
) -> str | None:                                   # GEÄNDERT
```

---

### 🔴 Problem 37: Healthchecks fehlen für Redis/Qdrant/Weaviate
🔍 **Ursache:** Ohne Healthcheck löst `depends_on: service_started` Startup-Race aus; `app` startet zu früh.
✅ **Fix-Strategie:** Pro Service Healthcheck ergänzen + im `app.depends_on` auf `service_healthy` umstellen.
💻 **Code-Änderung** — `docker-compose.yml`
```yaml
  redis:
    # ... bestehend
    healthcheck:                                   # NEU
      test: ["CMD", "redis-cli", "ping"]           # NEU
      interval: 10s                                # NEU
      timeout: 5s                                  # NEU
      retries: 5                                   # NEU

  qdrant:
    # ... bestehend
    healthcheck:                                   # NEU
      test: ["CMD-SHELL", "wget -qO- http://localhost:6333/healthz || exit 1"] # NEU
      interval: 10s                                # NEU
      timeout: 5s                                  # NEU
      retries: 5                                   # NEU
      start_period: 20s                            # NEU

  weaviate:
    # ... bestehend
    healthcheck:                                   # NEU
      test: ["CMD-SHELL", "wget -qO- http://localhost:8080/v1/.well-known/ready || exit 1"] # NEU
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s
```
```yaml
  app:
    depends_on:
      redis:
        condition: service_healthy   # GEÄNDERT
      postgres:
        condition: service_healthy
```

---

### 🔴 Problem 38: `.env.example` ↔ `.env.lite.example` Schema-Drift
🔍 **Ursache:** Schlüssel divergieren (`DB_HOST` vs `DB_PATH`); Kopier-Verwendung führt zu undefinierten Variablen.
✅ **Fix-Strategie:** Einen kommentierten Header in beide Dateien, der die andere Variante referenziert; gemeinsame Schlüssel auf identische Namen ziehen.
💻 **Code-Änderung** — `.env.example:1` und `.env.lite.example:1`
```bash
# WeKnora Environment — Vollversion (Postgres + alle Services).
# Für SQLite-Lite-Modus: siehe .env.lite.example. Schlüssel-Schemas sind NICHT identisch.
# Pflicht-Vars: JWT_SECRET, DB_*, MINIO_*, NEO4J_PASSWORD, LANGFUSE_*  # NEU
```

---

### 🔴 Problem 39: Container laufen ggf. als root
🔍 **Ursache:** Mehrere Dockerfiles haben kein `USER` am Ende; ein Compromise des Prozesses gibt root-im-Container.
✅ **Fix-Strategie:** Im finalen Stage `USER appuser` setzen. Pattern existiert bereits in `Dockerfile.app` als Vorbild — auf `Dockerfile.docreader`/`frontend/Dockerfile` ausweiten.
💻 **Code-Änderung**
```dockerfile
# Am Ende des finalen Stage:
USER appuser   # NEU
```

---

### 🔴 Problem 40: Race in `fetchSuggestedQuestions`
🔍 **Ursache:** Der `setTimeout`-Debounce verhindert Mehrfach­anfragen *nicht*, sondern verzögert sie nur; ein zwischenzeitlicher Routenwechsel kann zur falschen Session geliefert werden.
✅ **Fix-Strategie:** `AbortController` ergänzen, der bei jedem neuen Aufruf den vorherigen abbricht.
💻 **Code-Änderung** — `frontend/src/views/chat/index.vue:159-189`
```ts
let suggestionsAbort: AbortController | null = null  // NEU

const fetchSuggestedQuestions = async () => {
  suggestionsAbort?.abort()                          // NEU
  suggestionsAbort = new AbortController()           // NEU
  const fetchId = ++suggestedQuestionsFetchId
  // ...
  try {
    const res = await getSuggestedQuestions(agentId, {
      knowledge_base_ids: …,
      knowledge_ids: …,
      limit: 6,
      signal: suggestionsAbort.signal,               // NEU
    })
    // ...
  } catch (err) {
    if (err.name === 'AbortError') return            // NEU
    // ... bestehender Catch-Path
  }
}
```
🛡️ **No-Regression-Check:** `getSuggestedQuestions` muss `signal` an axios durchreichen — kleine API-Erweiterung, kein Breaking-Change.

---

## ZUSAMMENFASSUNG

- **Critical-Findings adressiert (1–10):** CORS, Slack-Bypass, Compose-Defaults, Encryption-Key, curl-pipe, JWT-Storage, asyncio.run, mutable defaults, resource limits, SQL-Templates.
- **High-Findings adressiert (11–20):** ShouldBindJSON, math/rand, Reranker-Timeout, WeChat-SSRF, KB-Clone-Logging, GH-Action-Pin, image-pin, migrate.sh-Logging, deep-watcher, context.Background.
- **Medium/Restliche (21–40):** Marshal/Unmarshal-Errors, Body.Close-Defers, file.Close-Logging, DNS-Ctx, v-html-Sanitization, Stream-Abort beim Route-Leave, Polling-Timer, subprocess-Timeout, print→logger, except-Refactor, rel-Attribute, .catch-Logging, set-euo, PEP585-Typings, Healthchecks, env-Drift, USER-direktive, AbortController.

**Verbleibend für separate Tickets** (zu groß für minimal-invasive Patches):
- HTTP-only-Cookies für JWT (Backend + Frontend Roundtrip).
- Vereinheitlichte IM-Webhook-Verifikation hinter gemeinsamem Interface.
- `defusedxml` im DocReader.
- `setupTLS` mit `mTLS` zwischen App und DocReader.
- Distroless-Migration der Runtime-Images.

*Erstellt 2026-05-06. Patches sind Vorschläge — vor Merge pro Datei manuell verifizieren und Tests schreiben.*
