package pocketkit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

func TestRouteAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name       string
		collection string
		public     bool
		options    []Option
		middleware func() *hook.Handler[*core.RequestEvent]
		want       int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "signed in", collection: "users", want: http.StatusOK},
		{
			name: "auth before cached response", want: http.StatusUnauthorized,
			middleware: func() *hook.Handler[*core.RequestEvent] {
				return &hook.Handler[*core.RequestEvent]{Func: func(e *core.RequestEvent) error {
					return e.String(http.StatusOK, "cached private response")
				}}
			},
		},
		{
			name: "explicit restriction preserved", collection: "users", want: http.StatusForbidden,
			middleware: func() *hook.Handler[*core.RequestEvent] { return apis.RequireAuth("staff") },
		},
		{
			name: "explicit restriction satisfied", collection: "staff", want: http.StatusOK,
			middleware: func() *hook.Handler[*core.RequestEvent] { return apis.RequireAuth("staff") },
		},
		{
			name: "framework restriction preserved", collection: "users", want: http.StatusForbidden,
			options:    []Option{AuthCollections("staff")},
			middleware: func() *hook.Handler[*core.RequestEvent] { return apis.RequireAuth("users") },
		},
		{name: "public directive", public: true, want: http.StatusOK},
		{name: "public default", options: []Option{PublicByDefault()}, want: http.StatusOK},
		{
			name: "explicit auth with public default", options: []Option{PublicByDefault()},
			middleware: func() *hook.Handler[*core.RequestEvent] { return apis.RequireAuth() },
			want:       http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := registeredRoutes
			t.Cleanup(func() { registeredRoutes = original })
			def := RouteDef{
				Method: http.MethodGet, Path: "/private", Public: tc.public,
				Handler: func(e *core.RequestEvent) error { return e.String(http.StatusOK, "private response") },
			}
			if tc.middleware != nil {
				def.Middlewares = []*hook.Handler[*core.RequestEvent]{tc.middleware()}
			}
			registeredRoutes = []RouteDef{def}
			a := New(append([]Option{AllowPasswords(), WithFrontend("")}, tc.options...)...)
			r := router.NewRouter(func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
				e := &core.RequestEvent{App: a}
				e.Response, e.Request = w, req
				if tc.collection != "" {
					e.Auth = core.NewRecord(core.NewAuthCollection(tc.collection))
				}
				return e, nil
			})
			if err := a.OnServe().Trigger(&core.ServeEvent{App: a, Router: r}); err != nil {
				t.Fatal(err)
			}
			mux, err := r.BuildMux()
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/private", nil))
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
