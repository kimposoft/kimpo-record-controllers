package detailsort

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

// —— data.sort_detail（明细排序）——
//
// 填报态动作：对当前表单会话中某张明细表的行做多列组合排序（物理行序重排）。
// 执行形态：
//  1. Mirror.ReadTable 读取目标明细表全行快照（宿主表单数据层是填报态业务权威，
//     行数据一律从它读，不经 editor）；
//  2. 内存多列稳定排序（targets 数组序 = 排序优先级，首个最优先；null 恒排末尾；
//     两值都可解析为数字时数值比较，否则字符串比较；desc 只反转有值之间的序）；
//  3. Mirror.ReorderDetailRows 把排好的 rowKey 序列落成物理行序。
//
// 授权设计（关键，勿改坏）：参数刻意采用 params.targets[]{data_table_id, field_id} 形态
// （宿主动作网关据此推导可写字段），使 write grant 白名单自动带上目标明细表 TableID ——
// ReorderDetailRows 的表级行操作授权（AllowsRowOps）由此满足。
//
// 数据交互铁律：读写全经 Host.Mirror() SDK；插件不碰裸 SQL、不连库。

const (
	ActionName = "data.sort_detail"
)

// Service 明细排序动作服务（无状态）。
type Service struct {
	bizpb.UnimplementedBusinessPluginServer
}

func New() *Service { return &Service{} }

// sortTarget 单个排序键：目标明细表 + 排序字段 + 升降序（数组序=优先级）。
// data_table_id 每行冗余携带（网关白名单形态要求），业务上要求全部相同（一次动作只排一张明细表）。
type sortTarget struct {
	DataTableID string `json:"data_table_id"`
	FieldID     string `json:"field_id"`
	Order       string `json:"order"` // "asc"（缺省）| "desc"
}

// paramsSchemaMap 参数 JSON-Schema 单一来源：GetActions 声明 + ValidateParams 浅校验共用。
// x-editor: detail-sort → 前端 NodeConfigDialog 据此渲染「明细排序」引导式配置面板
// （选明细表 + 排序键行编辑 + 上下移调优先级），不走通用输入框渲染。
func paramsSchemaMap() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"x-editor":             "detail-sort",
		"required":             []any{"targets"},
		"properties": map[string]any{
			"targets": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"data_table_id": map[string]any{"type": "string"},
						"field_id":      map[string]any{"type": "string"},
						"order":         map[string]any{"type": "string", "enum": []any{"asc", "desc"}},
					},
				},
			},
		},
	}
}

// GetActions 声明明细排序动作。SideEffecting=true（改写表单数据层业务数据物理行序，test 态被宿主拦截）；
// ExpressionBased=false（参数为轻量排序键列表，不挂标准化表达式编辑器）。
func (s *Service) GetActions(_ context.Context, _ *bizpb.GetActionsRequest) (*bizpb.GetActionsResponse, error) {
	schema, err := structpb.NewStruct(paramsSchemaMap())
	if err != nil {
		return nil, err
	}
	return &bizpb.GetActionsResponse{
		Actions: []*bizpb.ActionDefinition{
			{
				Name:         ActionName,
				Title:        "明细排序",
				Description:  "对当前表单某张明细表的行按多列组合排序（可配多个排序字段与各自升/降序，先后顺序即优先级），经宿主表单数据层重排物理行序。",
				ParamsSchema: schema,
				Style:        &bizpb.ActionStyle{Shape: "rounded", Color: "info"},
				// scheduled_context 不声明（缺省 false）：本动作依赖当前表单会话中明细表的物理行序，
				// 定时任务 headless 上下文无表单会话可言，故不可定时执行。
				Capabilities: &bizpb.ActionCapabilities{Asyncable: false, SideEffecting: true, ExpressionBased: false},
				// 动作面板排序权重 50：排在删除数据之后、发起流程之前。
				SortWeight: 50,
				// 动作面板功能分类（设计器右栏页签聚合依据；标准动作另有宿主 canonical 兜底防旧二进制漂移）。
				Category: "数据",
			},
		},
	}, nil
}

