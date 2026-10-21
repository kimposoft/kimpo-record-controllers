package detailsort

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("structpb: %v", err)
	}
	return s
}

func validParams() map[string]any {
	return map[string]any{
		"targets": []any{
			map[string]any{"data_table_id": "dt_detail", "field_id": "f_a", "order": "asc"},
			map[string]any{"data_table_id": "dt_detail", "field_id": "f_b", "order": "desc"},
		},
	}
}

func TestGetActions(t *testing.T) {
	resp, err := New().GetActions(context.Background(), &bizpb.GetActionsRequest{})
	if err != nil {
		t.Fatalf("GetActions: %v", err)
	}
	if len(resp.GetActions()) != 1 || resp.GetActions()[0].GetName() != ActionName {
		t.Fatalf("unexpected actions: %+v", resp.GetActions())
	}
	if !resp.GetActions()[0].GetCapabilities().GetSideEffecting() {
		t.Fatal("sort_detail 应为 SideEffecting（改写表单数据层业务数据行序）")
	}
	if resp.GetActions()[0].GetCapabilities().GetExpressionBased() {
		t.Fatal("sort_detail 不应挂标准化表达式编辑器")
	}
}

func TestValidateParams(t *testing.T) {
	svc := New()

	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, validParams())}); !r.GetValid() {
		t.Fatalf("期望通过: %v", r.GetErrors())
	}

	cases := []struct {
		name   string
		mutate func(p map[string]any)
	}{
		{"缺 targets", func(p map[string]any) { delete(p, "targets") }},
		{"targets 为空", func(p map[string]any) { p["targets"] = []any{} }},
		{"缺 field_id", func(p map[string]any) {
			p["targets"] = []any{map[string]any{"data_table_id": "dt_detail", "order": "asc"}}
		}},
		{"缺 data_table_id", func(p map[string]any) {
			p["targets"] = []any{map[string]any{"field_id": "f_a", "order": "asc"}}
		}},
		{"跨表排序键", func(p map[string]any) {
			p["targets"] = []any{
				map[string]any{"data_table_id": "dt_a", "field_id": "f_a"},
				map[string]any{"data_table_id": "dt_b", "field_id": "f_b"},
			}
		}},
		{"order 非法", func(p map[string]any) {
			p["targets"] = []any{map[string]any{"data_table_id": "dt_detail", "field_id": "f_a", "order": "up"}}
		}},
		{"排序字段重复", func(p map[string]any) {
			p["targets"] = []any{
				map[string]any{"data_table_id": "dt_detail", "field_id": "f_a"},
				map[string]any{"data_table_id": "dt_detail", "field_id": "f_a", "order": "desc"},
			}
		}},
	}
	for _, tc := range cases {
		p := validParams()
		tc.mutate(p)
		if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); r.GetValid() {
			t.Fatalf("%s: 期望校验失败", tc.name)
		}
	}
}

// order 缺省应归一化为 asc。
func TestParseSortTargetsDefaultOrder(t *testing.T) {
	targets, msg := parseSortTargets(map[string]any{
		"targets": []any{map[string]any{"data_table_id": "dt", "field_id": "f_a"}},
	})
	if msg != "" {
		t.Fatalf("期望通过: %s", msg)
	}
	if targets[0].Order != "asc" {
		t.Fatalf("缺省 order 应为 asc，得到 %q", targets[0].Order)
	}
}

func snap(rowKey string, cells map[string]any) rowSnap {
	cv := make(map[string]cellVal, len(cells))
	for k, v := range cells {
		cv[k] = cellVal{Raw: v}
	}
	return rowSnap{RowKey: rowKey, Cells: cv}
}

// 多列组合：首键最优先；同首键内按次键排。
func TestSortRowKeysMultiKeyPriority(t *testing.T) {
	snaps := []rowSnap{
		snap("r1", map[string]any{"cat": "B", "qty": float64(1)}),
		snap("r2", map[string]any{"cat": "A", "qty": float64(9)}),
		snap("r3", map[string]any{"cat": "A", "qty": float64(2)}),
		snap("r4", map[string]any{"cat": "B", "qty": float64(5)}),
	}
	got := sortRowKeys(snaps, []sortTarget{
		{DataTableID: "dt", FieldID: "cat", Order: "asc"},
		{DataTableID: "dt", FieldID: "qty", Order: "desc"},
	})
	want := []string{"r2", "r3", "r4", "r1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("多键排序序错: got=%v want=%v", got, want)
		}
	}
}

