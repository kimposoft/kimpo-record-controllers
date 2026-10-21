package actions_test

// hostwire_import_ratchet_test.go —— 宿主 wire 类型（hostpb）的 **import 面锁**。
//
// 判据来源：插件生态对客户与第三方开放，**插件看得见的类型就是对外契约**。
// 插件直接 import `github.com/kimposoft/kimpo-plugin-sdk/gen/hostpb` 等于把宿主的 wire
// 协议变成公开 API。本锁按清单逐文件收紧：已经切到 SDK facade 的文件锁死为 0 引用；
// 暂时无法切走的文件登记为「待收窄」并附理由，防止「先记着以后弄」变成永远不弄。
//
// 🔴 两处要点：
//
//  1. **落在本仓库内**。宿主侧有同类约束，但插件是独立 Go 模块，宿主的守卫在语言层面
//     扫不到这里，所以必须在本仓库内自守。
//  2. **谓词用「*ast.ImportSpec 路径匹配」而不是「标识符名字匹配」**。`hostpb.X` 在 AST 里
//     是 SelectorExpr，其 X 是一个 *ast.Ident——而 Ident 的名字是 **import 别名，可改名规避**
//     （`import foo "…/gen/hostpb"` 之后全文再无 `hostpb` 这个标识符）。只有比对
//     ImportSpec 的 **Path** 才锁得住。
//
// ⚠️ 本锁只看**生产源码**，不看 _test.go：测试构造 wire 应答（fake 宿主）是必要的，
// 且测试不构成对外契约面。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// hostWireImportPath 是被锁的宿主 wire 包路径（生成物，插件不应直接消费）。
const hostWireImportPath = "github.com/kimposoft/kimpo-plugin-sdk/gen/hostpb"

// hostWireImportPendingFiles 是**待收窄清单**：这些生产文件目前仍直接 import 宿主 wire 包，
// 附逐条理由。清单只允许变短，不允许变长（新增条目即判失败）。
// 当前清单为空：任何生产文件直接 import 宿主 wire 包都判失败。
var hostWireImportPendingFiles = map[string]string{}

// TestHostWireImportSurfaceIsLocked 扫 internal/actions/** 下全部生产 .go 文件。
func TestHostWireImportSurfaceIsLocked(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve actions root: %v", err)
	}
	var violations []string
	seenPending := map[string]bool{}

	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" || entry.Name() == "locales" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// 解析不了的文件不作为判据（生成物/构建标签组合），跳过即可。
			return nil
		}
		for _, spec := range parsed.Imports {
			// 🔴 比对 ImportSpec.Path（字面路径），不是 spec.Name / 后续的标识符
			// ——别名可改名规避，路径不能。
			unquoted, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil || unquoted != hostWireImportPath {
				continue
			}
			if _, pending := hostWireImportPendingFiles[relative]; pending {
				seenPending[relative] = true
				continue
			}
			violations = append(violations, fmt.Sprintf("%s:%d imports %s",
				relative, fileSet.Position(spec.Pos()).Line, hostWireImportPath))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("插件生产源码直接消费宿主 wire 包（对外契约面泄漏）——请改走 SDK facade"+
			"（plugin.ApplyMutation / plugin.MutationTicket / plugin.ApplyReceipt）：\n  %s",
			strings.Join(violations, "\n  "))
	}
	// 清单只允许变短：某个待收窄文件已经不再 import 了，就必须把它从清单里删掉，
	// 否则清单会退化成一份永不核销的欠账。
	for relative, reason := range hostWireImportPendingFiles {
		if !seenPending[relative] {
			t.Fatalf("待收窄清单里的 %q 已经不再 import %s（理由登记为 %q）——请把它从 "+
				"hostWireImportPendingFiles 删掉，清单只允许变短", relative, hostWireImportPath, reason)
		}
	}
}

// TestHostWireImportRatchetCatchesAliasEvasion 反证：确认本锁的谓词不是「按标识符名字匹配」。
// 用一份临时源码模拟「给宿主 wire 包起个别名」的规避手法——按名字匹配会漏掉它，按路径匹配抓得住。
func TestHostWireImportRatchetCatchesAliasEvasion(t *testing.T) {
	source := "package x\n\nimport notthename \"" + hostWireImportPath + "\"\n\nvar _ = notthename.ApplyChangeSetRequest{}\n"
	parsed, err := parser.ParseFile(token.NewFileSet(), "alias.go", source, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	byName, byPath := false, false
	for _, spec := range parsed.Imports {
		if spec.Name != nil && spec.Name.Name == "hostpb" {
			byName = true
		}
		if unquoted, uerr := strconv.Unquote(spec.Path.Value); uerr == nil && unquoted == hostWireImportPath {
			byPath = true
		}
	}
	if byName {
		t.Fatal("fixture 构造错了：别名规避的样本不该以 hostpb 这个名字出现")
	}
	if !byPath {
		t.Fatal("路径谓词没抓到别名规避——本锁的核心判据失效")
	}
	// 顺带用同一份 AST 复核「按 *ast.Ident 名字匹配」确实抓不到（这正是不用名字谓词的原因）。
	full, err := parser.ParseFile(token.NewFileSet(), "alias.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture (full): %v", err)
	}
	hit := false
	ast.Inspect(full, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "hostpb" {
			hit = true
		}
		return true
	})
	if hit {
		t.Fatal("别名规避样本里不该存在 hostpb 标识符")
	}
}
