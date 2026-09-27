package pocketkit

import (
	"fmt"

	sso "github.com/FiretailHosting/pocketbase-sso"
	"github.com/pocketbase/pocketbase/core"
)

// RauthyConfig configures Rauthy sign-in. It is pocketbase-sso's config,
// re-exported so apps need not import the package directly for the common case.
type RauthyConfig = sso.Config

// WithRauthy enables Rauthy (OIDC) sign-in.
//
// RequiredGroup and SessionMaxAge have no safe defaults and must be set:
//
//	pocketkit.New(pocketkit.WithRauthy(pocketkit.RauthyConfig{
//	    RequiredGroup: "solscope-users",
//	    SessionMaxAge: 12 * time.Hour,
//	}))
//
// Everything else defaults: the users collection, the oidc provider, and the
// sso_login_at field.
func WithRauthy(config RauthyConfig) Option {
	return func(c *Config) { c.Rauthy = &config }
}

// MigrateRauthy applies the collection settings required by WithRauthy.
// Call it from an application migration using the same config.
func MigrateRauthy(app core.App, config RauthyConfig) error {
	return sso.Migrate(app, config)
}

// bindAuthPolicy wires Rauthy sign-in into the app.
//
// pocketkit does not implement this itself. Disabling password login is the
// easy half; the half that matters is that PocketBase auth tokens last days, so
// without a session cap a user removed from the Rauthy group keeps working
// access until their existing token expires. pocketbase-sso caps the session
// on every request and closes stale realtime connections, which HTTP middleware
// cannot reach.
func (a *App) bindAuthPolicy() {
	if a.cfg.Rauthy == nil {
		if !a.cfg.AllowPasswords {
			a.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
				if err := e.Next(); err != nil {
					return err
				}
				e.App.Logger().Warn(
					"pocketkit: no auth configured; password login is still enabled. " +
						"Pass pocketkit.WithRauthy(...) to sign in through Rauthy only.")
				return nil
			})
		}
		return
	}

	if err := sso.Register(a.PocketBase, *a.cfg.Rauthy); err != nil {
		// A misconfigured auth policy must not start as an open app.
		panic(fmt.Sprintf("pocketkit: %v", err))
	}
}
