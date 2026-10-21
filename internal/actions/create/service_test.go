package create

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/hostpb"

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

// validParams 是无字面值的最小合法 write_v4 参数：插件侧只做浅校验
// （schema_version/operation 对得上），故这里的 query/target/assignments 内容足够简单——深层
// 结构（赋值非空、父锚定必填等）由宿主保存闸 + 设计态客户端闸负责，插件测试不重复覆盖。
func validParams() map[string]any {
	return map[string]any{
		"write_v4": map[string]any{
			"schema_version": "kimpo.write-node/v4",
			"syntax_profile": "kimposql.mssql/v1",
			"operation":      "insert",
			"query":          map[string]any{"sources": []any{}, "joins": []any{}, "options": map[string]any{"distinct": false}},
			"target":         map[string]any{"data_table_id": "dt_t", "template_id": "tpl_t"},
			"assignments":    []any{map[string]any{"target_field_id": "f_a", "expression": map[string]any{}}},
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
	caps := resp.GetActions()[0].GetCapabilities()
	// 三写动作声明 Asyncable=true——插件只声明"可以异步"这一能力，具体某节点是否异步由
	// 设计器决定、默认同步（node.Async 零值 false）。
	if !caps.GetSideEffecting() || !caps.GetExpressionBased() || !caps.GetAsyncable() {
		t.Fatalf("capabilities 异常: %+v", caps)
	}
}

func TestValidateParams(t *testing.T) {
	svc := New()

	// 合法配置通过。
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, validParams())}); !r.GetValid() {
		t.Fatalf("期望通过: %v", r.GetErrors())
	}

	// operation 不符（三写插件防串挂）。
	p := validParams()
	p["write_v4"].(map[string]any)["operation"] = "update"
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); r.GetValid() {
		t.Fatal("operation 不符应校验失败")
	}

	// schema_version 不符（旧格式/畸形节点，同一浅校验分支）。
	p = validParams()
	p["write_v4"].(map[string]any)["schema_version"] = "kimpo.write-node/v3"
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); r.GetValid() {
		t.Fatal("schema_version 不符应校验失败")
	}

	// write_v4 缺失（结构性拒绝生效）。
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, map[string]any{})}); r.GetValid() {
		t.Fatal("缺 write_v4 应校验失败")
	}

	// 旧参数键 expression（已退役）：additionalProperties:false 结构性拒绝 + 缺 write_v4。
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, map[string]any{
		"expression": map[string]any{"schema_version": float64(1)},
	})}); r.GetValid() {
		t.Fatal("旧 expression 键应被拒绝")
	}

	// 未知顶层键（schema 浅校验）。
	p = validParams()
	p["ghost"] = 1
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); r.GetValid() {
		t.Fatal("未知键应校验失败")
	}

	// launch_workflow：schema 不声明该键，additionalProperties:false 下必须结构性拒绝。
	p = validParams()
	p["launch_workflow"] = true
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); r.GetValid() {
		t.Fatal("launch_workflow 不在 schema 内，应校验失败")
	}
}

// TestValidateParams_LegacyExpressionRetired：只带存量旧格式
// 参数键 expression（无 write_v4）的节点，必须先于 additionalProperties:false 的 schema 浅校验，
// 具名拒绝到新 key write.common.validate.legacy_retired（FieldPath="expression"），而不是被结构性
// 拒绝成一句无业务含义的「不接受该参数 expression」；同时确认新增的前置分支不影响 write_v4
// 正常参数的既有通过路径。断言直接读 writecommon/locales/en-US.json 原文，不复用 writecommon.T
// 自证。
func TestValidateParams_LegacyExpressionRetired(t *testing.T) {
	const key = "write.common.validate.legacy_retired"
	raw, err := os.ReadFile("../writecommon/locales/en-US.json")
	if err != nil {
		t.Fatalf("read en-US.json: %v", err)
	}
	var enUS map[string]string
	if err := json.Unmarshal(raw, &enUS); err != nil {
		t.Fatalf("parse en-US.json: %v", err)
	}
	wantEnglish, ok := enUS[key]
	if !ok || wantEnglish == "" {
		t.Fatalf("en-US.json 缺少 key %q", key)
	}

	svc := New()

	// 只有 expression 键（旧格式，无 write_v4）：应命中具名退役分支，且只报这一条错误
	// （不应叠加 schema 浅校验对 expression 键本身的"不接受该参数"）。
	r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{
		Params: mustStruct(t, map[string]any{
			"expression": map[string]any{"schema_version": float64(1)},
		}),
		Locale: "en-US",
	})
	if r.GetValid() {
		t.Fatal("只有旧 expression 键应校验失败")
	}
	if len(r.GetErrors()) != 1 {
		t.Fatalf("应只报一条退役具名错误: %+v", r.GetErrors())
	}
	if got := r.GetErrors()[0].GetFieldPath(); got != "expression" {
		t.Fatalf("FieldPath 应为 expression, got %q", got)
	}
	if got := r.GetErrors()[0].GetMessage(); got != wantEnglish {
		t.Fatalf("locale=en-US 时消息应等于语言包原文: got=%q want=%q", got, wantEnglish)
	}

	// write_v4 正常参数不受影响：新增的前置分支只在"有 expression 且无 write_v4"时命中，
	// 既有合法 write_v4-only 配置的通过路径必须保持不变。
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, validParams())}); !r.GetValid() {
		t.Fatalf("write_v4 正常参数应通过: %v", r.GetErrors())
	}
}

