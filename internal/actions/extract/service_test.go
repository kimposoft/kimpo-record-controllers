package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

// —— Execute 全程（fake host） ——

// fakeQuery 回放固定结果，并记录收到的取数请求，用于验证 Execute 不触达 Host I/O。
//
// plugin.QueryClient 接口已不声明 SubmitQuery，下方 TestExecute_LegacyKimpoSQLFailsClosedBeforeHostIO
// 断言"legacy KimpoSQL 从不触达 Host I/O"因此是**编译期保证**（接口里没有这个方法可调）。
type fakeQuery struct {
	gotCtx      []byte
	gotModel    []byte
	rows        []map[string]any
	exprColumns []string
	err         error
}

func (f *fakeQuery) SubmitExpression(_ context.Context, modelJSON []byte, triggerCtx []byte, params []byte) ([]byte, []byte, error) {
	f.gotModel = modelJSON
	f.gotCtx = triggerCtx
	if f.err != nil {
		return nil, nil, f.err
	}
	rowsJSON, _ := json.Marshal(f.rows)
	colsJSON, _ := json.Marshal(f.exprColumns)
	return rowsJSON, colsJSON, nil
}

// writtenField 记录一次 Mirror.WriteField 调用，用于断言写回坐标 + 值。
type writtenField struct {
	sessionID, dataTableID, rowKey, fieldID, writeGrant string
	typedValue                                          []byte
}

// fakeMirror 记录 WriteField 调用；其余 MirrorClient 方法为提取不用的空实现。
type fakeMirror struct {
	writes    []writtenField
	err       error // 注入写回失败
	appends   int   // AppendDetailRow 调用次数（明细写路径断言）
	readValue []byte
	readFound bool
	readErr   error
	reads     []writtenField
	// 空行复用测试用：existingRows = ListDetailRows 返回的现有行；rowFieldRaw[rowKey|fieldID] = 该行该字段
	// 现值（仅出现在 map 里且非空串才视为非空，用于 ReadFields 判空）。
	existingRows []string
	rowFieldRaw  map[string]string
	// 左驱 left join 测试用：tableSnapshot = ReadTable 返回的行快照 JSON；reordered 捕获 ReorderDetailRows 入参。
	tableSnapshot []byte
	reordered     []string
}

func (m *fakeMirror) WriteField(_ context.Context, sessionID, dataTableID, rowKey, fieldID string, typedValue []byte, writeGrant string) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	m.writes = append(m.writes, writtenField{sessionID, dataTableID, rowKey, fieldID, writeGrant, typedValue})
	return int64(len(m.writes)), nil
}
func (m *fakeMirror) WriteFields(ctx context.Context, sessionID string, fields []plugin.WriteFieldValue, writeGrant string) ([]plugin.WriteFieldResult, error) {
	results := make([]plugin.WriteFieldResult, 0, len(fields))
	appendedRows := map[string]string{}
	for _, field := range fields {
		rowKey := field.RowKey
		if field.AppendRow {
			var ok bool
			rowKey, ok = appendedRows[field.DataTableID+"\x00"+field.ClientRowKey]
			if !ok {
				rowKey = m.allocatedRowKey()
				appendedRows[field.DataTableID+"\x00"+field.ClientRowKey] = rowKey
			}
		}
		rev, err := m.WriteField(ctx, sessionID, field.DataTableID, rowKey, field.FieldID, field.TypedValue, writeGrant)
		if err != nil {
			return nil, err
		}
		results = append(results, plugin.WriteFieldResult{
			DataTableID:     field.DataTableID,
			RowKey:          rowKey,
			FieldID:         field.FieldID,
			AppliedRevision: rev,
		})
	}
	return results, nil
}
func (m *fakeMirror) SyncFields(context.Context, string, int64, []byte) (int64, error) { return 0, nil }

