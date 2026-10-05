package pocketkit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// SSO restricts protected routes to its collection by default; apps can
// deliberately allow additional auth collections.
func TestSSOWithCollectionRestriction(t *testing.T) {
	for _, tc := range []struct {
		name, collection     string
		age                  time.Duration
		defaultSSOCollection bool
		allowSuperuser       bool
		want                 int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "active SSO user", collection: "users", want: http.StatusOK},
		{name: "default SSO collection", collection: "users", defaultSSOCollection: true, want: http.StatusOK},
		{name: "expired SSO user", collection: "users", age: 2 * time.Hour, want: http.StatusUnauthorized},
		{name: "unrelated identity", collection: "customers", want: http.StatusForbidden},
		{name: "superuser excluded", collection: "_superusers", want: http.StatusForbidden},
		{name: "superuser explicitly allowed", collection: "_superusers", allowSuperuser: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssoCollection := "users"
			if tc.defaultSSOCollection {
				ssoCollection = ""
			}
			cfg := Config{}
			WithSSO(SSOConfig{Collection: ssoCollection, RequiredGroup: "app-users", SessionMaxAge: time.Hour})(&cfg)
			if tc.allowSuperuser {
				AuthCollections("users", "_superusers")(&cfg)
			}
			a := &App{PocketBase: pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()}), cfg: cfg}
			if err := a.Bootstrap(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { a.ResetBootstrapState() })
			original := registeredRoutes
			t.Cleanup(func() { registeredRoutes = original })
			registeredRoutes = []RouteDef{{Method: "GET", Path: "/private", Handler: func(e *core.RequestEvent) error { return e.String(200, "private") }}}
			a.bindAuthPolicy()
			a.bindRoutes()
			r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
				e := &core.RequestEvent{App: a}
				e.Request, e.Response = req, w
				if tc.collection != "" {
					collection := core.NewAuthCollection(tc.collection)
					collection.Fields.Add(&core.DateField{Name: "sso_login_at"})
					e.Auth = core.NewRecord(collection)
					e.Auth.Set("sso_login_at", time.Now().Add(-tc.age))
				}
				return e, nil
			})
			if err := a.OnServe().Trigger(&core.ServeEvent{App: a, Router: r}); err != nil {
				t.Fatal(err)
			}
			handler, err := r.BuildMux()
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "/private", nil))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d; %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

// SSOConfig accepts a collection ID, but RequireAuth compares names. A missing
// collection must stop serving rather than leave routes unrestricted.
func TestRouteAuthCollectionsResolvesSSOCollection(t *testing.T) {
	app := &App{PocketBase: pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	app.cfg.SSO = &SSOConfig{Collection: users.Id}
	if collections, err := app.routeAuthCollections(); err != nil || len(collections) != 1 || collections[0] != "users" {
		t.Fatalf("collection ID resolved to %v, %v; want [users]", collections, err)
	}
	app.cfg.SSO = &SSOConfig{Collection: "missing"}
	if _, err := app.routeAuthCollections(); err == nil {
		t.Fatal("missing SSO collection was accepted")
	}
}