// TestValidateParams_MaxAffectedRowsRange：行数阈值必须是 >=0 的整数（不设上限，0 = 不限）；未配置该参数视为合法（可选字段）。
func TestValidateParams_MaxAffectedRowsRange(t *testing.T) {
	svc := New()

	// 未配置：合法。
	if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, validParams())}); !r.GetValid() {
		t.Fatalf("未配置 max_affected_rows 应通过: %v", r.GetErrors())
	}

	// 越界取值（含非整数）：均应校验失败。
	for name, val := range map[string]float64{
		"负数":  -1,
		"非整数": 1.5,
	} {
		t.Run(name, func(t *testing.T) {
			p := validParams()
			p["max_affected_rows"] = val
			r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)})
			if r.GetValid() {
				t.Fatalf("max_affected_rows=%v 应校验失败", val)
			}
			found := false
			for _, e := range r.GetErrors() {
				if e.GetFieldPath() == "max_affected_rows" {
					found = true
				}
			}
			if !found {
				t.Fatalf("期望 FieldPath=max_affected_rows 的错误项: %+v", r.GetErrors())
			}
		})
	}

	// 边界与大值均合法：0（不限）、1、100000、1e9（不设上限）。
	for _, val := range []float64{0, 1, 100000, 1e9} {
		p := validParams()
		p["max_affected_rows"] = val
		if r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{Params: mustStruct(t, p)}); !r.GetValid() {
			t.Fatalf("max_affected_rows=%v 应通过: %v", val, r.GetErrors())
		}
	}
}

// TestValidateParams_MaxAffectedRowsMessageFollowsRequestLocale 与 update 包同名用例对称：
// ValidateParams 的校验消息必须按请求 locale 返回，而不是固定按插件默认语言。断言直接读 writecommon/locales/en-US.json 原文，不复用
// writecommon.T 自证。
func TestValidateParams_MaxAffectedRowsMessageFollowsRequestLocale(t *testing.T) {
	const key = "write.create.max_affected_rows_out_of_range"
	raw, err := os.ReadFile("../writecommon/locales/en-US.json")
	if err != nil {
		t.Fatalf("read en-US.json: %v", err)
	}
	var enUS map[string]string
	if err := json.Unmarshal(raw, &enUS); err != nil {
		t.Fatalf("parse en-US.json: %v", err)
	}
	wantEnglish, ok := enUS[key]
	if !ok || wantEnglish == "" {
		t.Fatalf("en-US.json 缺少 key %q", key)
	}

	svc := New()
	p := validParams()
	p["max_affected_rows"] = float64(-1) // 越界（负数），触发本条校验错误；0 = 不限
	r, _ := svc.ValidateParams(context.Background(), &bizpb.ValidateParamsRequest{
		Params: mustStruct(t, p),
		Locale: "en-US",
	})
	if r.GetValid() {
		t.Fatal("max_affected_rows=-1 应校验失败")
	}
	var got string
	found := false
	for _, e := range r.GetErrors() {
		if e.GetFieldPath() == "max_affected_rows" {
			got = e.GetMessage()
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("未找到 max_affected_rows 字段错误: %+v", r.GetErrors())
	}
	if got != wantEnglish {
		t.Fatalf("locale=en-US 时消息应等于语言包原文: got=%q want=%q", got, wantEnglish)
	}
}

// TestMaxAffectedRowsOutOfRange 直接单测纯函数本身（不经 RPC 外壳），覆盖边界与类型异常，
// 确认不会因非 float64 类型（真实生产路径不会出现，但函数须防御性处理）而 panic。
func TestMaxAffectedRowsOutOfRange(t *testing.T) {
	cases := []struct {
		name    string
		omitKey bool // true=键整体缺失；false=写入键（val 可能是 nil，代表显式 JSON null）
		val     any
		want    bool
	}{
		{name: "键缺失", omitKey: true, want: false},
		{name: "显式 null", val: nil, want: false},
		{name: "下边界 1", val: float64(1), want: false},
		{name: "100000 合法", val: float64(100000), want: false},
		{name: "大值 1e9 合法（不设上限）", val: float64(1e9), want: false},
		{name: "零 = 不限", val: float64(0), want: false},
		{name: "负数", val: float64(-1), want: true},
		{name: "非整数", val: float64(1.5), want: true},
		{name: "字符串类型", val: "100", want: true},
		{name: "布尔类型", val: true, want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params := map[string]any{}
			if !c.omitKey {
				params["max_affected_rows"] = c.val
			}
			if got := maxAffectedRowsOutOfRange(params); got != c.want {
				t.Fatalf("maxAffectedRowsOutOfRange(%v) = %v, want %v", params, got, c.want)
			}
		})
	}
}

