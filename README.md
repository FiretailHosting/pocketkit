# pocketkit

An opinionated framework for PocketBase.

PocketBase is already excellent. pocketkit does not wrap it, hide it, or
reinvent it — handlers, middlewares and hooks are stock PocketBase values, and
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

**3. Auth is OIDC-only.** Password login is disabled on every auth collection at
boot and re-disabled on every save, so it cannot be clicked back on in the
dashboard. Point the OIDC provider at your [Rauthy](https://github.com/sebadob/rauthy)
instance under **Collections > users > Options > OAuth2 > OpenID Connect**.
Credentials live in the dashboard, never in the repo.

`_superusers` deliberately keeps its password. The dashboard is where OIDC gets
configured, so locking it behind OIDC would leave nobody able to set OIDC up.

**4. Hooks are files too.** A directory named after a PocketBase hook binds to it.
A directory above it scopes it to a collection.

```
hooks/OnBootstrap/hook.go                       app.OnBootstrap()
hooks/coins/OnRecordAfterCreateSuccess/hook.go  app.OnRecordAfterCreateSuccess("coins")
```

pocketkit never inspects hook signatures — the generated call is plain Go, so
the compiler checks them for you and every PocketBase hook works automatically.

**5. Committed migrations are the schema.** Build collections in the dashboard;
PocketBase writes them out as Go migrations, and you commit those. `pb_data/` is
disposable — a fresh clone rebuilds the exact same database.

**6. One origin, always.** SvelteKit builds to `frontend/build` as a static SPA
and PocketBase serves it at the site root. The dev server proxies `/api` and
`/_` to PocketBase, so the frontend talks to the same origin in development and
in production, and auth cookies behave identically in both.

**7. Types are generated, not written.** `pocketkit types` runs a pinned
[pocketbase-typegen](https://github.com/patmood/pocketbase-typegen) against the
local database and writes `frontend/src/lib/pocketbase-types.ts`.

## Install

```
go install github.com/Ovi1kanobe/pocketkit/cmd/pocketkit@latest
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

`pocketkit check` exists because `go vet ./...` silently skips directories
starting with `_`, which is how path parameters are spelled. It passes those
packages explicitly so nothing goes unchecked.

## Reserved paths

PocketBase serves its own routes under `/api`. A route file that lands on one
compiles fine and then panics at startup, so `pocketkit gen` refuses first:

```
backups, batch, collections, crons, files, health, logs, realtime, settings, sql
```

Only the first segment is reserved — `/api/coins/health` is fine.
