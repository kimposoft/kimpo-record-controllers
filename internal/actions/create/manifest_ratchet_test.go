package create

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// TestCreateProductionUsesOnlyMutationTicket 锁定 Create 的唯一写面：解析写入令牌
// MutationTicket 后只调用一次 SDK Apply facade。旧 plan-ref、裸 sealed/hash、插件侧三段式和
// RecordWrite/ActionQuery 选择器全部禁止，避免删除旧测试后形成假绿。
func TestCreateProductionUsesOnlyMutationTicket(t *testing.T) {
	raw, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	for _, retired := range [][]byte{
		[]byte("alice_plan_ref"),
		[]byte("alice_plan_grant"),
		[]byte("alice_command_create_rowset_sealed_ref"),
		[]byte("alice_command_create_rowset_content_hash"),
	} {
		if bytes.Contains(raw, retired) {
			t.Fatalf("CREATE production retains retired query_context key %q", retired)
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
		t.Fatalf("CREATE must parse one mutation ticket and call one SDK Apply, got=%v", counts)
	}
	for _, retired := range []string{
		"ParseAlicePlanRef", "LegacyMutationTicket", "DeriveChangeSet", "SealChangeSet", "ApplyChangeSet",
		"SubmitExpression", "SubmitExpressionStaging", "CreateRecords", "ExecuteRecordCommandGroup",
		"TemplateCatalog", "Query", "Record",
	} {
		if counts[retired] != 0 {
			t.Fatalf("CREATE must not use retired selector %q, inventory=%v", retired, counts)
		}
	}
}

func TestRecordControllerManifestIsLeastPrivilege(t *testing.T) {
	const manifestPath = "../../../manifest.json"

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	// 根 manifest.json 是唯一权威：根包 manifest.go 直接 embed 它，cmd/plugin/manifest.json 构建期
	// 副本已退役。副本若仍存在，说明是旧构建方式的残留——没人再同步它，是静默漂移的第二事实源，
	// 故判据为「必须不存在」。
	const embeddedManifestPath = "../../../cmd/plugin/manifest.json"
	if _, embeddedErr := os.Stat(embeddedManifestPath); embeddedErr == nil {
		t.Fatalf("retired embedded manifest copy still exists: path=%s (delete it; root manifest.json is embedded directly)", embeddedManifestPath)
	} else if !os.IsNotExist(embeddedErr) {
		t.Fatalf("stat retired embedded manifest copy: path=%s err=%v", embeddedManifestPath, embeddedErr)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	compatibility, ok := manifest["schema_compatibility"].(map[string]any)
	if !ok {
		t.Fatalf("%s schema_compatibility missing", manifestPath)
	}
	alice, ok := compatibility["host.alice"].(map[string]any)
	// host.alice 按 Kimpo 1.0 基线声明代际 1。
	if !ok || alice["min_version"] != float64(1) {
		t.Fatalf("%s host.alice=%v want min_version exact 1", manifestPath, alice)
	}
	for _, retiredCapability := range []string{"host.actionquery", "host.recordwrite", "host.template-catalog"} {
		if _, exists := compatibility[retiredCapability]; exists {
			t.Fatalf("%s retains retired capability %q", manifestPath, retiredCapability)
		}
	}

	counts := map[string]int{}
	for _, rpc := range manifest["api_scope"].(map[string]any)["allowed_rpcs"].([]any) {
		counts[rpc.(string)]++
	}
	// IssueRecordExtract/QueryRecordExtract 已从 SDK 删除，列入下方退役清单（必须为 0）。
	for _, required := range []string{
		"kimpo.plugins.host.v1.AliceDataService/ApplyChangeSet",
	} {
		if counts[required] != 1 {
			t.Fatalf("%s current RPC %q count=%d", manifestPath, required, counts[required])
		}
	}
	for _, retired := range []string{
		"kimpo.plugins.host.v1.AliceDataService/IssueRecordExtract",
		"kimpo.plugins.host.v1.AliceDataService/QueryRecordExtract",
		"kimpo.plugins.host.v1.AliceDataService/DeriveChangeSet",
		"kimpo.plugins.host.v1.AliceDataService/SealChangeSet",
		"kimpo.plugins.host.v1.TemplateCatalogService/ListFields",
		"kimpo.plugins.host.v1.ActionQueryService/SubmitExpression",
		"kimpo.plugins.host.v1.RecordWriteService/ExecuteRecordCommandGroup",
		"kimpo.plugins.host.v1.RecordWriteService/UpdateRecords",
		"kimpo.plugins.host.v1.RecordWriteService/DeleteRecords",
	} {
		if counts[retired] != 0 {
			t.Fatalf("%s retains retired RPC %q count=%d", manifestPath, retired, counts[retired])
		}
	}
}
