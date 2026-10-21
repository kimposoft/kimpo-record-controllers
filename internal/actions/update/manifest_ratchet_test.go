package update

// manifest_ratchet_test.go 是 AST 选择器棘轮，与 create/manifest_ratchet_test.go 同款判据：
// 钉死 update 包生产代码的调用面——只解析一次写入令牌、只经一次 SDK ApplyMutation 呈递，
// 旧版回落链路的选择器一律"禁止出现"。宿主侧有同类约束，但插件是独立 Go 模块，必须在本仓库内
// 自守。
import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

func TestUpdateProductionSelectorCallCounts(t *testing.T) {
	raw, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	for _, retired := range [][]byte{
		[]byte("alice_command_update_rowset_sealed_ref"),
		[]byte("alice_command_update_rowset_content_hash"),
		[]byte("alice_plan_grant"),
		[]byte("record_grant"),
	} {
		if bytes.Contains(raw, retired) {
			t.Fatalf("UPDATE production retains retired query_context key %q", retired)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", raw, 0)
	if err != nil {
		t.Fatalf("parse service.go: %v", err)
	}
	counts := map[string]int{}
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			counts[selector.Sel.Name]++
		}
		return true
	})
	if counts["ParseMutationTicket"] != 1 || counts["ApplyMutation"] != 1 {
		t.Fatalf("UPDATE must parse one mutation ticket and call one SDK Apply, got=%v", counts)
	}
	// 旧逻辑分支（定位/取值查询 SubmitExpression/SubmitExpressionStaging +
	// host.Record().UpdateRecords 写库）与插件侧 Derive/Seal/Apply 均已退役，相关选择器
	// "禁止出现"。
	for _, legacy := range []string{"LegacyMutationTicket", "ParseAlicePlanRef", "DeriveChangeSet", "SealChangeSet", "ApplyChangeSet", "SubmitExpression", "SubmitExpressionStaging", "UpdateRecords"} {
		if counts[legacy] != 0 {
			t.Fatalf("UPDATE must not use retired legacy selector %q, inventory=%v", legacy, counts)
		}
	}
	if counts["DeleteRecords"] != 0 {
		t.Fatalf("UPDATE must never call DeleteRecords, got=%v", counts)
	}
}
