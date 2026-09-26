package main

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

var collectorDatasetSuffix = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(Groups|Adaptive)$`)
var datasetConstantName = regexp.MustCompile(`(?i)(dataset|data_set)`)

func TestCollectorAPISurfaceHasContractEntries(t *testing.T) {
	files, err := parseCollectorFiles(t, "../../internal/collectors")
	if err != nil {
		t.Fatal(err)
	}

	datasets := collectorDatasets(files)
	paths := collectorRESTPaths(files)
	c, err := loadContract("../../spec/cloudflare/contract.json")
	if err != nil {
		t.Fatal(err)
	}

	contractDatasets := make(map[string]bool, len(c.GraphQL))
	for _, entry := range c.GraphQL {
		contractDatasets[entry.Dataset] = true
	}
	contractPaths := make(map[string]bool, len(c.REST))
	for _, entry := range c.REST {
		contractPaths[entry.Path] = true
	}

	var missing []string
	for dataset := range datasets {
		if !contractDatasets[dataset] {
			missing = append(missing, "GraphQL "+dataset)
		}
	}
	for path := range paths {
		if !contractPaths[path] {
			missing = append(missing, "REST "+path)
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("registered collector API surface is missing contract entries:\n  %s", strings.Join(missing, "\n  "))
	}
}

func parseCollectorFiles(t *testing.T, root string) ([]*ast.File, error) {
	t.Helper()
	var files []*ast.File
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.AllErrors)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	return files, err
}

func collectorDatasets(files []*ast.File) map[string]bool {
	datasets := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && collectorDatasetSuffix.MatchString(value) {
				datasets[value] = true
			}
			return true
		})

		// Dataset constants that do not use a Groups/Adaptive suffix remain in
		// scope when their identifier names the value as a dataset.
		ast.Inspect(file, func(node ast.Node) bool {
			value, ok := node.(*ast.ValueSpec)
			if !ok || len(value.Names) == 0 || len(value.Values) != 1 {
				return true
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			for _, name := range value.Names {
				if datasetConstantName.MatchString(name.Name) {
					datasets[text] = true
				}
			}
			return true
		})
	}
	return datasets
}

func collectorRESTPaths(files []*ast.File) map[string]bool {
	paths := map[string]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			values := functionStringValues(function.Body)
			members := rangeMemberValues(function.Body)
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := collectorCallName(call.Fun)
				if name == "Zones" {
					paths["/zones"] = true
					return true
				}
				if len(call.Args) < 2 {
					return true
				}
				index := 1
				switch name {
				case "Get", "GetPage", "GetRaw", "get":
				case "fetchPages":
					index = 2
				default:
					return true
				}
				if len(call.Args) <= index {
					return true
				}
				for _, path := range resolveStringPaths(call.Args[index], values, members) {
					if strings.HasPrefix(path, "/") {
						paths[path] = true
					}
				}
				return true
			})
		}
	}
	return paths
}

func collectorCallName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expression.Sel.Name
	case *ast.IndexExpr:
		return collectorCallName(expression.X)
	case *ast.IndexListExpr:
		return collectorCallName(expression.X)
	default:
		return ""
	}
}

func functionStringValues(body *ast.BlockStmt) map[string][]string {
	values := map[string][]string{}
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			for index, target := range statement.Lhs {
				name, ok := target.(*ast.Ident)
				if !ok || index >= len(statement.Rhs) {
					continue
				}
				resolved := resolveStringPaths(statement.Rhs[index], values, nil)
				if hasPath(resolved) {
					values[name.Name] = resolved
				}
			}
		case *ast.ValueSpec:
			for index, name := range statement.Names {
				if index >= len(statement.Values) {
					continue
				}
				resolved := resolveStringPaths(statement.Values[index], values, nil)
				if hasPath(resolved) {
					values[name.Name] = resolved
				}
			}
		}
		return true
	})
	return values
}

func hasPath(values []string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, "/") {
			return true
		}
	}
	return false
}

func rangeMemberValues(body *ast.BlockStmt) map[string][]string {
	values := map[string][]string{}
	ast.Inspect(body, func(node ast.Node) bool {
		statement, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		variable, ok := statement.Value.(*ast.Ident)
		if !ok {
			return true
		}
		literal, ok := statement.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		array, ok := literal.Type.(*ast.ArrayType)
		if !ok {
			return true
		}
		structure, ok := array.Elt.(*ast.StructType)
		if !ok {
			return true
		}
		fieldIndexes := map[string]int{}
		fieldIndex := 0
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				fieldIndexes[name.Name] = fieldIndex
				fieldIndex++
			}
		}
		for _, element := range literal.Elts {
			row, ok := element.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for name, index := range fieldIndexes {
				if index >= len(row.Elts) {
					continue
				}
				text, ok := stringLiteral(row.Elts[index])
				if ok {
					key := variable.Name + "." + name
					values[key] = append(values[key], text)
				}
			}
		}
		return true
	})
	return values
}

func resolveStringPaths(expression ast.Expr, values, members map[string][]string) []string {
	combine := func(left, right []string) []string {
		var combined []string
		for _, a := range left {
			for _, b := range right {
				combined = append(combined, a+b)
			}
		}
		return combined
	}
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if expression.Kind != token.STRING {
			return nil
		}
		value, err := strconv.Unquote(expression.Value)
		if err != nil {
			return nil
		}
		return []string{value}
	case *ast.Ident:
		if resolved := values[expression.Name]; len(resolved) > 0 {
			return resolved
		}
		return []string{"{" + pathParameter(expression.Name) + "}"}
	case *ast.SelectorExpr:
		if identifier, ok := expression.X.(*ast.Ident); ok {
			if resolved := members[identifier.Name+"."+expression.Sel.Name]; len(resolved) > 0 {
				return resolved
			}
		}
		if expression.Sel.Name == "AccountID" {
			return []string{"{account}"}
		}
		if expression.Sel.Name == "ID" {
			if identifier, ok := expression.X.(*ast.Ident); ok && strings.Contains(strings.ToLower(identifier.Name), "gateway") {
				return []string{"{gateway}"}
			}
			return []string{"{id}"}
		}
		return []string{"{" + pathParameter(expression.Sel.Name) + "}"}
	case *ast.CallExpr:
		if selector, ok := expression.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "PathEscape" && len(expression.Args) == 1 {
			return resolveStringPaths(expression.Args[0], values, members)
		}
		return []string{"{dynamic}"}
	case *ast.BinaryExpr:
		if expression.Op == token.ADD {
			return combine(resolveStringPaths(expression.X, values, members), resolveStringPaths(expression.Y, values, members))
		}
	}
	return nil
}

func pathParameter(name string) string {
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, "id")
	if name == "" {
		return "id"
	}
	return name
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}
