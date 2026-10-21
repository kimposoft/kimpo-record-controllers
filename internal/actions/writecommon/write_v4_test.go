package writecommon

import "testing"

// TestValidateWriteV4Key 覆盖三写插件共用的 write_v4 浅校验各分支：正常通过、
// 键缺失、形状不是对象、schema_version 不符、operation 不符。三写插件 service.go 的
// ValidateParams/Execute 都直接复用本函数，故在此集中覆盖，各插件包测试只需端到端串一遍。
func TestValidateWriteV4Key(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{
			"write_v4": map[string]any{
				"schema_version": WriteV4SchemaVersion,
				"operation":      "insert",
			},
		}
	}

	if key, _ := ValidateWriteV4Key(valid(), "insert"); key != "" {
		t.Fatalf("合法配置应通过, got key=%q", key)
	}

	if key, _ := ValidateWriteV4Key(map[string]any{}, "insert"); key != "write.common.bad_params.write_v4_missing" {
		t.Fatalf("write_v4 缺失应报 write_v4_missing, got %q", key)
	}

	if key, _ := ValidateWriteV4Key(map[string]any{"write_v4": "not-an-object"}, "insert"); key != "write.common.bad_params.write_v4_missing" {
		t.Fatalf("write_v4 非对象应报 write_v4_missing, got %q", key)
	}

	badSchema := valid()
	badSchema["write_v4"].(map[string]any)["schema_version"] = "kimpo.write-node/v3"
	if key, _ := ValidateWriteV4Key(badSchema, "insert"); key != "write.common.bad_params.write_v4_missing" {
		t.Fatalf("schema_version 不符应报 write_v4_missing, got %q", key)
	}

	badOp := valid()
	badOp["write_v4"].(map[string]any)["operation"] = "update"
	key, params := ValidateWriteV4Key(badOp, "insert")
	if key != "write.common.bad_params.operation_mismatch" {
		t.Fatalf("operation 不符应报 operation_mismatch, got %q", key)
	}
	if params["got"] != "update" || params["want"] != "insert" {
		t.Fatalf("operation_mismatch 插值参数异常: %+v", params)
	}
}

// TestRejectLegacyExpression 覆盖旧格式退役分支：三写插件
// ValidateParams 必须能在 additionalProperties:false 的 schema 浅校验之前，把「只有存量
// expression 键、无 write_v4」的老节点具名拒绝成 write.common.validate.legacy_retired。
func TestRejectLegacyExpression(t *testing.T) {
	// 只有 expression：命中，FieldPath=expression。
	verr, ok := RejectLegacyExpression("zh-CN", map[string]any{
		"expression": map[string]any{"schema_version": float64(1)},
	})
	if !ok {
		t.Fatal("只有 expression 键应命中退役分支")
	}
	if verr.GetFieldPath() != "expression" {
		t.Fatalf("FieldPath 应为 expression, got %q", verr.GetFieldPath())
	}
	if verr.GetMessage() != T("zh-CN", "write.common.validate.legacy_retired", nil) {
		t.Fatalf("文案应来自 write.common.validate.legacy_retired: got=%q", verr.GetMessage())
	}

	// 两键同时存在：不命中（视为已带上合法新格式，交给 ValidateWriteV4Key 校验 write_v4 本身）。
	if _, ok := RejectLegacyExpression("zh-CN", map[string]any{
		"expression": map[string]any{},
		"write_v4":   map[string]any{},
	}); ok {
		t.Fatal("expression 与 write_v4 同时存在不应命中退役分支")
	}

	// 只有 write_v4（正常新格式）：不命中。
	if _, ok := RejectLegacyExpression("zh-CN", map[string]any{
		"write_v4": map[string]any{},
	}); ok {
		t.Fatal("只有 write_v4 不应命中退役分支")
	}

	// 两键都没有：不命中。
	if _, ok := RejectLegacyExpression("zh-CN", map[string]any{}); ok {
		t.Fatal("两键都没有不应命中退役分支")
	}

	// params 为 nil：不命中，不 panic。
	if _, ok := RejectLegacyExpression("zh-CN", nil); ok {
		t.Fatal("params 为 nil 不应命中退役分支")
	}
}

// TestWriteV4SchemaVersionConstant 钉住 schema_version 字面量镜像值，与 `@kimposoft/expression-editor`
// 共享包的 WRITE_NODE_DOCUMENT_SCHEMA_VERSION
// 必须逐字一致（两侧独立维护，靠测试防漂移；跨语言无法直接 import，只能各自钉字面量）。
func TestWriteV4SchemaVersionConstant(t *testing.T) {
	if WriteV4SchemaVersion != "kimpo.write-node/v4" {
		t.Fatalf("WriteV4SchemaVersion = %q, want kimpo.write-node/v4", WriteV4SchemaVersion)
	}
}