// SyncFieldsStream 空实现：提取动作本身不走 editor 的分片投影通道，此处仅为满足 plugin.MirrorClient 接口完整性。
func (m *fakeMirror) SyncFieldsStream(context.Context, string, int64, plugin.MirrorDeltaChunkSource) (int64, error) {
	return 0, nil
}
func (m *fakeMirror) ReportCalcState(context.Context, string, int64, []byte) error { return nil }
func (m *fakeMirror) BuildPayload(context.Context, string) ([]byte, int64, error)  { return nil, 0, nil }
func (m *fakeMirror) CommitSaveResult(context.Context, string, []byte) error       { return nil }
func (m *fakeMirror) ReadField(_ context.Context, sessionID, dataTableID, rowKey, fieldID string, waitSettled bool) (bool, []byte, error) {
	m.reads = append(m.reads, writtenField{sessionID: sessionID, dataTableID: dataTableID, rowKey: rowKey, fieldID: fieldID})
	if m.readErr != nil {
		return false, nil, m.readErr
	}
	return m.readFound, m.readValue, nil
}
func (m *fakeMirror) ReadFields(_ context.Context, _ string, refs []plugin.ReadFieldRef, _ bool) ([]plugin.ReadFieldValue, error) {
	out := make([]plugin.ReadFieldValue, 0, len(refs))
	for _, ref := range refs {
		raw, ok := m.rowFieldRaw[ref.RowKey+"|"+ref.FieldID]
		v := plugin.ReadFieldValue{DataTableID: ref.DataTableID, RowKey: ref.RowKey, FieldID: ref.FieldID}
		if ok {
			v.Found = true
			v.TypedValue, _ = json.Marshal(map[string]any{"raw": raw, "text": raw})
		}
		out = append(out, v)
	}
	return out, nil
}

// allocatedRowKey 生成「追加」行的稳定 rowKey（row:0、row:1…，与原 AppendDetailRow 语义一致）。
func (m *fakeMirror) allocatedRowKey() string {
	rowKey := fmt.Sprintf("row:%d", m.appends)
	m.appends++
	return rowKey
}
func (m *fakeMirror) AppendDetailRow(context.Context, string, string, string) (string, error) {
	return m.allocatedRowKey(), nil
}
func (m *fakeMirror) AllocateDetailRow(context.Context, string, string, string) (string, error) {
	return m.allocatedRowKey(), nil
}

// insertIndex（第 5 参）/ requestedRowKeys（第 6 参）是 SDK MirrorClient 的分配参数：
// insertIndex<0 = 追加末尾，requestedRowKeys 为空 = 由宿主生成新 rowKey。
// 本 fake 不关心插入位/请求行键，故忽略这两参。
func (m *fakeMirror) AllocateDetailRows(_ context.Context, _, _ string, count int, _ int, _ []string, _ string) ([]string, error) {
	keys := make([]string, 0, count)
	for i := 0; i < count; i++ {
		keys = append(keys, m.allocatedRowKey())
	}
	return keys, nil
}
func (m *fakeMirror) RemoveDetailRow(context.Context, string, string, string, string) error {
	return nil
}
func (m *fakeMirror) MarkRowDeleted(context.Context, string, string, string, string) error {
	return nil
}
func (m *fakeMirror) MarkRowsDeleted(context.Context, string, string, []string, string) error {
	return nil
}
func (m *fakeMirror) ListDetailRows(context.Context, string, string) ([]string, error) {
	return m.existingRows, nil
}

// MirrorClient 读表/重排方法的桩（提取这些用例不走排序路径）。
func (m *fakeMirror) ReadTable(context.Context, string, string) ([]byte, error) {
	return m.tableSnapshot, nil
}
func (m *fakeMirror) ReorderDetailRows(_ context.Context, _, _ string, ordered []string, _ string) error {
	m.reordered = append([]string(nil), ordered...)
	return nil
}

// fakeHost 提供 Query + Mirror 子客户端（提取取数 + 写回用），其余出站方向返回 nil。
// 嵌入 plugin.Host 接口零值兜底其余方法（SDK Host 出站面持续增长，逐一手写 nil stub 每次扩面
// 必编译断裂；本包动作绝不应触达未覆写的方法，触达即 nil panic 使测试失败）。
type fakeHost struct {
	plugin.Host
	q plugin.QueryClient
	m plugin.MirrorClient
}

func (h *fakeHost) Template() plugin.TemplateClient { return nil }
func (h *fakeHost) Cache() plugin.CacheClient       { return nil }
func (h *fakeHost) Asset() plugin.AssetClient       { return nil }
func (h *fakeHost) Event() plugin.EventClient       { return nil }
func (h *fakeHost) Log() plugin.LogClient           { return nil }
func (h *fakeHost) Logger() plugin.Logger           { return nil }

func (h *fakeHost) Query() plugin.QueryClient           { return h.q }
func (h *fakeHost) Mirror() plugin.MirrorClient         { return h.m }
func (h *fakeHost) Capability() plugin.CapabilityClient { return nil }

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return s
}