// Execute 执行一次明细排序：解析排序键 → ReadTable → 多列稳定排序 → ReorderDetailRows。
func (s *Service) Execute(ctx context.Context, req *bizpb.ExecuteRequest) (*bizpb.ExecuteResponse, error) {
	ctxMap := contextMap(req.GetContext())
	params, _ := ctxMap["params"].(map[string]any)
	targets, msg := parseSortTargets(params)
	if msg != "" {
		return failure("SORT_BAD_PARAMS", msg), nil
	}
	tableID := targets[0].DataTableID

	host := plugin.HostFrom(ctx)
	if host == nil || host.Mirror() == nil {
		return failure("SORT_NO_HOST", "明细排序需经宿主反向通道执行，但未注入 host.Mirror 句柄"), nil
	}
	mirror := host.Mirror()

	qc, _ := ctxMap["query_context"].(map[string]any)
	sessionID, _ := qc["sheet_session_id"].(string)
	if strings.TrimSpace(sessionID) == "" {
		return failure("SORT_NO_SESSION", "明细排序仅在填报态（表单会话）可用，query_context 缺少 sheet_session_id"), nil
	}
	// 排序目标必须是明细表：主表单行无排序语义，且误配主表会把 Reorder 打到非明细结构上。
	if mainTableID, _ := qc["main_table_id"].(string); strings.TrimSpace(mainTableID) == tableID {
		return failure("SORT_BAD_TARGET", "明细排序的目标必须是明细表，不能是主表"), nil
	}
	// 查看态不跳过：查看态不可编辑是对用户的约束，不约束动作。查看态会话的表单数据层同样可写
	// （不分 form_mode），查看态与编辑/新增态走同一条 ReadTable → 排序 → ReorderDetailRows 路径；
	// 查看态下结果只活在本次会话（表单数据层内存 + 界面重排），不落库。
	formMode, _ := qc["form_mode"].(string)
	writeGrant, _ := qc["write_grant"].(string)

	// ① 读取全行快照（含 raw 单元格值，供排序比较）。
	rowsJSON, err := mirror.ReadTable(ctx, sessionID, tableID)
	if err != nil {
		logf(ctx, "ERROR", "明细排序 ReadTable 失败: %v", err)
		return failure("SORT_READ_FAILED", fmt.Sprintf("读取明细表失败: %v", err)), nil
	}
	snaps, err := parseRowSnapshots(rowsJSON)
	if err != nil {
		return failure("SORT_BAD_ROWS", fmt.Sprintf("解析明细行快照失败: %v", err)), nil
	}
	if len(snaps) <= 1 {
		logf(ctx, "INFO", "明细排序: 表 %s 行数 %d，无需重排", tableID, len(snaps))
		return &bizpb.ExecuteResponse{Success: true}, nil
	}

	// ② 多列稳定排序 → ③ 落物理行序。
	ordered := sortRowKeys(snaps, targets)
	if err := mirror.ReorderDetailRows(ctx, sessionID, tableID, ordered, writeGrant); err != nil {
		// 权威行序已生效、仅 editor 渲染通知失败 → 按成功继续（勿回滚业务流程），仅告警。
		var rne *plugin.RenderNotifyError
		if errors.As(err, &rne) {
			logf(ctx, "WARN", "明细排序权威行序已生效、仅渲染通知失败（按已生效继续）: %v", err)
		} else {
			logf(ctx, "ERROR", "明细排序 ReorderDetailRows 失败: %v", err)
			return failure("SORT_REORDER_FAILED", fmt.Sprintf("重排明细行失败: %v", err)), nil
		}
	}
	logf(ctx, "INFO", "明细排序完成: 表 %s 按 %d 列重排 %d 行 (form_mode=%s)", tableID, len(targets), len(snaps), formMode)
	return &bizpb.ExecuteResponse{Success: true}, nil
}

// ValidateParams 两层校验：① SDK schema 浅校验；② 排序键业务校验（非空/字段必填/同一明细表/order 合法）。
func (s *Service) ValidateParams(_ context.Context, req *bizpb.ValidateParamsRequest) (*bizpb.ValidateParamsResponse, error) {
	paramsMap, _ := structAsMap(req.GetParams()).(map[string]any)
	var errs []*bizpb.ValidationError
	schemaJSON, _ := json.Marshal(paramsSchemaMap())
	for _, v := range plugin.ValidateParamsAgainstSchema(schemaJSON, paramsMap) {
		errs = append(errs, &bizpb.ValidationError{FieldPath: v.FieldPath, Message: v.Message})
	}
	if _, msg := parseSortTargets(paramsMap); msg != "" {
		errs = append(errs, &bizpb.ValidationError{FieldPath: "targets", Message: msg})
	}
	if len(errs) > 0 {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: errs}, nil
	}
	return &bizpb.ValidateParamsResponse{Valid: true}, nil
}

