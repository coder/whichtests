package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"
)

type fileSnapshot struct {
	packageName string
	tests       map[string]lineRange
}

func parseFileSnapshot(data []byte) (fileSnapshot, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", data, parser.SkipObjectResolution)
	if err != nil {
		return fileSnapshot{}, err
	}

	testingDotImport := hasTestingDotImport(file)
	snapshot := fileSnapshot{
		packageName: file.Name.Name,
		tests:       map[string]lineRange{},
	}
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name == nil {
			continue
		}

		if isTopLevelTestFunc(funcDecl, testingDotImport) || isTopLevelFuzzFunc(funcDecl, testingDotImport) || isTopLevelExampleFunc(funcDecl) {
			snapshot.tests[funcDecl.Name.Name] = nodeRange(fset, decl)
		}
	}
	return snapshot, nil
}

func hasTestingDotImport(file *ast.File) bool {
	for _, importSpec := range file.Imports {
		if importSpec == nil || importSpec.Path == nil {
			continue
		}
		if importSpec.Name == nil || importSpec.Name.Name != "." {
			continue
		}
		if strings.Trim(importSpec.Path.Value, `"`) == "testing" {
			return true
		}
	}
	return false
}

func nodeRange(fset *token.FileSet, node ast.Node) lineRange {
	start := fset.Position(node.Pos()).Line
	end := fset.Position(node.End()).Line
	if end < start {
		end = start
	}
	return lineRange{Start: start, End: end}
}

func isTopLevelTestFunc(fn *ast.FuncDecl, testingDotImport bool) bool {
	if fn.Recv != nil || !hasRunnableName(fn.Name, "Test", false) {
		return false
	}
	if hasParamSelectorName(fn, "T") {
		return true
	}
	return testingDotImport && hasParamIdentName(fn, "T")
}

func isTopLevelFuzzFunc(fn *ast.FuncDecl, testingDotImport bool) bool {
	if fn.Recv != nil || !hasRunnableName(fn.Name, "Fuzz", false) {
		return false
	}
	if hasParamSelectorName(fn, "F") {
		return true
	}
	return testingDotImport && hasParamIdentName(fn, "F")
}

func isTopLevelExampleFunc(fn *ast.FuncDecl) bool {
	return fn.Recv == nil && hasRunnableName(fn.Name, "Example", true) && fn.Type != nil && fn.Type.Params != nil && len(fn.Type.Params.List) == 0
}

func hasRunnableName(name *ast.Ident, prefix string, allowBare bool) bool {
	if name == nil {
		return false
	}
	rest, ok := strings.CutPrefix(name.Name, prefix)
	if !ok {
		return false
	}
	if rest == "" {
		return allowBare
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return r == '_' || !unicode.IsLower(r)
}

func hasParamSelectorName(fn *ast.FuncDecl, expectedName string) bool {
	if fn.Type == nil || fn.Type.Params == nil {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	name, ok := pointerSelectorName(params[0].Type)
	return ok && name == expectedName
}

func hasParamIdentName(fn *ast.FuncDecl, expectedName string) bool {
	if fn.Type == nil || fn.Type.Params == nil {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	name, ok := pointerIdentName(params[0].Type)
	return ok && name == expectedName
}

func pointerSelectorName(expr ast.Expr) (string, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil {
		return "", false
	}
	return selector.Sel.Name, true
}

func pointerIdentName(expr ast.Expr) (string, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}
