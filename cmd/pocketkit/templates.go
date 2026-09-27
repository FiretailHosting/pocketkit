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

	// Committed migrations are the schema's source of truth. Blank-importing
	// them here is what makes a fresh clone rebuild the exact same database.
	_ "%s/migrations"
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
	app := pocketkit.New(pocketkit.WithFrontendFS(frontend))

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
    pocketkit dev

This regenerates route wiring, starts PocketBase on :8090, and runs the
SvelteKit dev server on :5173 with '/api' proxied to PocketBase.

## Sign-in

Auth is OIDC-only -- password login is disabled and cannot be re-enabled from
the dashboard. Point the OIDC provider at your Rauthy instance in the superuser
dashboard under **Collections > users > Options > OAuth2 > OpenID Connect**.

## Routes

    pocketkit routes

Add one by creating a file:

    api/things/GET.go        ->  GET  /api/things
    api/things/_id/GET.go    ->  GET  /api/things/{id}

## Types

    pocketkit types

Regenerates 'frontend/src/lib/pocketbase-types.ts' from the local database.
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
const POCKETBASE = 'http://127.0.0.1:8090';

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
import type { TypedPocketBase } from './pocketbase-types';

// Same-origin in production and, thanks to the Vite proxy, in development too.
export const pb = new PocketBase(window.location.origin) as TypedPocketBase;

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
      - name: Check generated wiring is current
        run: |
          go tool pocketkit gen
          git diff --exit-code pocketkit_gen.go

      - name: Build binaries
        env:
          CGO_ENABLED: '0'
          VERSION: ${{ github.ref_name }}
        run: |
          set -euo pipefail
          name=$(basename "$(go list -m)")
          mkdir -p dist

          # PocketBase uses a pure-Go SQLite driver, so every target
          # cross-compiles from this one runner with CGO off.
          for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
            os=${target%%/*}
            arch=${target#*/}
            echo "building $os/$arch"

            GOOS=$os GOARCH=$arch go build \
              -trimpath \
              -ldflags "-s -w -X github.com/FiretailHosting/pocketkit.Version=${VERSION}" \
              -o "dist/$name" .

            # The name must end in <os>_<arch>.tar.gz: that suffix is how the
            # update command picks the right asset for the machine it runs on.
            tar -czf "dist/${name}_${VERSION}_${os}_${arch}.tar.gz" -C dist "$name"
            rm "dist/$name"
          done

          cd dist && sha256sum *.tar.gz > checksums.txt
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