// parseSortTargets 解析并校验排序键列表；返回 (targets, 错误消息)，消息为空即通过。
// 约束：至少一条；field_id/data_table_id 必填；全部行属于同一明细表；order 只允许 asc/desc（空=asc）；
// 同一字段不得重复出现（重复键排序无意义，多半是误配）。
func parseSortTargets(params map[string]any) ([]sortTarget, string) {
	raw, ok := params["targets"]
	if !ok {
		return nil, "缺少排序键设置 targets"
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Sprintf("排序键设置无法序列化: %v", err)
	}
	var targets []sortTarget
	if err := json.Unmarshal(buf, &targets); err != nil {
		return nil, fmt.Sprintf("排序键设置无法解析: %v", err)
	}
	if len(targets) == 0 {
		return nil, "至少配置一个排序字段"
	}
	seen := make(map[string]bool, len(targets))
	for i := range targets {
		t := &targets[i]
		t.DataTableID = strings.TrimSpace(t.DataTableID)
		t.FieldID = strings.TrimSpace(t.FieldID)
		t.Order = strings.ToLower(strings.TrimSpace(t.Order))
		if t.DataTableID == "" {
			return nil, fmt.Sprintf("第 %d 个排序键缺少目标明细表（data_table_id）", i+1)
		}
		if t.DataTableID != targets[0].DataTableID {
			return nil, "所有排序键必须属于同一张明细表"
		}
		if t.FieldID == "" {
			return nil, fmt.Sprintf("第 %d 个排序键缺少排序字段（field_id）", i+1)
		}
		if seen[t.FieldID] {
			return nil, fmt.Sprintf("排序字段重复: %s", t.FieldID)
		}
		seen[t.FieldID] = true
		switch t.Order {
		case "", "asc":
			t.Order = "asc"
		case "desc":
		default:
			return nil, fmt.Sprintf("第 %d 个排序键的 order 只能是 asc/desc（当前 %q）", i+1, t.Order)
		}
	}
	return targets, ""
}

// rowSnap 明细行快照最小子集：row_key + cells（fieldID → raw 值），对应宿主表单数据层读表输出。
type rowSnap struct {
	RowKey string             `json:"row_key"`
	Cells  map[string]cellVal `json:"cells"`
}

type cellVal struct {
	Raw any `json:"raw"`
}

func parseRowSnapshots(rowsJSON []byte) ([]rowSnap, error) {
	var snaps []rowSnap
	if len(rowsJSON) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(rowsJSON, &snaps); err != nil {
		return nil, err
	}
	return snaps, nil
}

// sortRowKeys 多列稳定排序：从最后一个键到第一个键依次 stable sort，使 targets[0] 成为最高优先级键
// （与取数 PostSort applyPostSort 同款多键反序法）。返回排序后的 rowKey 序列。
func sortRowKeys(snaps []rowSnap, targets []sortTarget) []string {
	for i := len(targets) - 1; i >= 0; i-- {
		key := targets[i]
		desc := key.Order == "desc"
		sort.SliceStable(snaps, func(a, b int) bool {
			return lessCellRaw(snaps[a].Cells[key.FieldID].Raw, snaps[b].Cells[key.FieldID].Raw, desc)
		})
	}
	ordered := make([]string, len(snaps))
	for i, r := range snaps {
		ordered[i] = r.RowKey
	}
	return ordered
}

// lessCellRaw 单列排序 less 判定（与取数 PostSort 的 lessCellRaw 同语义，受控重复）。不变式：
//   - null（无值，raw==nil）恒排在有值之后（与 asc/desc 无关）；
//   - 两值都非空时优先数值比较（两值都能解析为数字），否则按字符串比较；
//   - desc 时反转「有值之间」的相对序（不影响 null-last）；
//   - 相等返回 false（保持 stable 多键排序的稳定性）。
func lessCellRaw(a, b any, desc bool) bool {
	aNull := a == nil
	bNull := b == nil
	if aNull || bNull {
		if aNull && bNull {
			return false
		}
		return bNull // a 有值、b null → a 在前；null 恒排末尾
	}
	af, aok := rawAsFloat(a)
	bf, bok := rawAsFloat(b)
	var less bool
	if aok && bok {
		if af == bf {
			return false
		}
		less = af < bf
	} else {
		sa, sb := fmt.Sprint(a), fmt.Sprint(b)
		if sa == sb {
			return false
		}
		less = sa < sb
	}
	if desc {
		return !less
	}
	return less
}

// rawAsFloat 尝试把单元格原始值解析为数字；仅当两值都成功才走数值比较，否则回退字符串比较。
func rawAsFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// —— 共用小工具（数据动作子包同构样板，受控重复）——

func contextMap(ctx *structpb.Struct) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	return ctx.AsMap()
}

func structAsMap(s *structpb.Struct) any {
	if s == nil {
		return map[string]any{}
	}
	return s.AsMap()
}

func failure(code, msg string) *bizpb.ExecuteResponse {
	return &bizpb.ExecuteResponse{Success: false, Message: msg, ErrorCode: code}
}

// logf 关键位置分级日志：stderr 兜底 + host 结构化通道上报给宿主。
func logf(ctx context.Context, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[detail-sort][%s] %s", level, msg)
	if h := plugin.HostFrom(ctx); h != nil && h.Log() != nil {
		_ = h.Log().Write(ctx, plugin.LogEntry{
			Severity: strings.ToLower(level),
			Module:   "detail-sort",
			Message:  msg,
		})
	}
}
