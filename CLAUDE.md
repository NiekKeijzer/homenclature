# Homenclature

A household homepage: a simple frosted-glass ("liquid glass") page of links. Users log in with OIDC, and their groups decide which links they see. No database: all config comes from a TOML file and env vars.

## Working rules

- **When unsure about the task, scope or approach, stop and ask the dev.** Don't guess. Asking is always fine.
- Anything not listed under Decisions is open. Propose it and ask before building.
- Keep it small: stdlib first. Ask before adding a dependency.
- Done means `mise run check` passes. For UI or auth changes, also run the app and log in through the mock OIDC.
- Commit or push only when asked.
- Significant changes, especially visible ones, get a screenshot in `docs/screenshots/` shown in `README.md`. Take it from the running app (example config, 2x scale). If the repo then holds more than 3 screenshots, ask the dev which to keep.
- `README.md` is also the Docker Hub description (synced by CI). Use links relative to the repo root; CI makes them absolute.

## Decisions

- Backend: Go (version in `mise.toml`). Single binary. Keep Go code out of the repo root: `cmd/homenclature` (main), `internal/server` (HTTP, auth, icons, backgrounds; templates in `internal/server/web`), `internal/config`.
- Frontend: Go `html/template` + HTMX + plain CSS, embedded with `go:embed`. HTMX and the Sora font come from npm (`package.json`); `mise run assets` copies them into `web/static/lib` (gitignored), so they're still self-hosted (no CDN). The build fails until that has run. Glass look via `backdrop-filter`.
- Auth: OIDC. The prod IdP is Authentik, which sends group names in the `groups` claim.
- Config: the TOML file holds only dashboards (parsed with `BurntSushi/toml`). App settings (OIDC, server) and secrets come from env vars prefixed `HOMENCLATURE_`. No database, no persistent state.
- Dashboards (`config.example.toml`, `internal/config`): one per OIDC group, keyed by group name. `HOMENCLATURE_DEFAULT_DASHBOARD` names the dashboard for users who match none (unset: they see nothing; names a missing dashboard: startup error). A user in several groups gets one merged page: file order, same-title sections merged, links deduped by URL. Links are written inline in sections. `url`, `api_url` and `seerr_url` can instead come from the env var named by `url_env`, `api_url_env` or `seerr_url_env` (one or the other; unset is a startup error). Sections have a title and an optional `layout`: `list` (default) or `cards`; when sections merge, the first layout set wins.
- Icons: `icon` is a dashboard-icons name or a URL. `/icons/{hash}` fetches it (SVG, then PNG fallback) and caches it in memory. Only icons from the config can be fetched.
- Auth: `coreos/go-oidc/v3` + `golang.org/x/oauth2`, with PKCE, state and nonce. Groups come from the `groups` claim, scopes `openid profile email`.
- Sessions: AES-GCM encrypted cookie (stdlib), key from `HOMENCLATURE_SESSION_SECRET`, valid for 24h, holding the OIDC `sub`, first name and groups. No logout button.
- Page (look "D: search first", see the mockups canvas): a clock with the date and greeting (first name), computed in the browser; a search pill that filters links client-side (`/` focuses, ↑/↓ select, Enter opens, Tab cycles the app chips, Esc clears); while it's focused or has a query the clock shrinks beside the date, animated FLIP-style in `search.js`; sections as glass panels (cards sections get a full row; neighbouring list sections share a row when there's room). Dark with glow blobs by default, a pastel light version when the system is light. Sora (OFL, `@fontsource-variable/sora`, served from `web/static/lib`) for the clock and headings, system fonts otherwise. JS is in `web/static/js`: ES modules, no bundler; `app.js` imports one per part (clock, landscape, slideshow, search, commands), and each runs only if its element is on the page. HTMX 2 (`htmx.org`) is served from `web/static/lib/htmx.min.js` and only loaded when the user can search an app.
- Search inside apps (`internal/search`, `internal/server/search.go`): opt in per link with `search = { type, token_env, api_url?, limit?, ... }`; types home-assistant (`domains`), immich, jellyfin (`types`, `seerr_url`), mealie, paperless, seerr. Seerr lists matching films and shows, skipping available and blocklisted ones, and links to them in Seerr, where people request as themselves; its posters come from TMDB's image CDN, without the key. The token comes from the env var named by `token_env`; unset is a startup error. Read-only: results deep-link into the app. After 2+ characters and a 300ms pause, HTMX loads `/search`: one glass section per app below the links (Immich as a photo grid), with chips (swapped out of band) to filter by app, and a keyboard hint bar. Only apps on the user's dashboard are searched, in parallel with a 3s timeout, using the configured token. Apps with no results are hidden, and so are apps whose search failed (logged); Jellyfin's section ends with a link to its own search, plus one to Seerr's search (`seerr_url`) for users without Seerr search on their dashboard (decided in `internal/server/search.go`). Thumbnails go through `/search/thumb/{key}/{id}`, with the ID validated per app.
- Personal keys (`internal/server/keys.go`): for home-assistant, immich, mealie and paperless (`keyHelp`; not Jellyfin, where only admins make keys, nor Seerr), people can set their own key with a command (below), or on the `/keys` page, which nothing links to (only for people given the URL); it replaces the shared token for their searches and thumbnails, and `token_env` stays required. No database and no env vars: keys live in an encrypted cookie (`homenclature_keys`, 400 days, renewed on each login), keyed by link URL and tied to the session's `sub`, so someone else's keys in the same browser are ignored. A key is tried with a search before it's saved. `POST /keys` and `POST /commands/keys` are behind `http.CrossOriginProtection`.
- Commands (`internal/server/commands.go`, `web/commands.html`, `web/static/js/commands.js`): the search pill doubles as a command panel; no buttons in the pill. A query starting with `>` hides links and app results (`/search` ignores it) and shows the user's commands, rendered into the page and filtered in the browser by title (every word, any order). Commands never leave the page. They can be grouped: `>` alone lists the top level, where a command opens each group (Esc goes back); typed words search all commands. The first group, opened by "Manage your keys", holds the key commands: use your own key (asks for it in the pill, in a separate password field so a key never reaches `/search`), replace and remove; after one runs, its group is open. `POST /commands/keys` answers with the updated command list, or the error as text. Found via a "Type > for commands" row under the empty, focused pill and `>` in the desktop hint bar. No commands, no hint.
- Background (`internal/server/background.go`): one for everyone, set by `HOMENCLATURE_BACKGROUND`. `default`: the blobs. `landscape`: an inline SVG (`web/landscape.html`) of sky, sun or moon and hills; `landscape.js` blends colour keyframes by the visitor's local time (a pastel set in light mode, a deep set in dark mode) and moves the sun by day and the moon by night along a low arc at fixed hours, so it glows through the panels rather than behind the clock. `folder`: photos in `HOMENCLATURE_BACKGROUND_DIR` (top level only; jpg, png, webp, avif; no dot files). `immich`: an album's previews, using the first Immich link with search in file order (its API URL and token, so no extra secret). Photos come from `/background` (logged in, `no-store`) in a shuffled round, each once before any repeats. The page loads one on open and crossfades to the next every `HOMENCLATURE_BACKGROUND_INTERVAL`, under a scrim in `--bg`. A photo that fails keeps the current one; the blobs show until the first loads.

