package pattern

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"honnef.co/go/tools/debug"
)

func TestParse(t *testing.T) {
	inputs := []string{
		`(Binding "name" _)`,
		`(Binding "name" _:[])`,
		`(Binding "name" _:_:[])`,
	}

	p := Parser{}
	for _, input := range inputs {
		if _, err := p.Parse(input); err != nil {
			t.Errorf("failed to parse %q: %s", input, err)
		}
	}
}

func FuzzParse(f *testing.F) {
	var files []*ast.File
	fset := token.NewFileSet()

	// Ideally we'd check against as much source code as possible, but that's fairly slow, on the order of 500ms per
	// pattern when checking against the whole standard library.
	//
	// We pick the runtime package in the hopes that it contains the most diverse, and weird, code.
	filepath.Walk(runtime.GOROOT()+"/src/runtime", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// XXX error handling
			panic(err)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := goparser.ParseFile(fset, path, nil, goparser.SkipObjectResolution)
		if err != nil {
			return nil
		}
		files = append(files, f)
		return nil
	})

	parse := func(in string, allowTypeInfo bool) (Pattern, bool) {
		p := Parser{
			AllowTypeInfo: allowTypeInfo,
		}
		pat, err := p.Parse(string(in))
		if err != nil {
			if strings.Contains(err.Error(), "internal error") {
				panic(err)
			}
			return Pattern{}, false
		}
		return pat, true
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		defer func() {
			if err := recover(); err != nil {
				str := fmt.Sprint(err)
				if strings.Contains(str, "binding already created:") {
					// This is an invalid pattern, not a real failure
				} else {
					// Re-panic the original panic
					panic(err)
				}
			}
		}()
		// Parse twice, once with AllowTypeInfo set to true to exercise the parser, and once with it set to false so we
		// can actually use it in Match, as we don't have type information available.

		pat, ok := parse(string(in), true)
		if !ok {
			return
		}
		// Make sure we can turn it back into a string
		_ = pat.Root.String()

		pat, ok = parse(string(in), false)
		if !ok {
			return
		}
		// Make sure we can turn it back into a string
		_ = pat.Root.String()

		entryNodesMap := make(map[reflect.Type]struct{})
		for _, node := range pat.EntryNodes {
			entryNodesMap[reflect.TypeOf(node)] = struct{}{}
		}

		// Don't check patterns with too many relevant nodes; it's too expensive
		if len(pat.EntryNodes) < 20 {
			// Make sure trying to match nodes doesn't panic
			for _, f := range files {
				ast.Inspect(f, func(node ast.Node) bool {
					rt := reflect.TypeOf(node)
					// We'd prefer calling Match on all nodes, not just those the pattern deems relevant, to find more bugs.
					// However, doing so has a 10x cost in execution time.
					if _, ok := entryNodesMap[rt]; ok {
						Match(pat, node)
					}
					return true
				})
			}
		}
	})
}

func TestMatchAlias(t *testing.T) {
	p1 := MustParse(`(CallExpr (Symbol "foo.Alias") _)`)
	p2 := MustParse(`(CallExpr (Symbol "int") _)`)

	f, _, info, err := debug.TypeCheck(`
package pkg
type Alias = int
func _() { _ = Alias(0) }
`)
	if err != nil {
		t.Fatal(err)
	}

	m := &Matcher{
		TypesInfo: info,
	}
	node := f.Decls[1].(*ast.FuncDecl).Body.List[0].(*ast.AssignStmt).Rhs[0]

	if debug.AliasesEnabled() {
		// Check that we can match on the name of the alias
		if ok := m.Match(p1, node); !ok {
			t.Errorf("%s did not match", p1.Root)
		}
	}

	// Check that we can match on the name of the alias's target
	if ok := m.Match(p2, node); !ok {
		t.Errorf("%s did not match", p2.Root)
	}
}

func TestCollectSymbols(t *testing.T) {
	for _, tt := range []struct {
		in  string
		out string
	}{
		{
			`(Or (Symbol "foo") (Symbol "bar"))`,
			`(Or (IndexSymbol "" "" "foo") (IndexSymbol "" "" "bar"))`,
		},
		{
			`(CallExpr (Symbol "foo") [(Symbol "bar") (Symbol "baz")])`,
			`(And (IndexSymbol "" "" "foo") (IndexSymbol "" "" "bar") (IndexSymbol "" "" "baz"))`,
		},
		{
			`(Symbol (Or "foo" "bar"))`,
			`(Or (IndexSymbol "" "" "foo") (IndexSymbol "" "" "bar"))`,
		},
		{
			// (Or) never matches anything, so we do need the "foo" symbol for a
			// successful match
			`(Or (Symbol "foo") (Or))`,
			`(IndexSymbol "" "" "foo")`,
		},
		{
			// This tests (And ...)
			`(BasicLit (Symbol "foo") (Ident "bar"))`,
			`(IndexSymbol "" "" "foo")`,
		},
		{
			`(Or (Symbol "foo") (Ident _))`,
			`_`,
		},
		{
			`(Or (Symbol "foo") (EmptyStmt))`,
			`_`,
		},
		{
			`(Or (Symbol "foo") nil)`,
			`_`,
		},
		{
			`(Symbol "example.com/foo.Get")`,
			`(IndexSymbol "example.com/foo" "" "Get")`,
		},
		{
			`(Symbol "(*example.com/foo.Client).Get")`,
			`(IndexSymbol "example.com/foo" "Client" "Get")`,
		},

		// Don't crash on malformed symbols
		{
			`(Symbol "")`,
			`(IndexSymbol "" "" "")`,
		},
		{
			`(Symbol "foo.")`,
			`(IndexSymbol "foo" "" "")`,
		},
		{
			`(Symbol "(foo")`,
			`(IndexSymbol "" "" "")`,
		},
		{
			`(Symbol "(foo)")`,
			`(IndexSymbol "" "" "")`,
		},
		{
			`(Symbol "(foo.Bar)")`,
			`(IndexSymbol "" "" "")`,
		},
		{
			`(Symbol "(foo.Bar).")`,
			`(IndexSymbol "foo" "Bar" "")`,
		},
		{
			`(Symbol "(foo.Bar.")`,
			`(IndexSymbol "" "" "")`,
		},
		{
			`(Symbol "(foo).Bar")`,
			`(IndexSymbol "" "" "")`,
		},
	} {
		p := &Parser{AllowTypeInfo: true}
		pat, err := p.Parse(tt.in)
		if err != nil {
			t.Fatal(err)
		}
		s := pat.SymbolsPattern.String()
		if s != tt.out {
			t.Fatalf("Symbol requirements for %s: got %s, want %s", tt.in, s, tt.out)
		}
	}
}