// null 恒排末尾（与 asc/desc 无关）；数值优先比较；字符串回退。
func TestLessCellRawSemantics(t *testing.T) {
	if !lessCellRaw(float64(1), nil, true) {
		t.Fatal("desc 下 null 也应排末尾")
	}
	if lessCellRaw(nil, "x", false) {
		t.Fatal("null 不应排在有值之前")
	}
	if !lessCellRaw("2", "10", false) {
		t.Fatal("可解析数字的字符串应按数值比较（2 < 10）")
	}
	if !lessCellRaw("apple", "banana", false) {
		t.Fatal("非数字字符串应按字典序")
	}
	if !lessCellRaw("banana", "apple", true) {
		t.Fatal("desc 应反转字符串序")
	}
	if lessCellRaw(float64(3), float64(3), true) || lessCellRaw(float64(3), float64(3), false) {
		t.Fatal("相等须返回 false（保持稳定性）")
	}
}

// 稳定性：排序键相等的行保持原相对序。
func TestSortRowKeysStable(t *testing.T) {
	snaps := []rowSnap{
		snap("r1", map[string]any{"cat": "A"}),
		snap("r2", map[string]any{"cat": "A"}),
		snap("r3", map[string]any{"cat": "A"}),
	}
	got := sortRowKeys(snaps, []sortTarget{{DataTableID: "dt", FieldID: "cat", Order: "desc"}})
	want := []string{"r1", "r2", "r3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("同值行应保持原序: got=%v", got)
		}
	}
}

