package pocketkit

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// RouteDef is a single discovered API route.
//
// pocketkit's generated code populates these from the filesystem layout of the
// app's api/ directory. Handlers and middlewares are plain PocketBase values --
// there is no pocketkit wrapper type -- so anything you can write in a stock
// PocketBase route works unchanged here.
type RouteDef struct {
	Method string
	Path   string

	// Handler is the route's Handle func.
	Handler func(e *core.RequestEvent) error

	// Middlewares are the route's optional exported Middlewares slice.
	Middlewares []*hook.Handler[*core.RequestEvent]

	// Public reports whether the route opted out of the default auth
	// requirement by declaring `var Public = true`.
	Public bool
}

// HookDef is a single discovered event hook binding.
type HookDef struct {
	// Name is the PocketBase hook method name, e.g. "OnRecordAfterCreateSuccess".
	Name string

	// Tags are the hook's scoping arguments, e.g. a collection name. May be empty.
	Tags []string

	// Bind attaches the hook's Handle func to the app. The generated code closes
	// over the concrete PocketBase event type, so signatures are checked by the
	// compiler rather than by pocketkit.
	Bind func(app core.App)
}

var (
	registeredRoutes []RouteDef
	registeredHooks  []HookDef
)

// Register records routes discovered by generated code. It is called from the
// generated file's init() and is not intended to be called by hand.
func Register(defs ...RouteDef) {
	registeredRoutes = append(registeredRoutes, defs...)
}

// RegisterHooks records hooks discovered by generated code.
func RegisterHooks(defs ...HookDef) {
	registeredHooks = append(registeredHooks, defs...)
}

// Routes returns the routes discovered so far, in registration order.
func Routes() []RouteDef { return registeredRoutes }

// Hooks returns the hooks discovered so far, in registration order.
func Hooks() []HookDef { return registeredHooks }
