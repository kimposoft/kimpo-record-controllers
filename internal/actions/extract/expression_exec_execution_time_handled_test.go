package extract

// expression_exec_execution_time_handled_test.go 是插件侧「宿主已处理」短路分支的判据：
// 宿主经 query_context 键 alice_record_extract_execution_time_handled 回执"本次取数已由宿主
// 完整处理"。
//
// 旧格式节点（params.expression）在 Execute 入口即具名拒绝（EXTRACT_EXPRESSION_LEGACY_RETIRED），
// 不享受回执短路，见 TestExecute_LegacyExpression_RejectedEvenWithHandledReceipt；
// V4 节点的回执短路见下方用例。
//
// fakeQuery/fakeMirror/fakeHost/mustStruct 定义于同包 service_test.go——本文件复用，不重复声明。

import (
	"context"
	"testing"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

// handledExecReqCtx 构造一份 3.4 旧格式（params.expression）"普通非暂存、单一 Fill 目标"的
// 请求，query_context 额外可选携带 alice_record_extract_execution_time_handled 键
// （handledPayload 为 nil 时不携带，即"键缺席"配置）。仅用于断言旧格式节点被具名拒绝。
func handledExecReqCtx(t *testing.T, handledPayload map[string]any) *bizpb.ExecuteRequest {
	t.Helper()
	qc := map[string]any{"sheet_session_id": "sess-1", "main_table_id": "tbl-main", "write_grant": "wg"}
	if handledPayload != nil {
		qc["alice_record_extract_execution_time_handled"] = handledPayload
	}
	return &bizpb.ExecuteRequest{Context: mustStruct(t, map[string]any{
		"params": map[string]any{
			"expression": map[string]any{
				"schema_version": float64(1),
				"source":         map[string]any{"tables": []any{map[string]any{"alias": "t1"}}, "joins": []any{}},
				"target": map[string]any{
					"target_data_table_id": "tbl-main",
					"outputs":              []any{map[string]any{"target_field_id": "f_main", "fill_strategy": "overwrite"}},
				},
			},
		},
		"query_context": qc,
	})}
}

// TestExecute_LegacyExpression_RejectedEvenWithHandledReceipt 3.4 旧格式节点
// （params 带 expression）即便 query_context 携带宿主 handled 回执，也必须在任何 Host IO 之前
// 以 EXTRACT_EXPRESSION_LEGACY_RETIRED 具名拒绝——回执短路只属于 V4 形态；无回执时同样拒绝。
func TestExecute_LegacyExpression_RejectedEvenWithHandledReceipt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
	}{
		{name: "handled receipt present", payload: map[string]any{"handled": true, "filled_target_count": float64(1), "row_count": float64(3), "plan_digest": "digest-1"}},
		{name: "receipt absent", payload: nil},
	} {
		fq := &fakeQuery{}
		fm := &fakeMirror{}
		ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})
		resp, err := New().Execute(ctx, handledExecReqCtx(t, tc.payload))
		if err != nil {
			t.Fatalf("%s: Execute transport error: %v", tc.name, err)
		}
		if resp.GetSuccess() || resp.GetErrorCode() != publicSchemaCodeExpressionLegacyRetired {
			t.Fatalf("%s: legacy expression node must be rejected with %s, got %+v", tc.name, publicSchemaCodeExpressionLegacyRetired, resp)
		}
		if resp.GetMessageKey() != "extract.public_schema.expression_legacy_retired" {
			t.Fatalf("%s: message_key = %q", tc.name, resp.GetMessageKey())
		}
		if len(fq.gotModel) != 0 || len(fm.writes) != 0 || fm.appends != 0 {
			t.Fatalf("%s: rejection must happen before Host I/O: query=%q writes=%d appends=%d", tc.name, fq.gotModel, len(fm.writes), fm.appends)
		}
	}
}

// ── V4 节点的短路 ───────────────────────────────────────────────

// v4HandledExecReqCtx 构造一份 **V4 取数节点** 的请求：params 里只有 extract_v4、
// 没有 expression（这正是设计器新配节点保存出来的形状），query_context 携带宿主的
// handled 回执。
func v4HandledExecReqCtx(t *testing.T, handledPayload map[string]any) *bizpb.ExecuteRequest {
	t.Helper()
	qc := map[string]any{"sheet_session_id": "sess-1", "main_table_id": "tbl-main", "write_grant": "wg"}
	if handledPayload != nil {
		qc["alice_record_extract_execution_time_handled"] = handledPayload
	}
	return &bizpb.ExecuteRequest{Context: mustStruct(t, map[string]any{
		"params":        map[string]any{"extract_v4": map[string]any{"schema_version": extractV4SchemaVersion}},
		"query_context": qc,
	})}
}

// 🔴 宿主已完整处理并写回表单数据层（Handled=true、query_context 带回执）时，V4 节点的
// Execute 必须**短路返回成功**。宿主 handler 始终会调用插件 Execute，只在 Handled 时额外注入
// 这个回执键，指望插件自己短路；若落到 EXTRACT_HOST_HANDLED_ONLY，就会出现「数据已写入，
// 动作却对外报失败」的部分成功状态，用户一重试就重复写。
func TestExecute_V4Params_RecordExtractExecutionTimeHandled_ShortCircuits(t *testing.T) {
	fq := &fakeQuery{}
	fm := &fakeMirror{}
	svc := New()
	ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})

	resp, err := svc.Execute(ctx, v4HandledExecReqCtx(t, map[string]any{
		"handled": true, "filled_target_count": float64(1), "row_count": float64(6), "plan_digest": "digest-v4",
	}))
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if !resp.GetSuccess() {
		t.Fatalf("宿主已 Handled 时 V4 节点必须短路成功，实得 code=%q msg=%q", resp.GetErrorCode(), resp.GetMessage())
	}
	// 回执 row_count>0 ⇒ outcome=filled。
	if got := resp.GetResult().GetFields()["outcome"].GetStringValue(); got != "filled" {
		t.Fatalf("outcome = %q, want filled（回执 row_count=6）", got)
	}
	// 短路必须发生在任何 RPC 之前：宿主已经写过一次表单数据层，插件再写一遍会在
	// append 语义下把结果翻倍。
	if len(fq.gotModel) != 0 || len(fm.writes) != 0 || fm.appends != 0 {
		t.Fatalf("短路后不得触达 Host I/O: query=%q writes=%d appends=%d", fq.gotModel, len(fm.writes), fm.appends)
	}
}

// 哨兵必须保留：没有 handled 回执（宿主没处理却调了插件）时，仍然报具名错误，
// 这才是 EXTRACT_HOST_HANDLED_ONLY 当初被设计出来的用途——让"路由误调用"可观测。
func TestExecute_V4Params_WithoutHandledReceipt_KeepsSentinel(t *testing.T) {
	fq := &fakeQuery{}
	fm := &fakeMirror{}
	svc := New()
	ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})

	resp, err := svc.Execute(ctx, v4HandledExecReqCtx(t, nil))
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != "EXTRACT_HOST_HANDLED_ONLY" {
		t.Fatalf("无回执时必须保留具名哨兵，实得 %+v", resp)
	}
	// handled=false 的回执同样不短路（严格看字段本身，不因键存在就短路）。
	falseResp, err := svc.Execute(ctx, v4HandledExecReqCtx(t, map[string]any{"handled": false}))
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if falseResp.GetErrorCode() != "EXTRACT_HOST_HANDLED_ONLY" {
		t.Fatalf("handled=false 时必须保留哨兵，实得 %+v", falseResp)
	}
}
