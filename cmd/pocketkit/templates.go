package main

const tmplGoMod = `module %s

go 1.25

// Lets CI and collaborators run the pocketkit CLI at the version this app
// pins, with "go tool pocketkit", instead of installing it separately.
tool github.com/FiretailHosting/pocketkit/cmd/pocketkit
`

const tmplMain = `package main

import (
	"embed"
	"log"

	"github.com/FiretailHosting/pocketkit"

	"%[1]s/internal/auth"

	// Committed migrations are the schema's source of truth. Blank-importing
	// them here is what makes a fresh clone rebuild the exact same database.
	_ "%[1]s/migrations"
)

// frontend is the built site, compiled into the binary so a release is one
// file. The all: prefix is required: SvelteKit puts its assets in _app, and a
// plain //go:embed skips paths that begin with an underscore.
//
// Until the frontend is built the directory holds only a placeholder, and
// pocketkit falls back to serving frontend/build from disk.
//
//go:embed all:frontend/build
var frontend embed.FS

func main() {
	app := pocketkit.New(
		pocketkit.WithFrontendFS(frontend),
		pocketkit.WithSSO(auth.SSO),
		pocketkit.AuthCollections(auth.SSO.Collection),
	)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
`

const tmplPing = `// Package ping answers GET /api/ping.
//
// The directory decides the URL and the file name decides the method, so this
// file needs no registration anywhere. The handler is named after its method,
// because every method file in a directory shares one Go package. Its
// signature is stock PocketBase.
package ping

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// The directive below opts this route out of pocketkit's default auth
// requirement. Remove it and the route requires a signed-in user.
//
//pocketkit:public
func GET(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
}
`

const tmplMigrationsDoc = `// Package migrations holds this app's schema history.
//
// Schema changes made in the superuser dashboard are written here automatically
// as Go migrations. Commit them: they, not pb_data, are the source of truth.
package migrations
`

const tmplGitignore = `# PocketBase runtime data. The schema lives in migrations/, not here.
pb_data/

# The built frontend is generated, but the directory itself must exist for
# //go:embed to compile in a fresh clone -- hence the placeholder.
frontend/build/*
!frontend/build/.gitkeep
frontend/.svelte-kit/
node_modules/

# Release binaries.
dist/

# pocketkit_gen.go is deliberately NOT ignored. Committing it keeps a fresh
# clone buildable with plain go build, and CI checks that it is up to date.

.DS_Store
.env
.env.*
!.env.example
`

const tmplReadme = "# %s\n\n" + `Built with [pocketkit](https://github.com/FiretailHosting/pocketkit).

## Develop

    go mod tidy
    go tool pocketkit dev

This regenerates route wiring, starts PocketBase on :8090, and runs the
SvelteKit dev server on :5173 with '/api' proxied to PocketBase.

Use 'go tool pocketkit dev --http 127.0.0.1:9090' to change the backend port;
the frontend proxy follows it automatically.

## Validate

    go tool pocketkit check

This regenerates wiring, vets packages, and runs tests with the race detector,
including parameter-route packages skipped by 'go test ./...'. It requires CGO
and a C compiler on a platform supported by Go's race detector.
Commit regenerated wiring. PR and release workflows reject stale wiring and
failed checks.

## Sign-in

The scaffold's migration disables password and OTP login for the users collection.
The configured SSO hooks enforce group membership and session age.
Protected routes accept only the configured users collection. Custom handlers
must still enforce record-level and tenant authorization.
Changes to the SSO collection or login field require a new migration;
historical migrations keep their original settings.
Configure an OIDC provider that supplies a verified email and groups claim in
the superuser dashboard under **Collections > users > Options > OAuth2 > OpenID Connect**.

## Routes

    pocketkit routes

Add one by creating a file:

    api/things/GET.go        ->  GET  /api/things
    api/things/_id/GET.go    ->  GET  /api/things/{id}

## Types

After the first boot has applied migrations, generate the client types in a second terminal.
The dev server can keep running, since type generation only reads the database.

    pocketkit types

In 'frontend/src/lib/pb.ts', add the type import and replace the client declaration, keeping the sign-in helper:

    import PocketBase from 'pocketbase';
    import type { TypedPocketBase } from './pocketbase-types';

    export const pb = new PocketBase(window.location.origin) as TypedPocketBase;

Commit 'frontend/src/lib/pocketbase-types.ts' and the updated 'pb.ts'.
After schema changes, apply migrations locally and rerun 'pocketkit types'; commit the regenerated types with the migrations.
The initial client works before this setup, but collection access is untyped until it is complete.
`

const tmplSvelteConfig = `import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
export default {
	preprocess: vitePreprocess(),
	kit: {
		// PocketBase serves this directory. The SPA fallback lets PocketBase
		// hand any unmatched path to the client router.
		adapter: adapter({
			pages: 'build',
			assets: 'build',
			fallback: 'index.html',
			precompress: false,
			strict: false
		})
	}
};
`

