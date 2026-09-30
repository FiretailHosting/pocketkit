package pocketkit

import (
	"fmt"

	sso "github.com/FiretailHosting/pocketbase-sso"
	"github.com/pocketbase/pocketbase/core"
)

// SSOConfig configures OIDC sign-in. It is pocketbase-sso's config,
// re-exported so apps need not import the package directly for the common case.
type SSOConfig = sso.Config

// WithSSO enables OIDC sign-in with the configured provider.
//
// RequiredGroup and SessionMaxAge have no safe defaults and must be set:
//
//	pocketkit.New(pocketkit.WithSSO(pocketkit.SSOConfig{
//	    RequiredGroup: "solscope-users",
//	    SessionMaxAge: 12 * time.Hour,
//	}))
//
// Everything else defaults: the users collection, the oidc provider, and the
// sso_login_at field.
// Pair this option with AuthCollections("users") to restrict application routes
// to that collection; WithSSO alone does not restrict the route auth guard.
func WithSSO(config SSOConfig) Option {
	return func(c *Config) { c.SSO = &config }
}

// MigrateSSO applies the collection settings required by WithSSO.
// Call it from an application migration using the same config.
func MigrateSSO(app core.App, config SSOConfig) error {
	return sso.Migrate(app, config)
}

// bindAuthPolicy wires OIDC sign-in into the app.
//
// pocketkit does not implement this itself. Disabling password login is the
// easy half; the half that matters is that PocketBase auth tokens last days, so
// without a session cap a user removed from the required group keeps working
// access until their existing token expires. pocketbase-sso caps the session
// on every request and closes stale realtime connections, which HTTP middleware
// cannot reach.
func (a *App) bindAuthPolicy() {
	if a.cfg.SSO == nil {
		if !a.cfg.AllowPasswords {
			a.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
				if err := e.Next(); err != nil {
					return err
				}
				e.App.Logger().Warn(
					"pocketkit: no auth configured; password login is still enabled. " +
						"Pass pocketkit.WithSSO(...) to use OIDC sign-in.")
				return nil
			})
		}
		return
	}

	if err := sso.Register(a.PocketBase, *a.cfg.SSO); err != nil {
		// A misconfigured auth policy must not start as an open app.
		panic(fmt.Sprintf("pocketkit: %v", err))
	}
}
