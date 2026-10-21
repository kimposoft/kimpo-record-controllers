package extract

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 本文件守两条退役约束：V1 查询/旧取数执行路径的函数与类型不得回到生产源码，
// manifest 不得再声明已退役的取数传输 RPC。

// productionGoFiles 返回本包全部非测试 Go 源文件（按名排序，结果稳定）。
func productionGoFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, path := range matches {
		if !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("no production go files found")
	}
	return out
}

func TestExtractProductionRetiresLegacyQueryAndDeadHelpers(t *testing.T) {
	counts := map[string]int{"SubmitQuery": 0, "SubmitExpression": 0, "SubmitExpressionStaging": 0}
	// 扫描面为本包全部现存生产文件（productionGoFiles），新增文件自动纳入。
	for _, path := range productionGoFiles(t) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.SelectorExpr:
				if _, tracked := counts[typed.Sel.Name]; tracked {
					counts[typed.Sel.Name]++
				}
			case *ast.FuncDecl:
				// 3.4 表达式取数执行路径的入口与读侧函数已退役，防止被原样加回。
				for _, retired := range []string{"applyStrategy", "resolveParamValues", "triggerValues",
					"executeExpression", "tryAliceExtractQuery", "decodeAliceQueryStream", "resolveTargetEffectPlan"} {
					if typed.Name.Name == retired {
						t.Fatalf("retired legacy helper %s remains in production", retired)
					}
				}
			case *ast.TypeSpec:
				if typed.Name.Name == "extractTarget" || typed.Name.Name == "extractParamsModel" {
					t.Fatalf("retired legacy type %s remains in production", typed.Name.Name)
				}
			}
			return true
		})
	}
	// V1 查询方法（含暂存专用的 SubmitExpressionStaging）已整体退役，生产源码中引用计数必须恒为 0。
	if counts["SubmitQuery"] != 0 || counts["SubmitExpression"] != 0 || counts["SubmitExpressionStaging"] != 0 {
		t.Fatalf("extract V1 selector inventory=%v", counts)
	}
}

// TestExtractManifestCopiesRetireRecordExtractTransport 3.4 selector-only 取数 RPC 已从 SDK 删除，
// manifest 不得再声明；cmd/plugin 下也不得出现 manifest 副本。
func TestExtractManifestCopiesRetireRecordExtractTransport(t *testing.T) {
	const manifestPath = "../../../manifest.json"

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	// 根 manifest.json 是唯一权威：根包 manifest.go 直接 embed 它。cmd/plugin/manifest.json 副本若存在，
	// 没人同步它，是静默漂移的第二事实源，故判据为「必须不存在」。
	const embeddedManifestPath = "../../../cmd/plugin/manifest.json"
	if _, embeddedErr := os.Stat(embeddedManifestPath); embeddedErr == nil {
		t.Fatalf("retired embedded manifest copy still exists: path=%s (delete it; root manifest.json is embedded directly)", embeddedManifestPath)
	} else if !os.IsNotExist(embeddedErr) {
		t.Fatalf("stat retired embedded manifest copy: path=%s err=%v", embeddedManifestPath, embeddedErr)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	compatibility := manifest["schema_compatibility"].(map[string]any)
	alice := compatibility["host.alice"].(map[string]any)
	// host.alice 按 Kimpo 1.0 基线声明代际 1。
	if minVersion, isNumber := alice["min_version"].(float64); !isNumber || minVersion != 1 {
		t.Fatalf("%s host.alice=%v (want min_version exact 1)", manifestPath, alice)
	}
	counts := map[string]int{}
	for _, rawRPC := range manifest["api_scope"].(map[string]any)["allowed_rpcs"].([]any) {
		counts[rawRPC.(string)]++
	}
	// IssueRecordExtract/QueryRecordExtract 已从 SDK 删除，与 V1 两条一样必须**不在** allowed_rpcs 里。
	for _, retired := range []string{
		"kimpo.plugins.host.v1.ActionQueryService/SubmitExpression",
		"kimpo.plugins.host.v1.ActionQueryService/SubmitQuery",
		"kimpo.plugins.host.v1.AliceDataService/IssueRecordExtract",
		"kimpo.plugins.host.v1.AliceDataService/QueryRecordExtract",
	} {
		if counts[retired] != 0 {
			t.Fatalf("%s retains retired RPC %s", manifestPath, retired)
		}
	}
}
