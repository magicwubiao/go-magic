# go-magic Authentication & URL Ticket Model

> Applies to: v0.5.9+
> Implementation: `internal/server/auth.go`, `internal/server/fs_ticket.go`, `internal/server/fs_serve.go`
> Chinese version: [AUTH.zh-CN.md](AUTH.zh-CN.md)

How the Dashboard / HTTP API authenticates requests, and how to handle the cases where a credential *must* appear in a URL (`<img src>`, `<a href>`, `EventSource`, iframe sub-resources).

---

## 1. TL;DR

1. **Login credentials travel in request headers only**: `Authorization: Bearer <token>` or `X-Magic-Session-Token: <token>`.
2. **`?token=<login credential>` has been removed entirely** — no endpoint accepts it any more (`/api/fs/*`, `/api/uploads/*`, `/api/events` included).
3. For requests the browser **cannot attach headers to**, use a **ticket**: call `POST /api/fs/sign` with header auth, then hand the returned URL to `<img>` / `<a>` / `EventSource`.
4. A ticket is **self-contained, purpose-scoped and hard-expiring**; it unlocks exactly one action on one file/directory, shrinking the blast radius from "the whole API" to "one file, one window of time".
5. Two intentional exceptions, neither of which is a login credential: the public share link `/api/fs/shared/<token>` (TTL credential issued by `POST /api/fs/share`) and the cross-machine relay `/api/relay/v1/dm` (token validated inside the request body).

---

## 2. Credential layers

| Credential | Form | Stored in | Purpose | May appear in a URL |
|------------|------|-----------|---------|---------------------|
| Login password | plaintext, user-supplied | user's head only | exchange for a session token | no |
| `authToken` | bcrypt hash of the password | `<magic_home>/.auth_token` | static credential; source of the ticket signing key | no |
| Session token | random string issued by the server | frontend `localStorage` (login response) | day-to-day API auth | no |
| Ticket | `base64url(v1␀scope␀path␀opts␀exp).base64url(hmac[:16])` | URL **path segment**, or `sig` query param | replaces `?token=`; unlocks a single action | yes (by design) |
| Share token | random string from `/api/fs/share` | URL path | let outsiders view one file/directory | yes (separate system) |

Key points:

* Tickets are cryptographically **separated** from login credentials: the signing key is derived one-way from `authToken` (`sha256("go-magic/fs-ticket/v1\0" + authToken)`). A ticket cannot be used to recover `authToken`, so it cannot be used to impersonate a login.
* Because the key derives from `authToken`, **resetting the password / resetting auth invalidates every existing ticket immediately**.
* Before initialization (`authToken` empty) no ticket is issued at all — otherwise the key would degrade into a public constant.

---

## 3. Header authentication (regular APIs)

```bash
# Preferred: log in with the password to get a session token (supports logout/expiry)
curl -s -X POST http://localhost:5000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"password":"your-password","remember":true}'
# => {"ok":true,"token":"..."}

TOKEN=...   # the value returned above

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://localhost:5000/api/fs/read?path=README.md'
```

The static credential is accepted too (equivalent to a session token, but without logout/expiry — handy for local scripts):

```bash
# authToken is literally the content of <magic_home>/.auth_token (the bcrypt hash)
curl -s -H "Authorization: Bearer $(cat ~/.magic/.auth_token)" \
  'http://localhost:5000/api/fs/list?path=.'
# equivalent
curl -s -H "X-Magic-Session-Token: $(cat ~/.magic/.auth_token)" \
  'http://localhost:5000/api/fs/list?path=.'
```

Cookies never carry the login state, and `?token=` always yields 401.

---

## 4. The ticket model

### 4.1 Structure

```
base64url("v1" ␀ scope ␀ path ␀ opts ␀ exp) . base64url(hmac_sha256(key, payload)[:16])
```

* `scope` decides **what the ticket can do**; the consuming endpoint matches it strictly, and scopes are not interchangeable.
* `path` is resolved to an absolute path by the server **at signing time** (workspace/upload-root boundaries are checked before signing). The consumer only verifies the signature and accepts no client-supplied path.
* `opts` is currently zip-only: an `h` means "include hidden files".
* `exp` is a hard expiry timestamp.
* Every validation failure (missing segment, bad signature, unknown version, expired, path escape) returns **the same error**, deliberately revealing nothing, so failures cannot be used as an oracle.

### 4.2 Scopes and lifetimes

