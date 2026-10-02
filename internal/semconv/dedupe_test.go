package semconv

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestEveryEventConstantHasDocumentedDedupeKey(t *testing.T) {
	events := eventStringConstants(t)
	document, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	if err != nil {
		t.Fatalf("read configuration documentation: %v", err)
	}
	rows := documentedEventRows(t, string(document))

	var missing []string
	for _, event := range events {
		if _, ok := rows[event]; !ok {
			missing = append(missing, event)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("delivery semantics dedupe table is missing rows for event constants: %s", strings.Join(missing, ", "))
	}
}

func eventStringConstants(t *testing.T) []string {
	values := semanticStringConstants(t, "Event")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func semanticStringConstants(t *testing.T, prefix string) map[string]string {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list semconv Go files: %v", err)
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, file)
	}

	info := &types.Info{Defs: make(map[*ast.Ident]types.Object)}
	config := types.Config{Importer: importer.Default()}
	if _, err := config.Check("github.com/rknightion/cf2otel/internal/semconv", fset, files, info); err != nil {
		t.Fatalf("type-check semconv constants: %v", err)
	}

	events := make(map[string]string)
	for _, file := range files {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, spec := range group.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range values.Names {
					if !strings.HasPrefix(name.Name, prefix) {
						continue
					}
					constantValue, ok := info.Defs[name].(*types.Const)
					if !ok {
						continue
					}
					basic, ok := constantValue.Type().Underlying().(*types.Basic)
					if ok && (basic.Kind() == types.String || basic.Kind() == types.UntypedString) && constantValue.Val().Kind() == constant.String {
						events[name.Name] = constant.StringVal(constantValue.Val())
					}
				}
			}
		}
	}
	if len(events) == 0 {
		t.Fatalf("found no string constants named %s* in internal/semconv", prefix)
	}
	return events
}

func TestEveryAttributeConstantIsDenyKey(t *testing.T) {
	declared := semanticStringConstants(t, "Attr")
	file, err := parser.ParseFile(token.NewFileSet(), "deny_registry.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var registry *ast.CompositeLit
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if ok && len(spec.Names) == 1 && spec.Names[0].Name == "denyAttributes" && len(spec.Values) == 1 {
			registry, _ = spec.Values[0].(*ast.CompositeLit)
		}
		return true
	})
	if registry == nil {
		t.Fatal("compiled denyAttributes registry missing")
	}
	rows := make(map[string]bool)
	for _, row := range registry.Elts {
		pair, ok := row.(*ast.KeyValueExpr)
		if !ok {
			t.Fatal("registry entries must reference attribute constants")
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok || declared[key.Name] == "" {
			t.Fatal("registry keys must reference declared Attr* constants, not literals or aliases")
		}
		rows[key.Name] = true
	}
	for name := range declared {
		if !rows[name] {
			t.Errorf("%s is missing from compiled deny keys", name)
		}
	}
}

func documentedEventRows(t *testing.T, document string) map[string]struct{} {
	t.Helper()

	lines := strings.Split(document, "\n")
	for index, line := range lines {
		header, ok := markdownTableCells(line)
		if !ok || len(header) != 3 || header[0] != "Event constant" || header[2] != "Dedupe key" {
			continue
		}
		rows := make(map[string]struct{})
		for rowIndex := index + 1; rowIndex < len(lines); rowIndex++ {
			cells, ok := markdownTableCells(lines[rowIndex])
			if !ok {
				break
			}
			if isMarkdownSeparator(cells) {
				continue
			}
			if len(cells) != len(header) {
				t.Fatalf("delivery semantics dedupe table row has %d columns, want %d: %s", len(cells), len(header), lines[rowIndex])
			}
			name := strings.Trim(cells[0], "`")
			if name == "" || strings.TrimSpace(cells[2]) == "" {
				t.Fatalf("delivery semantics dedupe table row is missing an event constant or key: %s", lines[rowIndex])
			}
			if _, exists := rows[name]; exists {
				t.Fatalf("delivery semantics dedupe table repeats event constant %s", name)
			}
			rows[name] = struct{}{}
		}
		return rows
	}
	t.Fatal("configuration documentation has no Event constant / Dedupe key table")
	return nil
}

func markdownTableCells(line string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil, false
	}
	parts := strings.Split(line[1:len(line)-1], "|")
	cells := make([]string, len(parts))
	for index, part := range parts {
		cells[index] = strings.TrimSpace(part)
	}
	return cells, true
}

func isMarkdownSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		separator := strings.Trim(cell, "-: ")
		if separator != "" {
			return false
		}
	}
	return true
}