const tmplViteConfig = `import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// In production PocketBase serves the built frontend, so the API is same-origin.
// The dev proxy reproduces that, which keeps auth cookies working in both.
const POCKETBASE = process.env.POCKETKIT_BACKEND_URL ?? 'http://127.0.0.1:8090';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/api': { target: POCKETBASE, changeOrigin: true },
			'/_': { target: POCKETBASE, changeOrigin: true }
		}
	}
});
`

const tmplLayoutTS = `// pocketkit ships a single-page app: PocketBase serves static files and the
// client talks to the API directly, so there is no server to render on.
export const ssr = false;
export const prerender = false;
`

const tmplPBClient = `import PocketBase from 'pocketbase';

// Same-origin in production and, thanks to the Vite proxy, in development too.
// Complete the typed-client setup in README.md after the first boot:
// run pocketkit types, import TypedPocketBase from './pocketbase-types',
// and cast this client to it.
export const pb = new PocketBase(window.location.origin);

// Auth is OIDC-only. There is deliberately no password sign-in helper here.
export function signIn(collection = 'users') {
	return pb.collection(collection).authWithOAuth2({ provider: 'oidc' });
}

export function signOut() {
	pb.authStore.clear();
}
`

const tmplGitkeep = `Generated by the frontend build. This placeholder keeps the directory present
so //go:embed compiles before the first build.
`

const tmplReleaseWorkflow = `name: release

# Only on a new version. Ordinary pushes build nothing.
on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v7

      - uses: oven-sh/setup-bun@v2

      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
          cache: true

      # The frontend is embedded into every binary, so it is built once here
      # rather than per target.
      - name: Build frontend
        working-directory: frontend
        run: |
          bun install --frozen-lockfile
          bun run build

      # A stale pocketkit_gen.go would silently ship missing routes.
      - name: Validate Go and generated wiring
        run: |
          go tool pocketkit check
          git diff --exit-code pocketkit_gen.go

      - name: Build binaries
        env:
          CGO_ENABLED: '0'
          VERSION: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          name=$(basename "$(go list -m)")
          mkdir -p dist

          # Linux only, and raw binaries rather than archives: that is the
          # layout FiretailHosting/go-selfupdate expects. The asset name must
          # be exactly <name>-linux-<arch>, and the binary must print its bare
          # X.Y.Z for --version, which the updater checks after downloading.
          # PocketBase'"'"'s SQLite driver is pure Go, so both arches cross-compile
          # from this one runner with CGO off.
          for arch in amd64 arm64; do
            echo "building linux/$arch"
            GOOS=linux GOARCH=$arch go build \
              -trimpath \
              -ldflags "-s -w -X github.com/FiretailHosting/pocketkit.Version=${VERSION}" \
              -o "dist/${name}-linux-${arch}" .
          done

          # sha256sum'"'"'s "<hex>  <name>" format is what the updater parses.
          cd dist && sha256sum "${name}"-linux-* > checksums.txt
          cat checksums.txt

      - name: Publish release
        env:
          GH_TOKEN: ${{ github.token }}
          VERSION: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          if gh release view "$VERSION" >/dev/null 2>&1; then
            gh release upload "$VERSION" dist/* --clobber
          else
            gh release create "$VERSION" \
              --title "$VERSION" \
              --generate-notes \
              dist/*
          fi
`

const tmplAuthConfig = `// Package auth holds this app's SSO sign-in settings.
//
// Historical migrations snapshot their own settings. Changes to Collection or
// LoginField need a new migration so fresh and upgraded databases agree.
package auth

import (
	"time"

	"github.com/FiretailHosting/pocketkit"
)

// SSO is this app's sign-in policy.
//
// RequiredGroup must match a real group from your OIDC provider; until it does,
// nobody can sign in, which is the correct failure for an OIDC-only app.
var SSO = pocketkit.SSOConfig{
	Collection:    "users",
	RequiredGroup: "%s-users",

	// How long a session survives after the last OIDC sign-in. This is the
	// window in which someone removed from the group still has access, so keep
	// it short rather than matching PocketBase's multi-day token lifetime.
	SessionMaxAge: 12 * time.Hour,
}
`

const tmplAuthMigration = `package migrations

import (
	"time"

	"github.com/FiretailHosting/pocketkit"
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Applies the SSO sign-in policy to the users collection: password and OTP
// off, OAuth2 on, account creation restricted to the OAuth2 flow, and the
// server-managed sso_login_at field the session cap reads.
func init() {
	m.Register(func(app core.App) error {
		// Keep this snapshot independent of mutable runtime configuration.
		// Apply later schema changes in a new migration.
		return pocketkit.MigrateSSO(app, pocketkit.SSOConfig{
			Collection:    "users",
			Provider:      "oidc",
			LoginField:    "sso_login_at",
			RequiredGroup: "%s-users",
			SessionMaxAge: 12 * time.Hour,
		})
	}, nil)
}
`

const tmplCheckWorkflow = `name: check

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
          cache: true
      - name: Validate Go and generated wiring
        run: |
          go tool pocketkit check
          git diff --exit-code pocketkit_gen.go
`
