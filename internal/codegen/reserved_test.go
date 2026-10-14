package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestReservedCoversRuntime makes sure every exported identifier declared by
// the hand-written runtime is reserved, so generated names never collide with it.
func TestReservedCoversRuntime(t *testing.T) {
	files, err := filepath.Glob("../../*.go")
	if err != nil {
		t.Fatal(err)
	}
	isReserved := map[string]bool{}
	for _, r := range reserved {
		isReserved[r] = true
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, ".gen.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, d := range af.Decls {
			var names []*ast.Ident
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					names = append(names, d.Name)
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						names = append(names, s.Name)
					case *ast.ValueSpec:
						names = append(names, s.Names...)
					}
				}
			}
			for _, n := range names {
				if n.IsExported() && !isReserved[n.Name] {
					t.Errorf("%s: exported identifier %s is not in the reserved list", filepath.Base(f), n.Name)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no runtime files found")
	}
}