func TestExecute_LegacyKimpoSQLFailsClosedBeforeHostIO(t *testing.T) {
	fq := &fakeQuery{rows: []map[string]any{{"name": "甲"}, {"name": "乙"}}}
	fm := &fakeMirror{}
	svc := New()
	reqCtx := mustStruct(t, map[string]any{
		"params": map[string]any{
			"kimpo_sql":      "SELECT name FROM t WHERE dept = :dept",
			"param_bindings": map[string]any{"dept": "field:dept_code"},
			"targets": []any{
				map[string]any{"field_id": "f_names", "column": "name", "strategy": "concat"},
			},
		},
		"trigger": map[string]any{"values": map[string]any{"dept_code": "D100"}},
		// 写回坐标 + 写权限凭据从 query_context 取（宿主 buildExecuteContext 透传）。
		"query_context": map[string]any{
			"app_id": "app1", "template_id": "tpl1", "action_id": "n1",
			"sheet_session_id": "sess-1", "main_table_id": "tbl-main", "write_grant": "wg-token",
		},
	})

	ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})
	resp, err := svc.Execute(ctx, &bizpb.ExecuteRequest{Context: reqCtx})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != publicSchemaCodeKimpoSQLRetired || len(fm.writes) != 0 || len(fq.gotModel) != 0 {
		t.Fatalf("legacy KimpoSQL was not rejected pre-I/O: response=%+v writes=%d", resp, len(fm.writes))
	}
	if strings.Contains(resp.GetMessage(), "SELECT") || strings.Contains(resp.GetMessage(), "D100") || strings.Contains(resp.GetMessage(), "wg-token") {
		t.Fatalf("legacy KimpoSQL rejection leaked business or credential content: %+v", resp)
	}
}

func retiredPublicSchemaExpressionCases() []struct {
	name       string
	expression map[string]any
	code       string
	fieldPath  string
} {
	const secretKey = "Cookie-auth_session-lease-route-attach-business-secret"
	return []struct {
		name       string
		expression map[string]any
		code       string
		fieldPath  string
	}{
		{name: "nested raw", expression: map[string]any{secretKey: map[string]any{"args": []any{map[string]any{"kind": "raw", "text": "SELECT never-echo-this 业务值"}}}}, code: publicSchemaCodeRawNodeRetired, fieldPath: "expression.*.args[0].kind"},
		{name: "nested post sort", expression: map[string]any{secretKey: []any{map[string]any{"post_sort": nil}}}, code: publicSchemaCodeInlinePostSortRetired, fieldPath: "expression.*[0].post_sort"},
		{name: "nested take v1", expression: map[string]any{secretKey: map[string]any{"take-v1": "lease-token-never-echo"}}, code: publicSchemaCodeTakeV1Retired, fieldPath: "expression.*.take-v1"},
		{name: "take v1 selector", expression: map[string]any{secretKey: map[string]any{"pushdown_face": "RECORD_EXTRACT_TAKE_V1"}}, code: publicSchemaCodeTakeV1Retired, fieldPath: "expression.*.pushdown_face"},
		{name: "nested KimpoSQL", expression: map[string]any{secretKey: map[string]any{"kimpo_sql": "SELECT attach-token-never-echo"}}, code: publicSchemaCodeKimpoSQLRetired, fieldPath: "expression.*.kimpo_sql"},
	}
}

func assertNoRetiredPayloadLeak(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{"never-echo", "SELECT", "业务值", "Cookie", "auth_session", "lease-token", "route-token", "attach-token", "business-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("retired public schema rejection leaked %q: %s", secret, text)
		}
	}
}

func TestExecuteRetiredPublicSchemaFailsBeforeHostIO(t *testing.T) {
	for _, test := range retiredPublicSchemaExpressionCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fq, fm := &fakeQuery{}, &fakeMirror{}
			ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})
			req := &bizpb.ExecuteRequest{
				Context: mustStruct(t, map[string]any{
					"params":        map[string]any{"expression": test.expression},
					"query_context": map[string]any{"sheet_session_id": "auth_session-never-echo", "write_grant": "route-token-never-echo"},
				}),
				TriggerContext: &bizpb.TriggerContextMsg{Locale: "en-US"},
			}
			resp, err := New().Execute(ctx, req)
			if err != nil {
				t.Fatalf("Execute transport error: %v", err)
			}
			// 插件无 3.4 表达式执行路径，任何旧格式 expression 节点（无论内含哪种
			// 已退役形状）在 Execute 入口统一以 EXTRACT_EXPRESSION_LEGACY_RETIRED 具名拒绝；
			// 细分退役码（raw/post_sort/take-v1/kimpo_sql）仍由 ValidateParams 保存期给出。
			want := publicSchemaCodeExpressionLegacyRetired
			if resp.GetSuccess() || resp.GetErrorCode() != want || resp.GetMessage() != tr.T("en-US", publicSchemaMessageKey(want), nil) {
				t.Fatalf("response=%+v, want code=%s", resp, want)
			}
			assertNoRetiredPayloadLeak(t, resp.String())
			if len(fq.gotModel) != 0 || len(fq.gotCtx) != 0 || len(fm.writes) != 0 || len(fm.reads) != 0 || fm.appends != 0 {
				t.Fatalf("retired shape reached Host I/O: query=%q context=%q writes=%d reads=%d appends=%d", fq.gotModel, fq.gotCtx, len(fm.writes), len(fm.reads), fm.appends)
			}
		})
	}
}

