// Package scan walks a pocketkit app's api/ and hooks/ directories and reports
// what it finds. It reads real Go source -- the package clause and the exported
// declarations -- rather than relying on magic comments, so route files stay
// ordinary Go that an editor and the compiler both understand.
package scan

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Methods are the file names recognised as route handlers, mapped to their HTTP method.
var Methods = map[string]string{
	"GET.go":     "GET",
	"POST.go":    "POST",
	"PUT.go":     "PUT",
	"PATCH.go":   "PATCH",
	"DELETE.go":  "DELETE",
	"HEAD.go":    "HEAD",
	"OPTIONS.go": "OPTIONS",
}

// Route is a discovered route file.
type Route struct {
	Method     string // HTTP method, from the file name
	Path       string // URL path, from the directory layout
	ImportPath string // full Go import path of the containing package
	Package    string // package clause as written in the file
	File       string // path to the file on disk, for error messages

	// Handler is the exported func implementing the route. It is named after
	// the method -- GET.go declares func GET -- because every method file in a
	// directory shares one Go package and so needs a distinct identifier.
	Handler string

	// Middlewares is the exported middleware slice, e.g. GETMiddlewares.
	// Empty when the package declares none.
	Middlewares string

	// Public reports whether the handler carries a //pocketkit:public directive.
	Public bool
}

// Hook is a discovered hook file.
type Hook struct {
	Name       string   // PocketBase hook method name, from the directory name
	Tags       []string // scoping args (e.g. collection name), from the parent directory
	ImportPath string
	Package    string
	File       string
}

// Result is everything found in one app.
type Result struct {
	Routes []Route
	Hooks  []Hook
}

// App scans the app rooted at dir, whose module path is modulePath.
func App(dir, modulePath string) (*Result, error) {
	res := &Result{}

	apiDir := filepath.Join(dir, "api")
	if isDir(apiDir) {
		routes, err := scanAPI(apiDir, modulePath, dir)
		if err != nil {
			return nil, err
		}
		res.Routes = routes
	}

	hooksDir := filepath.Join(dir, "hooks")
	if isDir(hooksDir) {
		hooks, err := scanHooks(hooksDir, modulePath, dir)
		if err != nil {
			return nil, err
		}
		res.Hooks = hooks
	}

	return res, nil
}

func scanAPI(apiDir, modulePath, root string) ([]Route, error) {
	var routes []Route

	err := filepath.WalkDir(apiDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		method, ok := Methods[d.Name()]
		if !ok {
			return nil
		}

		pkgDir := filepath.Dir(p)
		urlPath, err := urlFromDir(apiDir, pkgDir)
		if err != nil {
			return err
		}

		if err := checkReserved(urlPath, rel(root, p)); err != nil {
			return err
		}

		info, err := inspect(p, method)
		if err != nil {
			return err
		}
		if !info.hasHandler {
			return fmt.Errorf("%s: declares no `func %s(e *core.RequestEvent) error`", rel(root, p), method)
		}

		imp, err := importPath(modulePath, root, pkgDir)
		if err != nil {
			return err
		}

		route := Route{
			Method:     method,
			Path:       urlPath,
			ImportPath: imp,
			Package:    info.pkg,
			File:       p,
			Handler:    method,
			Public:     info.public,
		}
		if info.hasMiddlewares {
			route.Middlewares = method + middlewaresSuffix
		}
		routes = append(routes, route)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})

	for i := 1; i < len(routes); i++ {
		if routes[i].Path == routes[i-1].Path && routes[i].Method == routes[i-1].Method {
			return nil, fmt.Errorf("duplicate route %s %s: %s and %s",
				routes[i].Method, routes[i].Path, rel(root, routes[i-1].File), rel(root, routes[i].File))
		}
	}

	return routes, nil
}

