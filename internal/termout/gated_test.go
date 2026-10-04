package termout_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Nothing outside internal/termout may write to the terminal directly.
//
// This exists because an assumption was wrong twice. internal/process gated its
// pass-through behind a `silent` flag; internal/docker was assumed to share it
// and had no gate at all, so every container log line was echoed over the TUI's
// frame -- reported from a Compose stack after the first round of fixes claimed
// to have handled it. A grep is the only thing that actually checks.
func TestNothingWritesToTheTerminalDirectly(t *testing.T) {
	banned := map[string]string{
		"fmt.Print":   "use termout.Printf",
		"fmt.Println": "use termout.Printf",
		"fmt.Printf":  "use termout.Printf",
		"os.Stdout":   "use termout.Printf or termout.Writer",
		"os.Stderr":   "use termout.Errorf",
	}

	pkgs, err := filepath.Glob("../*")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	for _, dir := range pkgs {
		if strings.HasSuffix(dir, "termout") {
			continue // the one place allowed to do it
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue // tests may print; they own their own output
			}
			src, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				continue // not our business to fail on a parse error here
			}
			ast.Inspect(src, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				expr := ident.Name + "." + sel.Sel.Name
				if advice, bad := banned[expr]; bad {
					t.Errorf("%s writes to the terminal directly via %s -- %s\n"+
						"  (it would scroll or clear the TUI's frame while the TUI owns the screen)",
						fset.Position(sel.Pos()), expr, advice)
				}
				return true
			})
		}
	}
}