func TestValidateParams(t *testing.T) {
	svc := New()
	// 取数节点文档 v4（schema_version 与 extractV4SchemaVersion 一致）是唯一被 schema 接纳的形态——
	// ValidateParams 只做浅校验（object + schema_version），深层 sources/joins/targets 语义归宿主
	// 保存检查。
	ok := mustStruct(t, map[string]any{
		"extract_v4": map[string]any{
			"schema_version": extractV4SchemaVersion,
			"query":          map[string]any{"sources": []any{}, "joins": []any{}, "options": map[string]any{"distinct": false}},
			"targets":        []any{},
		},
	})
	if resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: ok}); !resp.GetValid() {
		t.Fatalf("expected valid, got %v", resp.GetErrors())
	}
	// 3.4 旧 expression 键（无论内容）已不在 schema 白名单内，落回通用 required/additionalProperties
	// 拒绝——具名的「旧格式已停用」提示归宿主保存检查负责。
	legacy := mustStruct(t, map[string]any{"expression": map[string]any{"schema_version": float64(1)}})
	if resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: legacy}); resp.GetValid() {
		t.Fatalf("legacy expression payload must be rejected once extract_v4 becomes the only accepted key, got %v", resp)
	}
	// Historical KimpoSQL shape is absent from the schema and fails closed.
	bad := mustStruct(t, map[string]any{
		"kimpo_sql": "SELECT 1",
		"targets":   []any{map[string]any{"field_id": "f", "column": "c"}},
	})
	resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: bad})
	if resp.GetValid() || len(resp.GetErrors()) != 1 || resp.GetErrors()[0].GetFieldPath() != "kimpo_sql" ||
		resp.GetErrors()[0].GetMessage() != "["+publicSchemaCodeKimpoSQLRetired+"] "+trDefault(publicSchemaMessageKey(publicSchemaCodeKimpoSQLRetired), nil) {
		t.Fatalf("expected legacy shape rejection, got %v", resp.GetErrors())
	}
	assertNoRetiredPayloadLeak(t, resp.String())
}

// TestValidateParams_ExtractV4PlansSiblingKeyAccepted：宿主在模板发布时把冻结好的发布物钉入键
// 写进 params 的**兄弟键** extract_v4_plans（不进 extract_v4 内部）；schema 是
// additionalProperties:false，本用例锁住该键被「放行」，并顺带覆盖两种形状不对的拒绝。
func TestValidateParams_ExtractV4PlansSiblingKeyAccepted(t *testing.T) {
	svc := New()
	baseV4 := map[string]any{
		"schema_version": extractV4SchemaVersion,
		"query":          map[string]any{"sources": []any{}, "joins": []any{}, "options": map[string]any{"distinct": false}},
		"targets":        []any{},
	}

	// 正形态：数组 + 元素为对象（target_id/chart_id 二选一 + publication_id），插件不解释内容。
	withPlans := mustStruct(t, map[string]any{
		"extract_v4": baseV4,
		"extract_v4_plans": []any{
			map[string]any{"target_id": "t1", "publication_id": "pub_abc123"},
			map[string]any{"chart_id": "c1", "publication_id": "pub_def456"},
		},
	})
	if resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: withPlans}); !resp.GetValid() {
		t.Fatalf("expected extract_v4_plans sibling key to pass validation, got %v", resp.GetErrors())
	}

	// 形状不对（顶层不是数组）：落回① schema 层的通用类型校验，具名拒绝。
	badShape := mustStruct(t, map[string]any{
		"extract_v4":       baseV4,
		"extract_v4_plans": map[string]any{"target_id": "t1", "publication_id": "pub_abc123"},
	})
	if resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: badShape}); resp.GetValid() {
		t.Fatalf("expected a non-array extract_v4_plans to be rejected, got valid")
	}

	// 数组但元素不是对象：① schema 层看不出问题（顶层类型是 array），须靠③ 的元素浅校验拦截。
	badElement := mustStruct(t, map[string]any{
		"extract_v4":       baseV4,
		"extract_v4_plans": []any{"not-an-object"},
	})
	resp, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: badElement})
	if resp.GetValid() || len(resp.GetErrors()) != 1 || resp.GetErrors()[0].GetFieldPath() != "extract_v4_plans" ||
		resp.GetErrors()[0].GetMessage() != trDefault("extract.validate.extract_v4_plans_shape_invalid", nil) {
		t.Fatalf("expected named extract_v4_plans shape rejection, got %v", resp.GetErrors())
	}
}

