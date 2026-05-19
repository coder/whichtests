package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	packagePatternRE = regexp.MustCompile(`(?m)^package\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	fallbackTestRE   = regexp.MustCompile(`(?m)^func\s+((?:Test|Fuzz)[A-Z_][A-Za-z0-9_]*|Example(?:[A-Z_][A-Za-z0-9_]*)?)\s*\(`)
)

type fileSnapshot struct {
	packageName      string
	tests            map[string]lineRange
	shared           []sharedDecl
	sharedKeys       map[string]struct{}
	testingDotImport bool
}

type sharedDeclKind uint8

const (
	sharedDeclImport sharedDeclKind = iota + 1
	sharedDeclVar
	sharedDeclConst
	sharedDeclType
	sharedDeclHelper
	sharedDeclInit
	sharedDeclTestMain
)

type sharedDecl struct {
	Range lineRange
	Kind  sharedDeclKind
	Keys  []string
}

func parseFileSnapshot(data []byte) (fileSnapshot, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", data, parser.SkipObjectResolution)
	if err != nil {
		return fileSnapshot{}, err
	}

	snapshot := fileSnapshot{
		packageName:      file.Name.Name,
		tests:            map[string]lineRange{},
		sharedKeys:       map[string]struct{}{},
		testingDotImport: hasTestingDotImport(file),
	}
	for _, decl := range file.Decls {
		rangeForDecl := nodeRange(fset, decl)
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				snapshot.addSharedDecl(sharedDecl{Range: rangeForDecl, Kind: sharedDeclHelper})
				continue
			}
			snapshot.addSharedDecl(classifyGenDecl(rangeForDecl, genDecl))
			continue
		}
		if funcDecl.Name == nil {
			snapshot.addSharedDecl(sharedDecl{Range: rangeForDecl, Kind: sharedDeclHelper})
			continue
		}

		name := funcDecl.Name.Name
		switch {
		case name == "TestMain":
			snapshot.addSharedDecl(sharedDecl{
				Range: rangeForDecl,
				Kind:  sharedDeclTestMain,
				Keys:  []string{"func:TestMain"},
			})
		case name == "init":
			snapshot.addSharedDecl(sharedDecl{Range: rangeForDecl, Kind: sharedDeclInit})
		case isTopLevelTestFunc(funcDecl, snapshot.testingDotImport), isTopLevelFuzzFunc(funcDecl, snapshot.testingDotImport), isTopLevelExampleFunc(funcDecl):
			snapshot.tests[name] = rangeForDecl
		default:
			snapshot.addSharedDecl(sharedDecl{
				Range: rangeForDecl,
				Kind:  sharedDeclHelper,
				Keys:  []string{funcIdentity(fset, funcDecl)},
			})
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

func classifyGenDecl(rangeForDecl lineRange, decl *ast.GenDecl) sharedDecl {
	shared := sharedDecl{Range: rangeForDecl}
	switch decl.Tok {
	case token.IMPORT:
		shared.Kind = sharedDeclImport
	case token.VAR:
		shared.Kind = sharedDeclVar
		shared.Keys = genDeclKeys("var", decl.Specs)
	case token.CONST:
		shared.Kind = sharedDeclConst
		shared.Keys = genDeclKeys("const", decl.Specs)
	case token.TYPE:
		shared.Kind = sharedDeclType
		shared.Keys = genDeclKeys("type", decl.Specs)
	default:
		shared.Kind = sharedDeclHelper
	}
	return shared
}

func genDeclKeys(prefix string, specs []ast.Spec) []string {
	keys := make([]string, 0, len(specs))
	for _, spec := range specs {
		switch typed := spec.(type) {
		case *ast.TypeSpec:
			if typed.Name == nil || typed.Name.Name == "_" {
				continue
			}
			keys = append(keys, prefix+":"+typed.Name.Name)
		case *ast.ValueSpec:
			for _, name := range typed.Names {
				if name == nil || name.Name == "_" {
					continue
				}
				keys = append(keys, prefix+":"+name.Name)
			}
		}
	}
	slices.Sort(keys)
	return keys
}

func funcIdentity(fset *token.FileSet, fn *ast.FuncDecl) string {
	if fn.Name == nil {
		return ""
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "func:" + fn.Name.Name
	}
	return "method:" + exprString(fset, fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, fset, expr); err != nil {
		return fmt.Sprintf("%T", expr)
	}
	return buffer.String()
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
	if name == nil || !strings.HasPrefix(name.Name, prefix) {
		return false
	}
	rest := strings.TrimPrefix(name.Name, prefix)
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

func fallbackPackageName(data []byte) (string, bool) {
	matches := packagePatternRE.FindSubmatch(data)
	if len(matches) < 2 {
		return "", false
	}
	return string(matches[1]), true
}

func fallbackTestNames(data []byte) []string {
	matches := fallbackTestRE.FindAllSubmatch(data, -1)
	selected := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		selected[string(match[1])] = struct{}{}
	}
	return slices.Sorted(maps.Keys(selected))
}

func parseOrFallbackSnapshot(data []byte) (fileSnapshot, error) {
	snapshot, err := parseFileSnapshot(data)
	if err == nil {
		return snapshot, nil
	}
	packageName, ok := fallbackPackageName(data)
	if !ok {
		return fileSnapshot{}, err
	}
	fallback := fileSnapshot{
		packageName: packageName,
		tests:       map[string]lineRange{},
		sharedKeys:  map[string]struct{}{},
	}
	for _, testName := range fallbackTestNames(data) {
		fallback.tests[testName] = lineRange{}
	}
	return fallback, nil
}

func (snapshot *fileSnapshot) addSharedDecl(decl sharedDecl) {
	snapshot.shared = append(snapshot.shared, decl)
	for _, key := range decl.Keys {
		if key == "" {
			continue
		}
		snapshot.sharedKeys[key] = struct{}{}
	}
}

func (snapshot *fileSnapshot) hasSharedKey(keys []string) bool {
	for _, key := range keys {
		if _, ok := snapshot.sharedKeys[key]; ok {
			return true
		}
	}
	return false
}