// —— Execute 测试脚手架：只提供宿主数据语义服务 Apply，不提供 Query/TemplateCatalog/RecordWrite ——

type fakeAliceClient struct {
	plugin.AliceClient
	applyReply *hostpb.ApplyChangeSetReply
	applyErr   error
	applyCalls int
	gotApply   *hostpb.ApplyChangeSetRequest
}

func (f *fakeAliceClient) ApplyChangeSet(_ context.Context, req *hostpb.ApplyChangeSetRequest) (*hostpb.ApplyChangeSetReply, error) {
	f.applyCalls++
	f.gotApply = req
	return f.applyReply, f.applyErr
}

type fakeHost struct {
	plugin.Host
	alice plugin.AliceClient
}

func (h *fakeHost) Alice() plugin.AliceClient { return h.alice }

// Log 覆写为返回 nil：logf 对 h.Log() 判空前会先调用它，嵌入的零值 plugin.Host 接口本身是
// nil，直接调用其方法会 panic（而非返回零值），故须显式覆写（与 Alice() 覆写同理）。
func (h *fakeHost) Log() plugin.LogClient { return nil }

func appliedReply(status string) *hostpb.ApplyChangeSetReply {
	return &hostpb.ApplyChangeSetReply{
		OverallStatus: status,
		Meta:          &hostpb.ExecutionMeta{Rows: 1, TraceId: "trace-create"},
	}
}

func executeWith(t *testing.T, queryContext map[string]any, alice *fakeAliceClient) *bizpb.ExecuteResponse {
	t.Helper()
	return executeWithParams(t, validParams(), queryContext, alice)
}

func executeWithParams(t *testing.T, params map[string]any, queryContext map[string]any, alice *fakeAliceClient) *bizpb.ExecuteResponse {
	t.Helper()
	reqCtx := mustStruct(t, map[string]any{"params": params, "query_context": queryContext})
	ctx := plugin.WithHost(context.Background(), &fakeHost{alice: alice})
	resp, err := New().Execute(ctx, &bizpb.ExecuteRequest{Context: reqCtx})
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	return resp
}

func TestExecute_MutationTicketAppliesWithoutClientAuthorityFields(t *testing.T) {
	alice := &fakeAliceClient{applyReply: appliedReply("SUCCEEDED")}
	resp := executeWith(t, map[string]any{
		plugin.QueryContextKeyAliceMutationTicket:     "ticket-create-1",
		"alice_command_create_rowset_sealed_ref":      "forged-sealed",
		"alice_command_create_rowset_content_hash":    "sha256:forged",
		"alice_command_create_rowset_idempotency_key": "forged-idempotency",
		"alice_plan_ref":   map[string]any{"consumer_id": "legacy"},
		"alice_plan_grant": "legacy-grant",
	}, alice)
	if !resp.GetSuccess() || alice.applyCalls != 1 {
		t.Fatalf("写入令牌 ticket 应只 Apply 一次: resp=%+v calls=%d", resp, alice.applyCalls)
	}
	if alice.gotApply.GetCommandGrant() != "ticket-create-1" || alice.gotApply.GetSealed() != nil ||
		alice.gotApply.GetIdempotencyKey() != "" || alice.gotApply.GetContext() == nil {
		t.Fatalf("插件不得提交 sealed/hash/idempotency/业务标识，只能呈递 ticket: %+v", alice.gotApply)
	}
}