func scanHooks(hooksDir, modulePath, root string) ([]Hook, error) {
	var hooks []Hook

	err := filepath.WalkDir(hooksDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == hooksDir {
			return nil
		}
		// A hook directory is one named after a PocketBase hook method.
		if !isHookName(d.Name()) {
			return nil
		}

		info, file, err := inspectDir(p, "Handle")
		if err != nil {
			return err
		}
		if info == nil {
			return nil // no .go files yet
		}
		if !info.hasHandler {
			return fmt.Errorf("%s: declares no `func Handle(...) error`", rel(root, file))
		}

		imp, err := importPath(modulePath, root, p)
		if err != nil {
			return err
		}

		// Directories between hooks/ and the hook dir become the hook's tags,
		// e.g. hooks/coins/OnRecordAfterCreateSuccess -> tag "coins".
		relDir, err := filepath.Rel(hooksDir, filepath.Dir(p))
		if err != nil {
			return err
		}
		var tags []string
		if relDir != "." {
			tags = strings.Split(filepath.ToSlash(relDir), "/")
		}

		hooks = append(hooks, Hook{
			Name:       d.Name(),
			Tags:       tags,
			ImportPath: imp,
			Package:    info.pkg,
			File:       file,
		})
		return fs.SkipDir
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].Name != hooks[j].Name {
			return hooks[i].Name < hooks[j].Name
		}
		return strings.Join(hooks[i].Tags, ",") < strings.Join(hooks[j].Tags, ",")
	})

	return hooks, nil
}

// urlFromDir converts a directory path under api/ into a URL path.
//
//	api/coins          -> /api/coins
//	api/coins/_id      -> /api/coins/{id}
//	api/files/_path_   -> /api/files/{path...}
func urlFromDir(apiDir, pkgDir string) (string, error) {
	relDir, err := filepath.Rel(apiDir, pkgDir)
	if err != nil {
		return "", err
	}
	out := "/api"
	if relDir == "." {
		return out, nil
	}
	for _, seg := range strings.Split(filepath.ToSlash(relDir), "/") {
		switch {
		case strings.HasPrefix(seg, "_") && strings.HasSuffix(seg, "_") && len(seg) > 2:
			out = path.Join(out, "{"+strings.Trim(seg, "_")+"...}")
		case strings.HasPrefix(seg, "_") && len(seg) > 1:
			out = path.Join(out, "{"+seg[1:]+"}")
		default:
			out = path.Join(out, seg)
		}
	}
	return out, nil
}

// middlewaresSuffix is appended to a method name to form the middleware slice
// identifier, e.g. GET + Middlewares.
const middlewaresSuffix = "Middlewares"

// publicDirective marks a handler as not requiring auth. It is a directive
// comment in the style of //go:embed, so it attaches to the handler it
// describes and cannot collide with another method in the same package.
const publicDirective = "//pocketkit:public"

type fileInfo struct {
	pkg            string
	hasHandler     bool
	hasMiddlewares bool
	public         bool
}

// inspect parses one file and reports the declarations pocketkit cares about.
// wantFunc is the exported func the file must declare: the method name for a
// route file, "Handle" for a hook.
func inspect(p, wantFunc string) (*fileInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	info := &fileInfo{pkg: f.Name.Name}
	readDecls(f, info, wantFunc)
	return info, nil
}

// inspectDir parses every .go file in a directory, merging what it finds. A hook
// or route package may split Handle and Middlewares across files.
func inspectDir(dir, wantFunc string) (*fileInfo, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
	}
	var merged *fileInfo
	var firstFile string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := inspect(p, wantFunc)
		if err != nil {
			return nil, p, err
		}
		if merged == nil {
			merged = info
			firstFile = p
			continue
		}
		merged.hasHandler = merged.hasHandler || info.hasHandler
		merged.hasMiddlewares = merged.hasMiddlewares || info.hasMiddlewares
		merged.public = merged.public || info.public
	}
	return merged, firstFile, nil
}

func readDecls(f *ast.File, info *fileInfo, wantFunc string) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || d.Name.Name != wantFunc {
				continue
			}
			info.hasHandler = true
			if hasPublicDirective(d.Doc) {
				info.public = true
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if name.Name == wantFunc+middlewaresSuffix {
						info.hasMiddlewares = true
					}
				}
			}
		}
	}
}

// hasPublicDirective reports whether a doc comment carries //pocketkit:public.
func hasPublicDirective(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.TrimSpace(c.Text) == publicDirective {
			return true
		}
	}
	return false
}

// isHookName reports whether a directory name looks like a PocketBase hook
// method: exported and starting with "On".
func isHookName(name string) bool {
	return strings.HasPrefix(name, "On") && len(name) > 2 && name[2] >= 'A' && name[2] <= 'Z'
}

func importPath(modulePath, root, dir string) (string, error) {
	relDir, err := filepath.Rel(root, dir)
	if err != nil {
		return "", err
	}
	return path.Join(modulePath, filepath.ToSlash(relDir)), nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
