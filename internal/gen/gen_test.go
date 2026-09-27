package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/FiretailHosting/pocketkit/internal/scan"
)

func TestEmptyAppTypeChecks(t *testing.T) {
	src, err := File(&scan.Result{}, "main")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "pocketkit_gen.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := new(types.Config).Check("example.com/app", fset, []*ast.File{f}, nil); err != nil {
		t.Fatalf("empty app does not type check: %v\n%s", err, src)
	}
}