func TestExecute_MissingMutationTicketFailsBeforeAliceRPC(t *testing.T) {
	for name, queryContext := range map[string]map[string]any{
		"empty context": {},
		"blank token":   {plugin.QueryContextKeyAliceMutationTicket: "  "},
		"wrong shape":   {plugin.QueryContextKeyAliceMutationTicket: float64(1)},
		"legacy pair":   {"alice_command_create_rowset_sealed_ref": "cs", "alice_command_create_rowset_content_hash": "sha256:x"},
		"legacy plan":   {"alice_plan_ref": map[string]any{"consumer_id": "c"}, "alice_plan_grant": "grant"},
	} {
		t.Run(name, func(t *testing.T) {
			alice := &fakeAliceClient{}
			resp := executeWith(t, queryContext, alice)
			// ErrorCode 本身不受 i18n 影响，但 MessageKey 也是对外契约面，必须一并钉住
			// （防止调用点漏传/传错 key，静默退化成裸错误码无文案）。
			if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_MUTATION_TICKET_REQUIRED" || resp.GetMessageKey() != "write.common.mutation_ticket_required" {
				t.Fatalf("resp=%+v", resp)
			}
			if alice.applyCalls != 0 {
				t.Fatalf("缺 ticket 不得发起宿主数据语义服务 RPC: apply=%d", alice.applyCalls)
			}
		})
	}
}

func TestExecute_TicketIsRequiredBeforeHostAvailability(t *testing.T) {
	reqCtx := mustStruct(t, map[string]any{"params": validParams(), "query_context": map[string]any{}})
	resp, err := New().Execute(context.Background(), &bizpb.ExecuteRequest{Context: reqCtx})
	if err != nil || resp.GetErrorCode() != "WRITE_MUTATION_TICKET_REQUIRED" || resp.GetMessageKey() != "write.common.mutation_ticket_required" {
		t.Fatalf("缺 ticket 必须先于 host 检查失败: resp=%+v err=%v", resp, err)
	}
}

// TestExecute_NoHostFailsWithNoHostKey 覆盖 WRITE_NO_HOST 分支端到端：持有效 ticket 但裸 context（未经 plugin.WithHost
// 注入宿主句柄）必须精确落在 WRITE_NO_HOST + write.common.no_host，而不是被前一道 ticket 检查
// 提前拦下（两者判据独立，靠 query_context 与 ctx 分别控制）。
func TestExecute_NoHostFailsWithNoHostKey(t *testing.T) {
	reqCtx := mustStruct(t, map[string]any{
		"params":        validParams(),
		"query_context": map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-no-host"},
	})
	resp, err := New().Execute(context.Background(), &bizpb.ExecuteRequest{Context: reqCtx})
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_NO_HOST" || resp.GetMessageKey() != "write.common.no_host" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestExecute_ApplyFailureDoesNotFallBack(t *testing.T) {
	for _, test := range []struct {
		name  string
		alice *fakeAliceClient
		code  string
		key   string
	}{
		{name: "apply RPC 失败", alice: &fakeAliceClient{applyErr: errors.New("credential ticket-create-fail")}, code: "WRITE_APPLY_FAILED", key: "write.common.apply_failed"},
		{name: "apply 状态非 SUCCEEDED", alice: &fakeAliceClient{applyReply: appliedReply("FAILED")}, code: "WRITE_FAILED", key: "write.common.failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := executeWith(t, map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-fail"}, test.alice)
			if resp.GetSuccess() || resp.GetErrorCode() != test.code || resp.GetMessageKey() != test.key {
				t.Fatalf("resp=%+v want code=%s key=%s", resp, test.code, test.key)
			}
			if test.alice.applyCalls != 1 {
				t.Fatalf("expected exactly one Apply attempt, got %d", test.alice.applyCalls)
			}
			if strings.Contains(resp.GetMessage(), "ticket-create-fail") {
				t.Fatalf("失败响应不得泄漏写入令牌: %q", resp.GetMessage())
			}
		})
	}
}

// TestExecute_ApplyFailure_MirrorFenceRejected：表单会话持有目标记录时，宿主表单数据层写围栏
// 拒绝整批写入（宿主的三种拒绝文案均含 "Mirror session" 或 "write fence"），
// 插件须改判专属错误码 WRITE_FENCE_REJECTED + 固定文案，且不得把宿主状态码/文案透出。
func TestExecute_ApplyFailure_MirrorFenceRejected(t *testing.T) {
	for _, hostMsg := range []string{
		"MIRROR_FENCE_BUSY: target record is covered by an open Mirror session the caller does not own",
		"MIRROR_FENCE_UNAVAILABLE: Mirror write fence session ownership authority is unavailable",
		"ALICE_APPLY_FENCE_UNAVAILABLE: Mirror write fence rejected the request: some underlying reason",
	} {
		t.Run(hostMsg, func(t *testing.T) {
			alice := &fakeAliceClient{applyErr: status.Error(codes.FailedPrecondition, hostMsg)}
			resp := executeWith(t, map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-fence"}, alice)
			if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_FENCE_REJECTED" || resp.GetMessageKey() != "write.common.fence_rejected" {
				t.Fatalf("resp=%+v", resp)
			}
			if strings.Contains(resp.GetMessage(), "MIRROR_FENCE") || strings.Contains(resp.GetMessage(), "ALICE_APPLY_FENCE") {
				t.Fatalf("围栏拒绝不得透传宿主状态码/文案: %q", resp.GetMessage())
			}
		})
	}
}

