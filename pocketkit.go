// Package pocketkit is an opinionated framework for PocketBase.
//
// It makes three decisions on your behalf and otherwise stays out of the way:
//
//   - Routes and hooks are discovered from the filesystem. You never write a
//     registration line; `pocketkit gen` reads api/ and hooks/ and wires them up.
//   - WithSSO enables OIDC sign-in for the configured auth collection.
//     Without it, existing authentication settings remain unchanged.
//   - Routes require auth unless the handler declares //pocketkit:public.
//
// Handlers, middlewares and hooks are plain PocketBase values. Anything you can
// write against stock PocketBase works here unchanged.
package pocketkit

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

// EmbeddedFrontendDir is the path an app's //go:embed directive is expected to
// capture. A frontend embedded at this path needs no configuration.
const EmbeddedFrontendDir = "frontend/build"

// Config holds the few things pocketkit lets you change.
type Config struct {
	// FrontendDir is the built frontend served at the site root when nothing is
	// embedded. Defaults to "frontend/build".
	FrontendDir string

	// FrontendFS is a frontend compiled into the binary. When it contains an
	// index.html it wins over FrontendDir, so a release is one file with no
	// directory to ship beside it.
	FrontendFS fs.FS

	// Slug overrides the "owner/repo" the update command pulls releases from.
	// Empty means derive it from the module path.
	Slug string

	// SSO configures OIDC sign-in. Nil leaves auth untouched.
	SSO *SSOConfig

	// AllowPasswords silences the warning that no SSO config was supplied.
	// Off by default, and you should need a good reason to turn it on.
	AllowPasswords bool

	// PublicByDefault disables automatic route authentication. Protect individual
	// routes with explicit auth middleware. Off by default.
	PublicByDefault bool

	// AuthCollections optionally restricts which collections satisfy the
	// default auth requirement. Empty means any auth collection.
	AuthCollections []string
}

// App is a PocketBase app with pocketkit's opinions applied. It embeds
// *pocketbase.PocketBase, so every native API remains available.
type App struct {
	*pocketbase.PocketBase
	cfg Config
}

// Option customises an App.
type Option func(*Config)

// WithFrontend sets the directory of the built frontend.
func WithFrontend(dir string) Option {
	return func(c *Config) { c.FrontendDir = dir }
}

// WithFrontendFS serves a frontend compiled into the binary.
//
// Pass the embed.FS directly; pocketkit looks inside it for
// EmbeddedFrontendDir and falls back to its root:
//
//	//go:embed all:frontend/build
//	var frontend embed.FS
//
//	pocketkit.New(pocketkit.WithFrontendFS(frontend))
//
// The all: prefix matters. SvelteKit emits its assets into _app, and a plain
// //go:embed skips paths beginning with an underscore.
func WithFrontendFS(fsys fs.FS) Option {
	return func(c *Config) { c.FrontendFS = fsys }
}

// WithUpdates overrides the "owner/repo" that `update` pulls releases from.
// By default it is derived from the app's module path.
func WithUpdates(slug string) Option {
	return func(c *Config) { c.Slug = slug }
}

// AllowPasswords acknowledges password login and suppresses the warning when
// no SSO policy is configured. It does not change authentication settings.
func AllowPasswords() Option {
	return func(c *Config) { c.AllowPasswords = true }
}

// PublicByDefault disables automatic route authentication. Protect individual
// routes with explicit auth middleware.
func PublicByDefault() Option {
	return func(c *Config) { c.PublicByDefault = true }
}

// AuthCollections restricts which collections satisfy the auth requirement.
func AuthCollections(names ...string) Option {
	return func(c *Config) { c.AuthCollections = names }
}

// New builds the app: migrations, auth policy, discovered hooks, discovered
// routes, and the frontend, in that order.
func New(opts ...Option) *App {
	cfg := Config{FrontendDir: "frontend/build"}
	for _, o := range opts {
		o(&cfg)
	}

	a := &App{PocketBase: pocketbase.New(), cfg: cfg}

	migratecmd.MustRegister(a.PocketBase, a.RootCmd, migratecmd.Config{
		// Schema changes made in the dashboard are written straight out as Go
		// migrations, so the committed migrations stay the source of truth.
		Automigrate: true,
	})

	a.bindAuthPolicy()
	a.bindHooks()
	a.bindRoutes()
	a.bindUpdateCommand()

	return a
}

// bindHooks attaches every hook found under hooks/.
func (a *App) bindHooks() {
	for _, h := range Hooks() {
		h.Bind(a.PocketBase)
	}
}

// bindRoutes attaches every route found under api/, then the frontend.
func (a *App) bindRoutes() {
	a.OnServe().BindFunc(func(se *core.ServeEvent) error {
		for _, r := range Routes() {
			route := se.Router.Route(r.Method, r.Path, r.Handler)

			if a.requiresAuth(r) {
				auth := apis.RequireAuth(a.cfg.AuthCollections...)
				// Keep explicit RequireAuth middleware independent of this guard.
				// PocketBase replaces handlers sharing an ID.
				auth.Id = "pocketkitRequireAuth"
				route.Bind(auth)
			}
			route.Bind(r.Middlewares...)
		}

		a.serveFrontend(se)

		return se.Next()
	})
}

// requiresAuth applies the default-deny opinion.
func (a *App) requiresAuth(r RouteDef) bool {
	if r.Public {
		return false
	}
	return !a.cfg.PublicByDefault
}

// serveFrontend serves the built frontend at the site root with SPA fallback.
// It is registered last so API routes always win.
//
// An embedded frontend wins over the directory: a release is then a single
// binary, while development still picks up whatever is on disk.
func (a *App) serveFrontend(se *core.ServeEvent) {
	if fsys, ok := a.embeddedFrontend(); ok {
		se.Router.GET("/{path...}", apis.Static(fsys, true))
		return
	}

	if a.cfg.FrontendDir == "" {
		return
	}
	// The directory alone is not enough: a scaffolded app keeps a placeholder
	// in frontend/build so //go:embed compiles, and serving that would mount a
	// catch-all that answers every unmatched path with "File not found"
	// instead of letting the API's own 404 through.
	if _, err := os.Stat(filepath.Join(a.cfg.FrontendDir, "index.html")); err != nil {
		se.App.Logger().Warn(
			"pocketkit: no frontend embedded and none built on disk; serving API only",
			"dir", a.cfg.FrontendDir,
		)
		return
	}
	se.Router.GET("/{path...}", apis.Static(os.DirFS(a.cfg.FrontendDir), true))
}

// embeddedFrontend returns the embedded site, if one was built in.
//
// A scaffolded app always embeds frontend/build, but that directory holds only
// a placeholder until the frontend is built -- so the presence of index.html,
// not of the FS, decides whether anything was really embedded.
func (a *App) embeddedFrontend() (fs.FS, bool) {
	if a.cfg.FrontendFS == nil {
		return nil, false
	}

	candidates := []fs.FS{}
	if sub, err := fs.Sub(a.cfg.FrontendFS, EmbeddedFrontendDir); err == nil {
		candidates = append(candidates, sub)
	}
	candidates = append(candidates, a.cfg.FrontendFS)

	for _, fsys := range candidates {
		if _, err := fs.Stat(fsys, "index.html"); err == nil {
			return fsys, true
		}
	}
	return nil, false
}
