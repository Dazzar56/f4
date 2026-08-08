package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// UI constructors we want to inspect for hardcoded string arguments.
var uiConstructors = map[string]bool{
	"NewLabel":          true,
	"NewButton":         true,
	"NewCheckbox":       true,
	"NewCenteredDialog": true,
	"NewVMenu":          true,
	"NewText":           true,
}

func main() {
	fmt.Println("--- f4 AST Hardcoded String Detector ---")
	fmt.Println("Scanning directory recursively...")

	fset := token.NewFileSet()
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "tools") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		inspectFile(path, node, fset)
		return nil
	})

	if err != nil {
		fmt.Printf("Scan failed: %v\n", err)
	} else {
		fmt.Println("Scan complete.")
	}
}

func inspectFile(path string, file *ast.File, fset *token.FileSet) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		var funcName string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			funcName = fun.Name
		case *ast.SelectorExpr:
			funcName = fun.Sel.Name
		}

		if uiConstructors[funcName] {
			for i, arg := range call.Args {
				inspectArg(path, funcName, i, arg, fset)
			}
		}

		// Also look for hardcoded combobox options slices like []string{"Never", "Daily"}
		if funcName == "NewComboBox" && len(call.Args) >= 4 {
			if comp, ok := call.Args[3].(*ast.CompositeLit); ok {
				for _, elt := range comp.Elts {
					if lit, ok := elt.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						pos := fset.Position(lit.ValuePos)
						fmt.Printf("[%s:%d] Combobox option is hardcoded: %s\n", path, pos.Line, lit.Value)
					}
				}
			}
		}

		return true
	})
}

func inspectArg(path string, funcName string, argIndex int, expr ast.Expr, fset *token.FileSet) {
	// Filter arguments that must be strings
	isTarget := false
	switch funcName {
	case "NewLabel", "NewButton", "NewCheckbox":
		isTarget = (argIndex == 2) // Third argument is label
	case "NewCenteredDialog":
		isTarget = (argIndex == 2) // Third argument is title
	case "NewVMenu", "NewText":
		isTarget = (argIndex == 0) // First argument is text
	}

	if !isTarget {
		return
	}

	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		pos := fset.Position(lit.ValuePos)
		// Ignore empty strings and layout borders
		val := strings.Trim(lit.Value, "\"`")
		if val != "" && !strings.Contains(val, "──") {
			fmt.Printf("[%s:%d] Call %s() has hardcoded arg: %s\n", path, pos.Line, funcName, lit.Value)
		}
	}
}