// TestExecute_ApplyFailure_OtherReasonSurfaced：非围栏的 Apply 失败仍是
// WRITE_APPLY_FAILED，但把宿主 gRPC 状态文案作为 {reason} 追加进展示文案，帮助 UI/审计定位。
func TestExecute_ApplyFailure_OtherReasonSurfaced(t *testing.T) {
	hostMsg := "ALICE_APPLY_CHANGESET_EXPIRED: change-set has expired; re-derive"
	alice := &fakeAliceClient{applyErr: status.Error(codes.FailedPrecondition, hostMsg)}
	resp := executeWith(t, map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-other"}, alice)
	if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_APPLY_FAILED" || resp.GetMessageKey() != "write.common.apply_failed" {
		t.Fatalf("resp=%+v", resp)
	}
	if !strings.Contains(resp.GetMessage(), hostMsg) {
		t.Fatalf("非围栏 Apply 失败应透出宿主状态文案作为 reason: %q", resp.GetMessage())
	}
}

// TestExecute_MaxAffectedRowsOutOfRangeFailsBeforeAliceRPC：越界的行数阈值必须在触达 ticket
// 检查/宿主数据语义服务 RPC 之前拦下，专属错误码 WRITE_MAX_AFFECTED_ROWS_OUT_OF_RANGE，
// 不与其它写模式结构校验共用 WRITE_BAD_PARAMS。
func TestExecute_MaxAffectedRowsOutOfRangeFailsBeforeAliceRPC(t *testing.T) {
	p := validParams()
	p["max_affected_rows"] = float64(-1) // 不设上限且 0 = 不限，用负数作为越界样本
	reqCtx := mustStruct(t, map[string]any{
		"params":        p,
		"query_context": map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-max-rows"},
	})
	resp, err := New().Execute(context.Background(), &bizpb.ExecuteRequest{Context: reqCtx})
	if err != nil {
		t.Fatalf("Execute transport error: %v", err)
	}
	if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_MAX_AFFECTED_ROWS_OUT_OF_RANGE" || resp.GetMessageKey() != "write.create.max_affected_rows_out_of_range" {
		t.Fatalf("resp=%+v", resp)
	}
}

// TestExecute_BadWriteV4FailsBeforeAliceRPC：write_v4 浅校验（存在/是对象/schema_version/
// operation 对得上）必须先于 ticket 检查与任何宿主数据语义服务 RPC 拦下，在 Execute 端到端覆盖。
func TestExecute_BadWriteV4FailsBeforeAliceRPC(t *testing.T) {
	qc := map[string]any{plugin.QueryContextKeyAliceMutationTicket: "ticket-create-bad-write-v4"}
	for name, mutate := range map[string]func(p map[string]any){
		"write_v4 缺失": func(p map[string]any) { delete(p, "write_v4") },
		"schema_version 不符": func(p map[string]any) {
			p["write_v4"].(map[string]any)["schema_version"] = "kimpo.write-node/v3"
		},
		"operation 不符": func(p map[string]any) {
			p["write_v4"].(map[string]any)["operation"] = "delete"
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := validParams()
			mutate(p)
			alice := &fakeAliceClient{}
			resp := executeWithParams(t, p, qc, alice)
			if resp.GetSuccess() || resp.GetErrorCode() != "WRITE_BAD_PARAMS" {
				t.Fatalf("resp=%+v", resp)
			}
			if alice.applyCalls != 0 {
				t.Fatalf("write_v4 形状不合法不得发起宿主数据语义服务 RPC: apply=%d", alice.applyCalls)
			}
		})
	}
	// operation 不符时 MessageKey 是稳定契约面（携带 got/want 插值），单独钉住。
	p := validParams()
	p["write_v4"].(map[string]any)["operation"] = "delete"
	resp := executeWithParams(t, p, qc, &fakeAliceClient{})
	if resp.GetMessageKey() != "write.common.bad_params.operation_mismatch" {
		t.Fatalf("resp=%+v", resp)
	}
}
