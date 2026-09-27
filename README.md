# pocketkit

An opinionated framework for PocketBase.

PocketBase is already excellent. pocketkit does not wrap it, hide it, or
reinvent it - handlers, middlewares and hooks are stock PocketBase values, and
anything you can write against plain PocketBase works here unchanged.

What pocketkit removes is the bookkeeping: you never write a registration line.

```go
// api/coins/_id/GET.go  ->  GET /api/coins/{id}
package coin

import "github.com/pocketbase/pocketbase/core"

func GET(e *core.RequestEvent) error {
	return e.JSON(200, e.Request.PathValue("id"))
}
```

That file is the whole thing. No `OnServe`, no `se.Router.GET`, no import to add.

The handler is named after its method. `GET.go` and `POST.go` in one directory
are one Go package, so a shared name like `Handle` would not compile.

## The opinions

**1. The filesystem is the router.** A directory under `api/` is a URL path and
the file name is the HTTP method. `pocketkit gen` reads the tree and writes the
wiring; `pocketkit dev` does it on every save.

```
api/coins/GET.go            GET    /api/coins
api/coins/POST.go           POST   /api/coins
api/coins/_id/GET.go        GET    /api/coins/{id}
api/coins/_id/chart/GET.go  GET    /api/coins/{id}/chart
api/files/_path_/GET.go     GET    /api/files/{path...}
```

A leading underscore marks a path parameter. Underscores on both sides mark a
wildcard. Go forbids `{`, `[`, `-` and `$` in import paths, so `_id` is the only
spelling of this that Go itself will actually compile.

**2. Routes require auth. Say so to opt out.** Every route needs a signed-in
user unless its handler carries a `//pocketkit:public` directive:

```go
//pocketkit:public
func GET(e *core.RequestEvent) error { ... }
```

Forgetting to protect a route is a security bug; forgetting to open one is a 401
you notice immediately. A mistyped directive fails the same safe way. Per-route
middleware goes in a `GETMiddlewares` slice, named after its method for the same
reason the handler is.