func TestValidateParamsRetiredPublicSchemaReturnsStableCodesBeforeTypedDecode(t *testing.T) {
	for _, test := range retiredPublicSchemaExpressionCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			resp, err := New().ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{
				Params: mustStruct(t, map[string]any{"expression": test.expression}),
			})
			if err != nil {
				t.Fatalf("ValidateParams transport error: %v", err)
			}
			if resp.GetValid() || len(resp.GetErrors()) != 1 {
				t.Fatalf("response=%+v", resp)
			}
			got := resp.GetErrors()[0]
			if got.GetFieldPath() != test.fieldPath || got.GetMessage() != "["+test.code+"] "+trDefault(publicSchemaMessageKey(test.code), nil) {
				t.Fatalf("validation error=%+v, want code=%s path=%s", got, test.code, test.fieldPath)
			}
			assertNoRetiredPayloadLeak(t, resp.String())
		})
	}
}

// TestValidateParams_ExpressionAtomicModesFailAtSaveTime：3.4 "expression" 键的深层原子模式校验
// （多真实目标/明细清空/明细行匹配/角色缺失诊断）归宿主保存检查，插件侧 ValidateParams 不重复
// 实现。本测试锁定：这些 legacy "expression" payload（无论内容）在 schema 下统一因
// required(extract_v4)+additionalProperties(expression) 被拒绝，与内容无关——唯一例外是
// "inline post sort" 用例，它命中的是前置的「已退役公开形态」扫描（detectRetiredExpressionValue）。
//
// 旧格式配置若越过宿主网关误达 Execute，会在入口被具名拒绝，见
// TestExecute_LegacyExpression_RejectedEvenWithHandledReceipt。
func TestValidateParams_ExpressionAtomicModesFailAtSaveTime(t *testing.T) {
	baseTarget := func() map[string]any {
		return map[string]any{
			"target_data_table_id": "tbl-main",
			"target_table_role":    "main",
			"outputs": []any{
				map[string]any{"target_field_id": "f_a", "fill_strategy": "overwrite"},
			},
		}
	}
	baseExpression := func(target map[string]any) map[string]any {
		return map[string]any{
			"schema_version": float64(1),
			"source":         map[string]any{"tables": []any{map[string]any{"alias": "t1"}}, "joins": []any{}},
			"target":         target,
		}
	}

	tests := []struct {
		name        string
		expression  func() map[string]any
		fieldPath   string
		messageKey  string
		messageCode string
		// legacySchemaRejected=true：预期落入新 schema 的通用 required/additionalProperties 拒绝
		// （2 条错误，fieldPath/messageKey 字段此时不生效）；false：仍走独立的 retired-shape 扫描
		// （1 条错误，字段生效）。
		legacySchemaRejected bool
	}{
		{
			name: "multiple real targets",
			expression: func() map[string]any {
				first := baseTarget()
				second := baseTarget()
				second["target_data_table_id"] = "tbl-other"
				second["target_table_role"] = "detail"
				expr := baseExpression(first)
				expr["targets"] = []any{first, second}
				return expr
			},
			legacySchemaRejected: true,
		},
		{
			name: "detail clear",
			expression: func() map[string]any {
				target := baseTarget()
				target["target_data_table_id"] = "tbl-detail"
				target["target_table_role"] = "detail"
				target["clear"] = map[string]any{"mode": "mapped", "timing": "before"}
				return baseExpression(target)
			},
			legacySchemaRejected: true,
		},
		{
			name: "inline post sort",
			expression: func() map[string]any {
				target := baseTarget()
				target["post_sort"] = []any{map[string]any{"field_id": "f_a", "order": "asc"}}
				return baseExpression(target)
			},
			fieldPath:   "expression.target.post_sort",
			messageKey:  "extract.public_schema.inline_post_sort_retired",
			messageCode: publicSchemaCodeInlinePostSortRetired,
		},
		{
			name: "detail row match",
			expression: func() map[string]any {
				target := baseTarget()
				target["target_data_table_id"] = "tbl-detail"
				target["target_table_role"] = "detail"
				target["auto_row_match"] = true
				return baseExpression(target)
			},
			legacySchemaRejected: true,
		},
		{
			name: "legacy role-sensitive config has migration diagnostic",
			expression: func() map[string]any {
				target := baseTarget()
				delete(target, "target_table_role")
				target["clear"] = map[string]any{"mode": "mapped", "timing": "before"}
				return baseExpression(target)
			},
			legacySchemaRejected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := New().ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{
				Params: mustStruct(t, map[string]any{"expression": tc.expression()}),
			})
			if tc.legacySchemaRejected {
				// 新 schema 通用拒绝：缺 extract_v4 + 不接受 expression，与内容无关。
				if resp.GetValid() || len(resp.GetErrors()) != 2 {
					t.Fatalf("expected generic v4-schema rejection (2 errors), got valid=%t errors=%+v", resp.GetValid(), resp.GetErrors())
				}
				return
			}
			if resp.GetValid() || len(resp.GetErrors()) != 1 {
				t.Fatalf("expected one save-time error, got valid=%t errors=%+v", resp.GetValid(), resp.GetErrors())
			}
			got := resp.GetErrors()[0]
			if got.GetFieldPath() != tc.fieldPath {
				t.Fatalf("field_path=%q, want %q", got.GetFieldPath(), tc.fieldPath)
			}
			want := trDefault(tc.messageKey, nil)
			if tc.messageCode != "" {
				want = "[" + tc.messageCode + "] " + want
			}
			if got.GetMessage() != want {
				t.Fatalf("message=%q, want key %s => %q", got.GetMessage(), tc.messageKey, want)
			}
		})
	}
}

