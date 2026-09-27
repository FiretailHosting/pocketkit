package main

import (
	"go/parser"
	"go/token"
)

// packageClause returns the package name declared by a Go file.
func packageClause(path string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}