// Execute 无宿主句柄时应失败而非 panic（sidecar 裸调保护）。
func TestExecuteNoHost(t *testing.T) {
	resp, err := New().Execute(context.Background(), &bizpb.ExecuteRequest{
		ActionName: ActionName,
		Context:    mustStruct(t, map[string]any{"params": validParams()}),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != "SORT_NO_HOST" {
		t.Fatalf("期望 SORT_NO_HOST 失败态: %+v", resp)
	}
}

type executeMirror struct {
	plugin.MirrorClient
	snapshot       []byte
	readSessionID  string
	readTableID    string
	reorderSession string
	reorderTable   string
	reorderGrant   string
	reordered      []string
	reorderErr     error
}

func (m *executeMirror) ReadTable(_ context.Context, sessionID, dataTableID string) ([]byte, error) {
	m.readSessionID = sessionID
	m.readTableID = dataTableID
	return m.snapshot, nil
}

func (m *executeMirror) ReorderDetailRows(
	_ context.Context,
	sessionID,
	dataTableID string,
	ordered []string,
	writeGrant string,
) error {
	m.reorderSession = sessionID
	m.reorderTable = dataTableID
	m.reorderGrant = writeGrant
	m.reordered = append([]string(nil), ordered...)
	return m.reorderErr
}

type executeHost struct {
	plugin.Host
	mirror plugin.MirrorClient
}

func (h *executeHost) Mirror() plugin.MirrorClient { return h.mirror }
func (h *executeHost) Log() plugin.LogClient       { return nil }

// TestExecuteSortsAmountDescendingThroughMirror 是运行态证据：不是只校验/保存 params，
// 而是实际读取表单数据层行快照，按金额降序计算稳定 rowKey 序列，再携授权票据调用物理重排。
func TestExecuteSortsAmountDescendingThroughMirror(t *testing.T) {
	snapshot, err := json.Marshal([]rowSnap{
		snap("row:1", map[string]any{"amount": float64(100)}),
		snap("row:2", map[string]any{"amount": float64(300)}),
		snap("row:3", map[string]any{"amount": float64(200)}),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	mirror := &executeMirror{snapshot: snapshot}
	ctx := plugin.WithHost(context.Background(), &executeHost{mirror: mirror})
	response, err := New().Execute(ctx, &bizpb.ExecuteRequest{
		ActionName: ActionName,
		Context: mustStruct(t, map[string]any{
			"params": map[string]any{
				"targets": []any{
					map[string]any{"data_table_id": "detail_lines", "field_id": "amount", "order": "desc"},
				},
			},
			"query_context": map[string]any{
				"sheet_session_id": "sheet-session-5",
				"main_table_id":    "main",
				"form_mode":        "edit",
				"write_grant":      "grant-5",
			},
		}),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !response.GetSuccess() {
		t.Fatalf("Execute failed: code=%s message=%s", response.GetErrorCode(), response.GetMessage())
	}
	if mirror.readSessionID != "sheet-session-5" || mirror.readTableID != "detail_lines" {
		t.Fatalf("ReadTable coords = (%q,%q)", mirror.readSessionID, mirror.readTableID)
	}
	if mirror.reorderSession != "sheet-session-5" || mirror.reorderTable != "detail_lines" || mirror.reorderGrant != "grant-5" {
		t.Fatalf(
			"ReorderDetailRows coords/grant = (%q,%q,%q)",
			mirror.reorderSession,
			mirror.reorderTable,
			mirror.reorderGrant,
		)
	}
	if want := []string{"row:2", "row:3", "row:1"}; !reflect.DeepEqual(mirror.reordered, want) {
		t.Fatalf("reordered = %v, want %v", mirror.reordered, want)
	}
}

// TestExecuteSortDetailViewModeStillMutatesMirror 是「查看态动作不受只读约束」的运行态证据：
// view 态与 edit 态走同一条 ReadTable → 排序 → ReorderDetailRows 路径，照常改表单数据层行序
// （不落库、只活在本次会话）。若 Execute 加回 view 跳过分支，本用例会因 ReadTable 未被调用而失败。
func TestExecuteSortDetailViewModeStillMutatesMirror(t *testing.T) {
	snapshot, err := json.Marshal([]rowSnap{
		snap("row:1", map[string]any{"amount": float64(1)}),
		snap("row:2", map[string]any{"amount": float64(2)}),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	mirror := &executeMirror{snapshot: snapshot}
	ctx := plugin.WithHost(context.Background(), &executeHost{mirror: mirror})
	response, err := New().Execute(ctx, &bizpb.ExecuteRequest{
		ActionName: ActionName,
		Context: mustStruct(t, map[string]any{
			"params": map[string]any{
				"targets": []any{
					map[string]any{"data_table_id": "detail_lines", "field_id": "amount", "order": "desc"},
				},
			},
			"query_context": map[string]any{
				"sheet_session_id": "sheet-session-view",
				"main_table_id":    "main",
				"form_mode":        "view",
				"write_grant":      "grant-view",
			},
		}),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !response.GetSuccess() {
		t.Fatalf("Execute failed: code=%s message=%s", response.GetErrorCode(), response.GetMessage())
	}
	if mirror.readSessionID != "sheet-session-view" || mirror.readTableID != "detail_lines" {
		t.Fatalf("view mode should still ReadTable, got (%q,%q)", mirror.readSessionID, mirror.readTableID)
	}
	if mirror.reorderSession != "sheet-session-view" || mirror.reorderTable != "detail_lines" || mirror.reorderGrant != "grant-view" {
		t.Fatalf(
			"view mode should still ReorderDetailRows coords/grant = (%q,%q,%q)",
			mirror.reorderSession,
			mirror.reorderTable,
			mirror.reorderGrant,
		)
	}
	if want := []string{"row:2", "row:1"}; !reflect.DeepEqual(mirror.reordered, want) {
		t.Fatalf("reordered = %v, want %v", mirror.reordered, want)
	}
}

func TestExecuteSortDetailSurfacesMirrorReorderFailure(t *testing.T) {
	snapshot, err := json.Marshal([]rowSnap{
		snap("row:1", map[string]any{"amount": float64(1)}),
		snap("row:2", map[string]any{"amount": float64(2)}),
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	mirror := &executeMirror{snapshot: snapshot, reorderErr: errors.New("mirror conflict")}
	ctx := plugin.WithHost(context.Background(), &executeHost{mirror: mirror})
	response, err := New().Execute(ctx, &bizpb.ExecuteRequest{
		ActionName: ActionName,
		Context: mustStruct(t, map[string]any{
			"params": map[string]any{
				"targets": []any{
					map[string]any{"data_table_id": "detail_lines", "field_id": "amount", "order": "desc"},
				},
			},
			"query_context": map[string]any{
				"sheet_session_id": "sheet-session-error",
				"main_table_id":    "main",
				"form_mode":        "edit",
				"write_grant":      "grant-error",
			},
		}),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.GetSuccess() || response.GetErrorCode() != "SORT_REORDER_FAILED" {
		t.Fatalf("expected SORT_REORDER_FAILED, got %+v", response)
	}
}