// TestExecute_HostHandledOnlyForV4Params：Execute 命中 extract_v4-only（或无 kimpo_sql/expression
// 的任意）参数形状**且没有宿主 handled 回执**时，必须返回具名错误 EXTRACT_HOST_HANDLED_ONLY
// （"这是一次不该发生的误调用"，不是「旧版本不支持」语义的错误码），且不触达任何 Host I/O。
//
// 🔴 宿主 handler 始终会调用插件 Execute，只在 Handled 时经 query_context 注入回执键。本用例
// 构造的请求**不带**那个回执键（= 宿主确实没处理），哨兵在这种情形下必须保留；带回执时的短路由
// TestExecute_V4Params_RecordExtractExecutionTimeHandled_ShortCircuits 覆盖。
func TestExecute_HostHandledOnlyForV4Params(t *testing.T) {
	fq := &fakeQuery{}
	fm := &fakeMirror{}
	svc := New()
	ctx := plugin.WithHost(context.Background(), &fakeHost{q: fq, m: fm})

	req := &bizpb.ExecuteRequest{Context: mustStruct(t, map[string]any{
		"params": map[string]any{
			"extract_v4": map[string]any{"schema_version": extractV4SchemaVersion},
		},
	})}
	resp, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != "EXTRACT_HOST_HANDLED_ONLY" ||
		resp.GetMessage() != trDefault("extract.host_handled_only", nil) {
		t.Fatalf("expected EXTRACT_HOST_HANDLED_ONLY for a misrouted V4 call, got %+v", resp)
	}
	if len(fq.gotModel) != 0 || len(fm.writes) != 0 || fm.appends != 0 {
		t.Fatalf("misrouted V4 call must not touch Host I/O: query=%q writes=%d appends=%d", fq.gotModel, len(fm.writes), fm.appends)
	}

	// 参数完全为空（无 params 键，如误发的心跳式请求）时同样落到该分支——不是新的空洞例外。
	emptyResp, err := svc.Execute(ctx, &bizpb.ExecuteRequest{Context: mustStruct(t, map[string]any{})})
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if emptyResp.GetErrorCode() != "EXTRACT_HOST_HANDLED_ONLY" {
		t.Fatalf("expected EXTRACT_HOST_HANDLED_ONLY for empty params, got %+v", emptyResp)
	}
}
