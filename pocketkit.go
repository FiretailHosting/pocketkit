// Package pocketkit is an opinionated framework for PocketBase.
//
// It makes three decisions on your behalf and otherwise stays out of the way:
//
//   - Routes and hooks are discovered from the filesystem. You never write a
//     registration line; `pocketkit gen` reads api/ and hooks/ and wires them up.
//   - Auth is OIDC-only. Password login is disabled on every auth collection and
//     cannot be re-enabled from the dashboard.
//   - Routes require auth unless they say otherwise with `var Public = true`.
//
// Handlers, middlewares and hooks are plain PocketBase values. Anything you can
// write against stock PocketBase works here unchanged.
package pocketkit

import (
	"os"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

// Config holds the few things pocketkit lets you change.
type Config struct {
	// FrontendDir is the built frontend served at the site root.
	// Defaults to "frontend/build".
	FrontendDir string

	// AllowPasswords disables pocketkit's OIDC-only opinion. Off by default,
	// and you should need a good reason to turn it on.
	AllowPasswords bool

	// PublicByDefault inverts the auth default so routes are open unless they
	// declare otherwise. Off by default.
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

// AllowPasswords re-enables password login. See Config.AllowPasswords.
func AllowPasswords() Option {
	return func(c *Config) { c.AllowPasswords = true }
}

// PublicByDefault inverts the default auth requirement for routes.
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

			if len(r.Middlewares) > 0 {
				route.Bind(r.Middlewares...)
			}
			if a.requiresAuth(r) {
				route.Bind(apis.RequireAuth(a.cfg.AuthCollections...))
			}
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
func (a *App) serveFrontend(se *core.ServeEvent) {
	if a.cfg.FrontendDir == "" {
		return
	}
	if st, err := os.Stat(a.cfg.FrontendDir); err != nil || !st.IsDir() {
		se.App.Logger().Warn(
			"pocketkit: frontend directory not found; serving API only",
			"dir", a.cfg.FrontendDir,
		)
		return
	}
	se.Router.GET("/{path...}", apis.Static(os.DirFS(a.cfg.FrontendDir), true))
}