## Env vars

| Var | Default |
| --- | --- |
| `HOMENCLATURE_CONFIG` | `config.toml` (Docker: `/config/config.toml`) |
| `HOMENCLATURE_LISTEN` | `:8000` |
| `HOMENCLATURE_URL` | required, public base URL; the callback is `/auth/callback` |
| `HOMENCLATURE_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` | required |
| `HOMENCLATURE_SESSION_SECRET` | required, at least 32 characters |
| `HOMENCLATURE_DEFAULT_DASHBOARD` | unset |
| `HOMENCLATURE_BACKGROUND` | `default`; or `landscape`, `folder`, `immich` |
| `HOMENCLATURE_BACKGROUND_INTERVAL` | `10m`, at least `1m`; time between photos |
| `HOMENCLATURE_BACKGROUND_DIR` | `backgrounds` (Docker: `/backgrounds`); for `folder` |
| `HOMENCLATURE_BACKGROUND_IMMICH_ALBUM` | required for `immich`: the album ID |
| app tokens for search | named per link by `token_env` in the config |
| app URLs, optional | named per link by `url_env`, `api_url_env`, `seerr_url_env` |

`mise run dev` sets dev values for all of these. `HOMENCLATURE_CONFIG` and the example config's search tokens and Immich URL can come from `.env` (gitignored, loaded by mise) instead.
- Images: `ghcr.io/niekkeijzer/homenclature` and `docker.io/niekkeijzer/homenclature`, for amd64 and arm64. `v*` tags publish semver tags + `latest`; `main` publishes `edge`. See `.github/workflows/ci.yml`. Docker Hub needs the `DOCKERHUB_USERNAME` repo variable and the `DOCKERHUB_TOKEN` repo secret.
- CI (`.github/workflows/ci.yml`): `mise run check` on every push to `main`, `v*` tag and pull request. Images are published only after it passes.
- Renovate (`.github/renovate.json5`): actions and images pinned to digests, npm packages pinned exactly. Minor, patch and digest updates automerge; majors request a review from NiekKeijzer. When an automerged update changes `go.mod`, `go.sum`, the `Dockerfile`, `package.json` or `package-lock.json`, CI publishes the next patch release (needs a first `v*` tag pushed by hand).

## Commands

```
mise install         # toolchain (go, node, jq, air)
mise run assets      # npm packages into web/static/lib (dev, lint and test run it first)
mise run oidc        # start mock OIDC (oidc:down to stop)
mise run dev         # live reload at http://localhost:8000 (air proxy; app on :8001, see dev/air.toml)
mise run check       # lint + test
mise run docker      # build the image locally
```

## Mock OIDC (local dev)

`compose.yaml` runs mock-oauth2-server. Its users are configured in `dev/oidc.json`.

- Issuer: `http://localhost:8080/default`. Any client ID and secret are accepted.
- At the login form, type a username:
  - `parent` gets groups `family`, `parents`
  - `kid` gets groups `family`, `kids`
  - any other name gets no groups. You can add claims in the form's JSON field, e.g. `{"groups":["guests"]}`.

## Hooks

After each edit to a `.go` file, `.claude/hooks/go-check.sh` runs gofmt and `go vet` on that package. Fix any vet errors it reports before moving on.
