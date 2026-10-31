# Homenclature

A household homepage: a simple frosted-glass ("liquid glass") page of links. Users log in with OIDC, and their groups decide which links they see. No database: all config comes from a TOML file and env vars.

## Working rules

- **When unsure about the task, scope or approach, stop and ask the dev.** Don't guess. Asking is always fine.
- Anything not listed under Decisions is open. Propose it and ask before building.
- Keep it small: stdlib first. Ask before adding a dependency.
- Done means `mise run check` passes. For UI or auth changes, also run the app and log in through the mock OIDC.
- Commit or push only when asked.

## Decisions

- Backend: Go (version in `mise.toml`). Single binary, `package main` at the repo root.
- Frontend: Go `html/template` + HTMX + plain CSS, embedded with `go:embed`. HTMX is vendored (no CDN). Glass look via `backdrop-filter`.
- Auth: OIDC. The prod IdP is Authentik, which sends group names in the `groups` claim.
- Config: the TOML file holds only dashboards (parsed with `BurntSushi/toml`). App settings (OIDC, server) and secrets come from env vars prefixed `HOMENCLATURE_`. No database, no persistent state.
- Dashboards (`config.example.toml`, `internal/config`): one per OIDC group, keyed by group name. `HOMENCLATURE_DEFAULT_DASHBOARD` names the dashboard for users who match none (unset: they see nothing; names a missing dashboard: startup error). A user in several groups gets one merged page: file order, same-title sections merged, links deduped by URL. Links are written inline in sections. Sections only have a title.
- Icons: `icon` is a dashboard-icons name or a URL. The server fetches icons and caches them in memory (not built yet).
- Images: `ghcr.io/niekkeijzer/homenclature` and `docker.io/niekkeijzer/homenclature`, for amd64 and arm64. `v*` tags publish semver tags + `latest`; `main` publishes `edge`. See `.github/workflows/docker.yml`. Docker Hub needs the `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repo secrets.

## Commands

```
mise install         # toolchain (go, jq)
mise run oidc        # start mock OIDC (oidc:down to stop)
mise run dev         # run the app
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
