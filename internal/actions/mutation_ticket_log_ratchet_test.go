package actions_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestMutationTicketsNeverReachPluginLogs 防止不透明写入令牌被写进插件日志。三项写动作可以记录
// 稳定错误码和宿主 trace，但任何 logf 调用的参数树都不得引用局部 ticket 或原始 err 变量。
func TestMutationTicketsNeverReachPluginLogs(t *testing.T) {
	for _, relative := range []string{"create/service.go", "update/service.go", "delete/service.go"} {
		raw, err := os.ReadFile(relative)
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, relative, raw, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || calledName(call.Fun) != "logf" {
				return true
			}
			for _, arg := range call.Args {
				for _, secretBearing := range []string{"ticket", "err"} {
					if containsIdent(arg, secretBearing) {
						t.Fatalf("%s:%d logf argument references secret-bearing %s value", filepath.ToSlash(relative), fileSet.Position(arg.Pos()).Line, secretBearing)
					}
				}
			}
			return true
		})
	}
}

func calledName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

func containsIdent(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if ident, ok := candidate.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}
