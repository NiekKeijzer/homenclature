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

- Backend: Go (version in `mise.toml`). Single binary. Keep Go code out of the repo root: `cmd/homenclature` (main), `internal/server` (HTTP, auth, icons; templates in `internal/server/web`), `internal/config`.
- Frontend: Go `html/template` + HTMX + plain CSS, embedded with `go:embed`. HTMX is vendored (no CDN). Glass look via `backdrop-filter`.
- Auth: OIDC. The prod IdP is Authentik, which sends group names in the `groups` claim.
- Config: the TOML file holds only dashboards (parsed with `BurntSushi/toml`). App settings (OIDC, server) and secrets come from env vars prefixed `HOMENCLATURE_`. No database, no persistent state.
- Dashboards (`config.example.toml`, `internal/config`): one per OIDC group, keyed by group name. `HOMENCLATURE_DEFAULT_DASHBOARD` names the dashboard for users who match none (unset: they see nothing; names a missing dashboard: startup error). A user in several groups gets one merged page: file order, same-title sections merged, links deduped by URL. Links are written inline in sections. Sections have a title and an optional `layout`: `list` (default) or `cards`; when sections merge, the first layout set wins.
- Icons: `icon` is a dashboard-icons name or a URL. `/icons/{hash}` fetches it (SVG, then PNG fallback) and caches it in memory. Only icons from the config can be fetched.
- Auth: `coreos/go-oidc/v3` + `golang.org/x/oauth2`, with PKCE, state and nonce. Groups come from the `groups` claim, scopes `openid profile email`.
- Sessions: AES-GCM encrypted cookie (stdlib), key from `HOMENCLATURE_SESSION_SECRET`, valid for 24h. No logout button.
- Page (look "D: search first", see the mockups canvas): a clock with the date and greeting (first name), computed in the browser; a search pill that filters links client-side (`/` focuses, ↑/↓ select, Enter opens, Tab cycles the app chips, Esc clears); while it's focused or has a query the clock shrinks beside the date, animated FLIP-style in `app.js`; sections as glass panels (cards sections get a full row; neighbouring list sections share a row when there's room). Dark with glow blobs by default, a pastel light version when the system is light. Sora (OFL, self-hosted in `web/static/fonts`) for the clock and headings, system fonts otherwise. JS is in `web/static/app.js`. HTMX 2 is vendored in `web/static/htmx.min.js` and only loaded when the user can search an app.
- Search inside apps (`internal/search`, `internal/server/search.go`): opt in per link with `search = { type, token_env, api_url?, limit?, ... }`; types home-assistant (`domains`), immich, jellyfin (`types`, `seerr_url`), mealie, paperless, seerr. Seerr lists matching films and shows, skipping available and blocklisted ones, and links to them in Seerr, where people request as themselves; its posters come from TMDB's image CDN, without the key. The token comes from the env var named by `token_env`; unset is a startup error. Read-only: results deep-link into the app. After 2+ characters and a 300ms pause, HTMX loads `/search`: one glass section per app below the links (Immich as a photo grid), with chips (swapped out of band) to filter by app, and a keyboard hint bar. Only apps on the user's dashboard are searched, in parallel with a 3s timeout, using the configured token. Apps with no results are hidden, and so are apps whose search failed (logged); Jellyfin's section ends with a link to its own search, plus one to Seerr's search (`seerr_url`) for users without Seerr search on their dashboard (decided in `internal/server/search.go`). Thumbnails go through `/search/thumb/{key}/{id}`, with the ID validated per app.

## Env vars

| Var | Default |
| --- | --- |
| `HOMENCLATURE_CONFIG` | `config.toml` (Docker: `/config/config.toml`) |
| `HOMENCLATURE_LISTEN` | `:8000` |
| `HOMENCLATURE_URL` | required, public base URL; the callback is `/auth/callback` |
| `HOMENCLATURE_OIDC_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET` | required |
| `HOMENCLATURE_SESSION_SECRET` | required, at least 32 characters |
| `HOMENCLATURE_DEFAULT_DASHBOARD` | unset |
| app tokens for search | named per link by `token_env` in the config |

`mise run dev` sets dev values for all of these.
- Images: `ghcr.io/niekkeijzer/homenclature` and `docker.io/niekkeijzer/homenclature`, for amd64 and arm64. `v*` tags publish semver tags + `latest`; `main` publishes `edge`. See `.github/workflows/ci.yml`. Docker Hub needs the `DOCKERHUB_USERNAME` repo variable and the `DOCKERHUB_TOKEN` repo secret.
- CI (`.github/workflows/ci.yml`): `mise run check` on every push to `main`, `v*` tag and pull request. Images are published only after it passes.
- Renovate (`.github/renovate.json5`): actions and images pinned to digests. Minor, patch and digest updates automerge; majors request a review from NiekKeijzer. When an automerged update changes `go.mod`, `go.sum` or the `Dockerfile`, CI publishes the next patch release (needs a first `v*` tag pushed by hand).

## Commands

```
mise install         # toolchain (go, jq, air)
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