| scope | Unlocks | `path` semantics | Consumed at | TTL |
|-------|---------|------------------|-------------|-----|
| `read` | single-file inline read (`<img src>`, open in a new tab) | absolute path inside the session workspace (file) | `GET /api/fs/ticket/<sig>` | 1 h |
| `download` | single-file attachment download (`<a href>`) | same (file) | `GET /api/fs/ticket/<sig>` | 1 h |
| `zip` | archive download of a file/directory | same (may be a directory) | `GET /api/fs/ticket/<sig>` | 1 h |
| `uploads` | a session's uploaded attachment | **relative path inside the upload root** (e.g. `sess-1/abc.png`) | `GET /api/fs/ticket/<sig>` | 1 h |
| `serve` | static web preview (serve a directory) | absolute path in the workspace (directory, or the file's directory) | `GET /api/fs/serve/<sig>/<subpath>` | 24 h |
| `events` | global SSE event stream | none | `GET /api/events?sig=<sig>` | 24 h |

One-shot action tickets get 1 hour (clients fetch them right before issuing the real request); `serve` must be inherited by relative sub-resources inside the iframe and `events` must survive EventSource auto-reconnects, so both get a day.

### 4.3 Why the serve credential lives in the **path**

`/api/fs/serve/<sig>/<subpath>` instead of a query param:

1. Relative references inside `index.html` (`assets/style.css`) are resolved by **path merge** only (RFC 3986 §5.3); the query string is dropped wholesale — credentials on the query would be lost on every sub-resource, turning every css/js/image into a 401.
2. iframe sub-resource requests (`img`/`link`/`script`) cannot set an `Authorization` header.
3. The preview iframe deliberately runs with `sandbox="allow-scripts"` (no `allow-same-origin`), under which cookies are not sent with sub-resource requests either — and that sandbox is exactly what stops a previewed page from reading `localStorage.auth_token`.

The path segment is the only place that is both inherited automatically by relative references and independent of any browser credential.

### 4.4 Tickets are not single-use

A ticket is reusable within its TTL (`<img>` may be repainted, video scrubbing relies on Range requests, EventSource reconnects). Therefore:

* never paste tickets into logs, tickets systems or public docs;
* never cache a ticket beyond the `expires_in` returned with it;
* to narrow the blast radius further, sign a smaller `path` — a ticket authorizes only the file/directory baked into its payload.

---

## 5. Minting a ticket: `POST /api/fs/sign`

* Auth: header credentials are **required** (`requireAuth`). This is the only place a login credential is needed for URL-based access.
* Request body (JSON, 64 KiB max):

| Field | Type | Notes |
|-------|------|-------|
| `scope` | string | `read` / `download` / `zip` / `uploads` / `serve` / `events`; defaults to `serve` (backward compatible with older frontends) |
| `path` | string | not needed for `events`; relative to the upload root for `uploads`; a workspace path otherwise |
| `session_id` | string | optional, used for workspace boundary checks (not needed for `uploads`) |
| `hidden` | bool | `zip` only: include hidden files in the archive |

* Response: `{"url":"...","expires_in":3600}` (for `serve`: `{"url":"/api/fs/serve/<sig>/<entry>","expires_in":86400}`).

```bash
# Mint a read ticket, then use it as an <img src>
URL=$(curl -s -X POST http://localhost:5000/api/fs/sign \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"scope":"read","path":"/home/me/proj/cover.png"}' | jq -r .url)

curl -s -o cover.png "http://localhost:5000$URL"

# SSE: mint an events ticket and hand it to EventSource
curl -s -X POST http://localhost:5000/api/fs/sign \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"scope":"events"}'
# => {"url":"/api/events?sig=...","expires_in":86400}
```

Equivalent JS (already wrapped as `signFSTicket()` in `web/src/api/sessions.ts`):

```js
const res = await fetch('/api/fs/sign', {
  method: 'POST',
  headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
  body: JSON.stringify({ scope: 'read', path }),
})
const { url } = await res.json()   // pass to <img src={url}>
```

Python:

```python
import requests
s = requests.Session()
s.headers['Authorization'] = f'Bearer {token}'
ticket = s.post(f'{base}/api/fs/sign', json={'scope': 'download', 'path': p}).json()['url']
data = s.get(f'{base}{ticket}').content      # or hand the URL to <a href>
```

### 5.1 Common error codes

| Status | When |
|--------|------|
| 401 `authentication required...` | `/api/fs/sign` or `/api/events` without valid credentials (or the server is not initialized yet) |
| 400 `invalid request body` / `unknown ticket scope: x` | malformed body / misspelled scope |
| 400 `path is a directory` | `read` / `download` pointing at a directory |
| 400 `directory has no index.html to preview` | the `serve` target has no `index.html` (fails early instead of showing an empty iframe) |
| 403 `invalid or expired ticket` | ticket tampered with, expired, or wrong scope (e.g. a `read` ticket pasted into `/api/fs/serve/`) |
| 404 `resource not found` / `attachment not found` | signing target does not exist |

---

## 6. Protected resources at a glance

| Action | Endpoint | Auth |
|--------|----------|------|
| list, read text, write, delete, rename, upload | `GET/POST /api/fs/{list,read,write,delete,rename,upload}` | header |
| download a single file (scripts/curl) | `GET /api/fs/download?path=...` | header |
| archive download (scripts/curl) | `GET /api/fs/zip?path=...&hidden=1` | header |
| download / inline display (browser tab, `<img>`, `<a>`) | `GET /api/fs/ticket/<sig>` | ticket (`read`/`download`/`zip`/`uploads`) |
| static web preview | `GET /api/fs/serve/<sig>/<subpath>` | ticket (`serve`) |
| session uploads | `GET /api/uploads/<session>/<file>` | header (use an `uploads` ticket for `<img>` cases) |
| live event stream | `GET /api/events` | header, or an `events` ticket via `?sig=` |
| public share link | `GET /api/fs/shared/<token>` | the share token itself (independent of login state, TTL bound) |
| cross-machine relay | `POST /api/relay/v1/dm` | relay token inside the request body |

---

## 7. Migration guide (after `?token=` removal)

### 7.1 Old → new

| Old | New |
|-----|-----|
| `GET /api/fs/read?path=p&token=<authToken>` | `GET /api/fs/read?path=p` + `Authorization: Bearer <token>` |
| `<img src="/api/fs/read?path=p&token=...">` | `POST /api/fs/sign {"scope":"read","path":p}` → `<img src={url}>` |
| `<a href="/api/fs/download?path=p&token=...">` | `scope:"download"` |
| `/api/fs/zip?path=p&hidden=1&token=...` | `scope:"zip"` + `hidden:true` |
| `/api/uploads/...?token=...` | `scope:"uploads"` + a path relative to the upload root |
| `/api/fs/serve?path=dir&token=...` | `scope:"serve"` → `/api/fs/serve/<sig>/<entry>` |
| `new EventSource('/api/events?token=...')` | `scope:"events"` → `new EventSource('/api/events?sig=...')` |

### 7.2 Compatibility boundaries (what keeps working)

* **Attachment refs already persisted in old sessions** may still carry `?token=` (left over from the old upload response). The web client strips that query param on read (`stripLegacyUrlToken` in `web/src/api/sessions.ts`), so old sessions keep rendering — **no data migration needed**.
* **CLI / TUI** read and write the local filesystem directly and never touch these HTTP endpoints.
* **The desktop app (go-magic-desktop)** spawns the very same Go binary and embeds the same web UI, so it follows the new model automatically — no changes needed.
* **Cross-machine relay** (`bot_mode.relay_token`) uses a completely separate check from session tokens: `/api/relay/v1/dm` validates only the shared secret in the request body (constant-time compare), and with no secret configured it accepts loopback (127.0.0.1) callers only. Managing the peer table (`/api/peers`) is an admin surface and requires a session token like every other `/api/*` route.
* **Public share links** `/api/fs/shared/<token>` carry a token issued by `POST /api/fs/share` — a credential for "let an outsider view one file", not a login credential. Unchanged (TTL bound, hidden files skipped in directory listings).
* **Header-authenticated use** (scripts, `curl`, server-side integrations) behaves exactly as before: `/api/fs/read`, `/api/fs/download`, `/api/fs/zip`, `/api/uploads/` and `/api/events` still accept header credentials.

### 7.3 The only thing that breaks

Any **external script or third-party integration** that pastes a login credential into a URL (`?token=`) now gets 401:

```
{"error":"authentication required, please setup via /api/auth/setup first"}
```

Apply the table in 7.1: scripts use headers, browsers use tickets.

---

## 8. Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| image 403 `invalid or expired ticket` | ticket expired (`read` lives 1 h) → mint a new one; or the ticket was truncated/altered |
| preview 403 `invalid or expired preview link` | the `serve` ticket expired (24 h) or its signature was altered; call `/api/fs/sign` again |
| all css/js inside a preview return 401 | the credential was put in the query instead of the path; use the `/api/fs/serve/<sig>/<subpath>` form and make sure no reverse proxy rewrites the path |
| one file 403 while others are fine, `read` ticket ignored | scope mismatch: `read` tickets only work at `/api/fs/ticket/<sig>`, `serve` tickets only at `/api/fs/serve/` |
| SSE does not auto-reconnect | a reverse proxy dropped/rewrote the `?sig=` query param, or the ticket expired before the reconnect; proxies must pass the query through |
| every preview/download link dies after a password reset | expected: the signing key derives from `authToken` |
| signing returns 500 `ticket signing unavailable` | the server is not initialized yet (`authToken` empty); visit `/api/auth/setup` first |
| tempted to temporarily restore `?token=` | don't. Use header auth (script scenarios) or sign a narrower ticket |
