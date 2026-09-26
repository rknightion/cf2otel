package cfapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	saturationMessageText = regexp.MustCompile(`(?i)saturated\s+limit`)
	saturationWord        = regexp.MustCompile(`(?i)saturat`)
)

// stringMatchers are the text-inspection calls a collector could use to
// recognise the saturation message instead of calling AsSaturation.
var stringMatchers = map[string]map[string]bool{
	"strings": {"Contains": true, "ContainsAny": true, "EqualFold": true, "HasPrefix": true, "HasSuffix": true, "Index": true, "LastIndex": true},
	"regexp":  {"MustCompile": true, "Compile": true, "MatchString": true},
}

// TestCollectorsDoNotStringMatchSaturation keeps saturation recognition in
// cfapi: collectors call AsSaturation and never inspect the message text.
// The aigateway package is owned elsewhere; it carries no saturation check
// today and is scanned like every other collector.
func TestCollectorsDoNotStringMatchSaturation(t *testing.T) {
	root := filepath.Join("..", "collectors")
	var violations []string
	scanned := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned[filepath.Dir(path)] = true
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BasicLit:
				if node.Kind == token.STRING && saturationMessageText.MatchString(literalText(node)) {
					violations = append(violations, fset.Position(node.Pos()).String()+": saturation message literal")
				}
			case *ast.CallExpr:
				if isStringMatcher(node) && mentionsSaturation(node) {
					violations = append(violations, fset.Position(node.Pos()).String()+": string match on saturation text")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan collectors: %v", err)
	}
	if len(scanned) < 10 {
		t.Fatalf("scanned %d collector packages under %s, want the full collector tree", len(scanned), root)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("collectors must recognise saturation with cfapi.AsSaturation, not by message text:\n%s", strings.Join(violations, "\n"))
	}
}

func literalText(lit *ast.BasicLit) string {
	if text, err := strconv.Unquote(lit.Value); err == nil {
		return text
	}
	return lit.Value
}

func isStringMatcher(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && stringMatchers[pkg.Name][selector.Sel.Name]
}

func mentionsSaturation(call *ast.CallExpr) bool {
	found := false
	for _, arg := range call.Args {
		ast.Inspect(arg, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BasicLit:
				if node.Kind == token.STRING && saturationWord.MatchString(literalText(node)) {
					found = true
				}
			case *ast.Ident:
				if saturationWord.MatchString(node.Name) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}
