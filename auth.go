package pocketkit

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
)

// OIDCProvider is the PocketBase OAuth2 provider slot pocketkit expects the
// identity server (Rauthy) to occupy. PocketBase registers a generic OpenID
// Connect provider under "oidc", "oidc2" and "oidc3"; pocketkit uses the first.
const OIDCProvider = auth.NameOIDC

// enforceAuthPolicy applies pocketkit's auth opinion to every auth collection:
// password login off, OAuth2 on. Credentials themselves are never set here --
// they are entered in the superuser dashboard, so secrets stay out of the repo.
//
// It runs both at bootstrap (to correct existing collections) and on every
// collection save (so the policy cannot be clicked off in the dashboard).
func (a *App) enforceAuthPolicy(c *core.Collection) bool {
	if !c.IsAuth() || a.cfg.AllowPasswords {
		return false
	}

	// _superusers keeps password login on purpose. The dashboard is where the
	// OIDC provider gets configured in the first place, so locking it behind
	// OIDC would leave nobody able to set OIDC up.
	if c.System || c.Name == core.CollectionNameSuperusers {
		return false
	}

	changed := false
	if c.PasswordAuth.Enabled {
		c.PasswordAuth.Enabled = false
		changed = true
	}
	if !c.OAuth2.Enabled {
		c.OAuth2.Enabled = true
		changed = true
	}
	return changed
}

// bindAuthPolicy wires the policy into the app's lifecycle.
func (a *App) bindAuthPolicy() {
	if a.cfg.AllowPasswords {
		return
	}

	a.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}

		collections := []*core.Collection{}
		if err := e.App.CollectionQuery().All(&collections); err != nil {
			return fmt.Errorf("pocketkit: loading collections: %w", err)
		}

		for _, c := range collections {
			if a.enforceAuthPolicy(c) {
				if err := e.App.Save(c); err != nil {
					return fmt.Errorf("pocketkit: enforcing auth policy on %q: %w", c.Name, err)
				}
				e.App.Logger().Info("pocketkit: disabled password auth", "collection", c.Name)
			}
			if c.IsAuth() && !c.System && c.Name != core.CollectionNameSuperusers {
				a.warnIfNoOIDC(e.App, c)
			}
		}
		return nil
	})

	// Re-assert on every save so the dashboard cannot re-enable passwords.
	reassert := func(e *core.CollectionEvent) error {
		a.enforceAuthPolicy(e.Collection)
		return e.Next()
	}
	a.OnCollectionCreate().BindFunc(reassert)
	a.OnCollectionUpdate().BindFunc(reassert)
}

// warnIfNoOIDC logs a startup warning when an auth collection has no OIDC
// provider configured. With passwords disabled such a collection cannot be
// logged into at all, which is worth saying out loud rather than discovering
// at the login screen.
func (a *App) warnIfNoOIDC(app core.App, c *core.Collection) {
	if _, ok := c.OAuth2.GetProviderConfig(OIDCProvider); ok {
		return
	}
	app.Logger().Warn(
		"pocketkit: auth collection has no OIDC provider; nobody can sign in. "+
			"Configure it in the dashboard under Collections > "+c.Name+
			" > Options > OAuth2 > OpenID Connect.",
		"collection", c.Name,
	)
}
