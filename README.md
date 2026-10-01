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

The middleware slice may live in any ordinary Go file in the same package.
At the default priority, authentication runs first and explicit `apis.RequireAuth("staff")` middleware adds a collection restriction.
A negative middleware priority explicitly runs before the auth guard and must not serve protected content.
`PublicByDefault()` disables the automatic guard; protect individual routes with explicit auth middleware.

**3. Auth is OIDC-only.** Sign-in uses
[pocketbase-sso](https://github.com/FiretailHosting/pocketbase-sso):

```go
pocketkit.New(
    pocketkit.WithSSO(pocketkit.SSOConfig{
        Collection: "users",
        RequiredGroup: "myapp-users",
        SessionMaxAge: 12 * time.Hour,
    }),
    pocketkit.AuthCollections("users"),
)
```

The scaffold's SSO migration disables password and OTP login for its auth collection.
Sign-in requires membership of `RequiredGroup` and a verified email from the OIDC provider; ordinary clients can create accounts only through the OAuth2 flow.
When integrating an existing app, pair `WithSSO` with a migration calling `pocketkit.MigrateSSO` with the same config.
`WithSSO` does not restrict which auth collections can call protected routes; without `AuthCollections`, any auth record passes, including superusers.
Set `AuthCollections` to the SSO collection to restrict them, as the scaffold does.
Authentication does not replace record-level or tenant authorization in custom handlers.

Historical migrations snapshot their settings.
Changes to the runtime SSO collection or login field need a new migration; do not edit an applied migration.

Without `WithSSO`, existing authentication settings remain unchanged and pocketkit logs a warning.
`AllowPasswords()` acknowledges that choice and silences the warning; it does not change login settings.

`SessionMaxAge` is the part worth understanding. PocketBase auth tokens last
days, so disabling passwords is not enough on its own: without a session cap, a
user removed from the required group keeps working access until their existing
token expires. The session is therefore capped on every request, not only on
refresh, and stale realtime connections are closed too, since HTTP middleware
cannot reach a connection that is already open.

Configure the OIDC provider in the dashboard under
**Collections > users > Options > OAuth2 > OpenID Connect**. Credentials live
there, never in the repo. `_superusers` keeps its password: the dashboard is
where OIDC gets configured, so locking it behind OIDC would leave nobody able
to set it up.

The provider must supply a `groups` claim and a verified email.
Two settings catch people out when wiring this up.

**Ensure the provider sends `groups` with the identity data.**
PocketBase's OIDC provider requests `openid`, `email` and `profile`, and that list is not configurable.
It never asks for `groups`, so configure the provider to include the claim without an explicit request for that scope.
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

**7. Apps update themselves.** Every pocketkit app gets `update` and `version`
for free:

```
myapp update            # install the latest release
myapp update --check    # just say whether one exists
myapp version
```

Self-update supports Linux amd64 and arm64.
It reads the repository from the app's own module path, downloads the matching `<name>-linux-<arch>` binary, and verifies it against the release's `checksums.txt` before replacing anything.
Private repositories work - pass
`--token` or set `GITHUB_TOKEN`, and assets are fetched through the GitHub API
rather than a public download URL.

`pocketkit new` also writes a release workflow that fires only on a `v*` tag, builds the frontend once, and cross-compiles Linux amd64 and arm64 from a single runner.
PocketBase's SQLite driver is pure Go, so `CGO` stays off and no per-platform runners are needed.

**8. Types are generated, not written.** `pocketkit types` runs a pinned
[pocketbase-typegen](https://github.com/patmood/pocketbase-typegen) against the
local database and writes `frontend/src/lib/pocketbase-types.ts`.

Complete the typed-client setup after the first boot using the [start an app](#start-an-app) steps below.

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
`pocketkit dev --http 127.0.0.1:9090` updates both the backend address and the
frontend proxy. Existing apps should read `process.env.POCKETKIT_BACKEND_URL`
in their Vite proxy configuration, with `http://127.0.0.1:8090` as the fallback.
If scaffolding fails, the command exits unsuccessfully and retains its partial
output for diagnosis.

Once PocketBase has started and applied migrations, generate types in a second terminal.
The dev server can keep running, since type generation only reads the database.

```
pocketkit types
```

In `frontend/src/lib/pb.ts`, add the type import and replace the client declaration, keeping the sign-in helper:

```ts
import PocketBase from 'pocketbase';
import type { TypedPocketBase } from './pocketbase-types';

export const pb = new PocketBase(window.location.origin) as TypedPocketBase;
```

Commit `frontend/src/lib/pocketbase-types.ts` and the updated `pb.ts`.
After schema changes, apply migrations locally and rerun `pocketkit types`; commit the regenerated types with the migrations.
The initial client works before this setup, but collection access is untyped until it is complete.

## Commands

| Command | Does |
| --- | --- |
| `pocketkit new <module>` | Scaffold an app |
| `pocketkit gen` | Regenerate route and hook wiring |
| `pocketkit routes` | List what was discovered |
| `pocketkit dev` | Regenerate, run, restart on change |
| `pocketkit types` | Generate frontend TypeScript types |
| `pocketkit check` | Vet and test packages, including `_`-prefixed route dirs |

And in every app you build with it:

| Command | Does |
| --- | --- |
| `myapp update` | Replace this binary with the latest release |
| `myapp update --check` | Report whether an update exists |
| `myapp version` | Print version, OS and architecture |

`pocketkit check` regenerates wiring, runs `go vet`, and runs `go test`.
Go's `./...` pattern skips directories starting with `_`, which is how path parameters are spelled.
Discovered route and hook packages are passed explicitly so their tests run too.
Tests use the race detector when CGO is enabled, which also needs a C compiler; without CGO, check warns and runs them without it.

New apps include PR and release validation that runs `go tool pocketkit check`
and rejects stale committed wiring. Existing apps should add these steps to CI;
upgrading the library does not rewrite application workflows.

## Reserved paths

PocketBase serves its own routes under `/api`. A route file that lands on one
compiles fine and then panics at startup, so `pocketkit gen` refuses first:

```
backups, batch, collections, crons, files, health, logs, oauth2-redirect, realtime, settings, sql
```

Only the first segment is reserved - `/api/coins/health` is fine.

## Generated files

`pocketkit_gen.go` is committed, not ignored: a fresh clone then builds with
plain `go build`, and CI checks it is current with

```
go tool pocketkit check && git diff --exit-code pocketkit_gen.go
```

The `tool` directive in an app's `go.mod` pins the CLI to the same version as
the library, so nobody has to install it separately.
