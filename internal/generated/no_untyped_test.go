package generated

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestGeneratedTypesHaveNoUntypedValues walks every exported declaration in
// the generated wire types and fails on `interface{}` / `any` (doc 42
// §5.1.2). Free-form JSON objects are typed as json.RawMessage by
// scripts/lib/freeform-json.mjs, and the union template omits
// ValueByDiscriminator (scripts/templates/union.tmpl); if a spec change
// reintroduces an untyped value, fix the generator step rather than the
// generated file.
func TestGeneratedTypesHaveNoUntypedValues(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, node ast.Node) {
		ast.Inspect(node, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.InterfaceType:
				t.Errorf("%s: %s uses interface{}", fset.Position(x.Pos()), where)
			case *ast.Ident:
				if x.Name == "any" && x.Obj == nil {
					t.Errorf("%s: %s uses any", fset.Position(x.Pos()), where)
				}
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
					check("type "+ts.Name.Name, ts.Type)
				}
			}
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				// Exported signatures only; bodies may use any internally.
				check("func "+d.Name.Name, d.Type)
			}
		}
	}
}