**3. Auth is OIDC-only, through Rauthy.** Sign-in goes through
[pocketbase-rauthy](https://github.com/FiretailHosting/pocketbase-rauthy):

```go
pocketkit.New(pocketkit.WithRauthy(pocketkit.RauthyConfig{
    RequiredGroup: "myapp-users",
    SessionMaxAge: 12 * time.Hour,
}))
```

Password and OTP login are off, accounts are created only through the OAuth2
flow, and membership of `RequiredGroup` with a verified email is required.

`SessionMaxAge` is the part worth understanding. PocketBase auth tokens last
days, so disabling passwords is not enough on its own: without a session cap, a
user removed from the Rauthy group keeps working access until their existing
token expires. The session is therefore capped on every request, not only on
refresh, and stale realtime connections are closed too, since HTTP middleware
cannot reach a connection that is already open.

Point the provider at your Rauthy instance in the dashboard under
**Collections > users > Options > OAuth2 > OpenID Connect**. Credentials live
there, never in the repo. `_superusers` keeps its password: the dashboard is
where OIDC gets configured, so locking it behind OIDC would leave nobody able
to set it up.

Two things catch people out when wiring this up.

**Add `groups` to the client's default scopes in Rauthy.**
PocketBase's OIDC provider requests `openid`, `email` and `profile`, and that list is not configurable.
It never asks for `groups`, so Rauthy sends the claim only if the client emits it unasked.
This is a per-client setting, separate from the global scope list, which is already marked default and does not affect it.
Miss it and every sign-in fails with "Your account is not in the ... group", which points at membership rather than at the absent claim.

**The redirect URI follows the origin you open, not the server's address.**
The JS SDK builds it from its own base URL, which the scaffold sets to `window.location.origin`.
Opening the binary directly gives `http://127.0.0.1:8090/api/oauth2-redirect`, while the dev server gives `http://localhost:5173/api/oauth2-redirect`.
Register whichever you use, or both.

**4. Hooks are files too.** A directory named after a PocketBase hook binds to it.
A directory above it scopes it to a collection.

```
hooks/OnBootstrap/hook.go                       app.OnBootstrap()
hooks/coins/OnRecordAfterCreateSuccess/hook.go  app.OnRecordAfterCreateSuccess("coins")
```

pocketkit never inspects hook signatures - the generated call is plain Go, so
the compiler checks them for you and every PocketBase hook works automatically.

**5. Committed migrations are the schema.** Build collections in the dashboard;
PocketBase writes them out as Go migrations, and you commit those. `pb_data/` is
disposable - a fresh clone rebuilds the exact same database.

**6. One origin, always - and in production, one file.** SvelteKit builds to
`frontend/build` as a static SPA and PocketBase serves it at the site root. The
dev server proxies `/api` and `/_` to PocketBase, so the frontend talks to the
same origin in development and in production, and auth cookies behave
identically in both.

For release builds the site is compiled into the binary:

```go
//go:embed all:frontend/build
var frontend embed.FS

app := pocketkit.New(pocketkit.WithFrontendFS(frontend))
```

An embedded site wins over the directory, so deploying is copying one file,
while development still picks up whatever the dev server just wrote. The `all:`
prefix is not optional - SvelteKit emits into `_app`, and a plain `//go:embed`
skips paths beginning with an underscore.

**8. Apps update themselves.** Every pocketkit app gets `update` and `version`
for free:

```
myapp update            # install the latest release
myapp update --check    # just say whether one exists
myapp version
```

It reads the repository from the app's own module path, downloads the asset
matching the current OS and architecture, and verifies it against the release's
`checksums.txt` before replacing anything. Private repositories work - pass
`--token` or set `GITHUB_TOKEN`, and assets are fetched through the GitHub API
rather than a public download URL.

`pocketkit new` also writes a release workflow that fires only on a `v*` tag,
builds the frontend once, and cross-compiles linux and darwin on amd64 and
arm64 from a single runner. PocketBase's SQLite driver is pure Go, so `CGO` stays
off and no per-platform runners are needed.

**7. Types are generated, not written.** `pocketkit types` runs a pinned
[pocketbase-typegen](https://github.com/patmood/pocketbase-typegen) against the
local database and writes `frontend/src/lib/pocketbase-types.ts`.

## Install

```
go install github.com/FiretailHosting/pocketkit/cmd/pocketkit@latest
```

## Start an app

```
pocketkit new github.com/you/myapp
cd myapp
go mod tidy
pocketkit dev
```

This scaffolds the Go app and a SvelteKit frontend wired to the opinions above,
then runs PocketBase on `:8090` and the frontend dev server on `:5173`.

## Commands

| Command | Does |
| --- | --- |
| `pocketkit new <module>` | Scaffold an app |
| `pocketkit gen` | Regenerate route and hook wiring |
| `pocketkit routes` | List what was discovered |
| `pocketkit dev` | Regenerate, run, restart on change |
| `pocketkit types` | Generate frontend TypeScript types |
| `pocketkit check` | Vet every package, including `_`-prefixed route dirs |

And in every app you build with it:

| Command | Does |
| --- | --- |
| `myapp update` | Replace this binary with the latest release |
| `myapp update --check` | Report whether an update exists |
| `myapp version` | Print version, OS and architecture |

`pocketkit check` exists because `go vet ./...` silently skips directories
starting with `_`, which is how path parameters are spelled. It passes those
packages explicitly so nothing goes unchecked.

## Reserved paths

PocketBase serves its own routes under `/api`. A route file that lands on one
compiles fine and then panics at startup, so `pocketkit gen` refuses first:

```
backups, batch, collections, crons, files, health, logs, realtime, settings, sql
```

Only the first segment is reserved - `/api/coins/health` is fine.

## Generated files

`pocketkit_gen.go` is committed, not ignored: a fresh clone then builds with
plain `go build`, and CI checks it is current with

```
go tool pocketkit gen && git diff --exit-code pocketkit_gen.go
```

The `tool` directive in an app's `go.mod` pins the CLI to the same version as
the library, so nobody has to install it separately.
