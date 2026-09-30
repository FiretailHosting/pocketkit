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

// Match the scaffold's explicit policy: SSO sessions and collection restrictions
// must both hold. Other apps can deliberately allow additional auth collections.
func TestSSOWithCollectionRestriction(t *testing.T) {
	for _, tc := range []struct {
		name, collection string
		age              time.Duration
		allowSuperuser   bool
		want             int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "active SSO user", collection: "users", want: http.StatusOK},
		{name: "expired SSO user", collection: "users", age: 2 * time.Hour, want: http.StatusUnauthorized},
		{name: "unrelated identity", collection: "customers", want: http.StatusForbidden},
		{name: "superuser excluded", collection: "_superusers", want: http.StatusForbidden},
		{name: "superuser explicitly allowed", collection: "_superusers", allowSuperuser: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			WithSSO(SSOConfig{Collection: "users", RequiredGroup: "app-users", SessionMaxAge: time.Hour})(&cfg)
			AuthCollections("users")(&cfg)
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
