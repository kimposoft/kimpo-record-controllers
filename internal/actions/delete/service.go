package deleteaction

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/direction/biz"
	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"

	"github.com/kimposoft/kimpo-record-controllers/internal/actions/writecommon"
)

// —— data.delete_record（删除数据）——
//
// 宿主数据语义服务的写入令牌（MutationTicket）是唯一生产写路径：宿主执行期负责定位、软删除、CAS、
// Derive 与 Seal，插件只把 query_context 中的一次性不透明 MutationTicket 交回 SDK Apply facade。
// 缺少 ticket 时必须在任何宿主数据语义服务 RPC 前拒绝，不得回落旧 selector、裸 sealed/hash 或
// RecordWrite。
//
// 配置面：参数键是 V4 写节点文档 `write_v4`（schema_version=kimpo.write-node/v4），旧参数键
// `expression` 已退役。插件侧不解析 target/source/where 等深层结构，也不做"定位条件是否为空"
// 这类深层校验，理由同 update 包同名注释：该校验由设计态客户端闸负责，本包只保留浅到不能再浅的
// 两条形状校验（write_v4 是对象且 schema_version/operation 对得上）。删除动作的级联策略
// （主表恒级联同模板扩展表、扩展表不级联）不由配置决定，本文件不涉及。

const (
	ActionName = "data.delete_record"
	// writeOperation 是本插件对应的写节点操作；ValidateParams/Execute 强制一致（防串挂）。
	writeOperation = "delete"
)

type Service struct {
	bizpb.UnimplementedBusinessPluginServer
}

func New() *Service { return &Service{} }

// paramsSchemaMap 参数 JSON-Schema 单一来源：GetActions 声明 + ValidateParams 浅校验共用。
func paramsSchemaMap() map[string]any {
	// 参数 schema 由公开 SDK helper 按动作种类产出，与第三方数据动作同一来源；extra 为空时输出与
	// 金样逐字节一致（params_schema_golden_test.go 守护）。
	// 引擎键（extract_v4 / write_v4 等）、x-editor、x-write-operation、x-tabs 的语义见 SDK direction/biz/dataaction.go。
	return biz.MustDataActionParamsSchema(biz.DataActionDelete, nil)
}

// GetActions 声明删除数据动作（SideEffecting：test 态被宿主拦截；参数走 V4 写配置面）。
func (s *Service) GetActions(_ context.Context, _ *bizpb.GetActionsRequest) (*bizpb.GetActionsResponse, error) {
	schema, err := structpb.NewStruct(paramsSchemaMap())
	if err != nil {
		return nil, err
	}
	return &bizpb.GetActionsResponse{
		Actions: []*bizpb.ActionDefinition{
			{
				Name: ActionName,
				// 插件 manifest.json 的 name 是四个动作的合体名（"数据动作"），不能代表本动作，
				// 故标题用字面量。
				Title:        "删除数据",
				Description:  "由宿主数据语义服务按当前动作配置在执行期编译、定位并软删除记录，插件仅呈递一次性写入令牌。",
				ParamsSchema: schema,
				Style:        &bizpb.ActionStyle{Shape: "rounded", Color: "danger"},
				// scheduled_context=true：不依赖表单会话/editor，可在定时任务 headless 上下文执行。
				// asyncable=true（理由同 create 包同名字段注释：插件只声明"可以异步"，具体节点
				// 是否异步由设计器决定、默认同步）。
				Capabilities: &bizpb.ActionCapabilities{Asyncable: true, SideEffecting: true, ExpressionBased: true, ScheduledContext: true},
				// 标准动作面板排序权重 40（删除数据排在明细排序之前）。
				SortWeight: 40,
				// 动作面板功能分类（设计器右栏页签聚合依据；标准动作另有宿主 canonical 兜底防旧二进制漂移）。
				Category: "数据",
			},
		},
	}, nil
}

// Execute 执行一次删除：write_v4 浅校验 → 行数阈值 → 解析写入令牌 ticket → 经 SDK facade Apply。
func (s *Service) Execute(ctx context.Context, req *bizpb.ExecuteRequest) (*bizpb.ExecuteResponse, error) {
	ctxMap := contextMap(req.GetContext())
	// 用户 locale 由宿主 HTTP 边界协商后经 TriggerContext 透传；空=老宿主，writecommon.T 自动回落
	// 默认语言（三写动作错误信息四语 i18n，取法同 extract/service.go）。
	loc := req.GetTriggerContext().GetLocale()
	params, _ := ctxMap["params"].(map[string]any)
	if key, kp := writecommon.ValidateWriteV4Key(params, writeOperation); key != "" {
		return failure(loc, "WRITE_BAD_PARAMS", key, kp), nil
	}
	// 行数阈值越界必须先于任何宿主数据语义服务 RPC 拦下，理由同 create 包同名检查注释。
	if maxAffectedRowsOutOfRange(params) {
		return failure(loc, "WRITE_MAX_AFFECTED_ROWS_OUT_OF_RANGE", "write.delete.max_affected_rows_out_of_range", nil), nil
	}
	// 定位条件（筛选）是否为空的校验由设计态客户端闸在保存前阻断，理由同 update 包同名检查注释。

	qc, _ := ctxMap["query_context"].(map[string]any)
	ticket, ok := plugin.ParseMutationTicket(qc)
	if !ok {
		return failure(loc, "WRITE_MUTATION_TICKET_REQUIRED", "write.common.mutation_ticket_required", nil), nil
	}
	host := plugin.HostFrom(ctx)
	if host == nil || host.Alice() == nil {
		return failure(loc, "WRITE_NO_HOST", "write.common.no_host", nil), nil
	}
	return applyMutationTicketDelete(ctx, loc, host, ticket), nil
}

// applyMutationTicketDelete 只经 SDK facade 呈递不透明 ticket。Apply 失败直接返回，不回落任何旧写轨。
func applyMutationTicketDelete(ctx context.Context, loc string, host plugin.Host, ticket plugin.MutationTicket) *bizpb.ExecuteResponse {
	logf(ctx, "INFO", "宿主数据语义服务: 收到不透明写入令牌，直接 Apply")
	receipt, err := plugin.ApplyMutation(ctx, host, ticket)
	if err != nil {
		logf(ctx, "ERROR", "宿主数据语义服务删除 Apply 失败")
		// 按 writecommon.ClassifyApplyError 分流围栏拒绝 vs 其它 Apply 失败，理由同 create 包
		// applyMutationTicketCreate 的同名注释。
		if rejected, reason := writecommon.ClassifyApplyError(err); rejected {
			return failure(loc, "WRITE_FENCE_REJECTED", "write.common.fence_rejected", nil)
		} else {
			return failure(loc, "WRITE_APPLY_FAILED", "write.common.apply_failed", map[string]any{"reason": reason})
		}
	}
	if !receipt.Succeeded() {
		return failure(loc, "WRITE_FAILED", "write.common.failed", map[string]any{"status": receipt.Status})
	}
	logf(ctx, "INFO", "删除完成: 宿主数据语义服务 Apply 成功（rows=%d, trace=%s）", receipt.AffectedRows, receipt.TraceID)
	for _, warning := range receipt.Warnings {
		logf(ctx, "WARN", "宿主数据语义服务删除告警: code=%s message_key=%s", warning.Code, warning.MessageKey)
	}
	return &bizpb.ExecuteResponse{Success: true}
}

// ValidateParams 两层校验：① SDK schema 浅校验；② write_v4 浅校验（operation 匹配）+ 行数阈值。
// 深层结构（定位条件非空、禁字段映射等）由宿主保存闸 + 设计态客户端闸负责。
//
// loc 取自 req.GetLocale()（biz.proto 的 locale 字段）。空串（老宿主未透传/未识别到 locale）时
// writecommon.T 自动回落默认语言。
func (s *Service) ValidateParams(_ context.Context, req *bizpb.ValidateParamsRequest) (*bizpb.ValidateParamsResponse, error) {
	loc := req.GetLocale()
	paramsMap, _ := structAsMap(req.GetParams()).(map[string]any)

	// 存量旧格式节点（参数键 expression、无 write_v4）必须先于下面的 schema 浅校验具名拒绝——
	// paramsSchemaMap 是 additionalProperties:false，一旦先跑 schema 校验，老节点就会被结构性
	// 拒绝成一句「不接受该参数 expression」，看不出"请重新配置"这层业务含义；口径同宿主保存闸
	// 的 EXPR_WRITE_LEGACY_RETIRED。
	if verr, ok := writecommon.RejectLegacyExpression(loc, paramsMap); ok {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{verr}}, nil
	}

	var errs []*bizpb.ValidationError
	schemaJSON, _ := json.Marshal(paramsSchemaMap())
	for _, v := range plugin.ValidateParamsAgainstSchema(schemaJSON, paramsMap) {
		errs = append(errs, &bizpb.ValidationError{FieldPath: v.FieldPath, Message: v.Message})
	}
	if len(errs) > 0 {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: errs}, nil
	}

	if key, kp := writecommon.ValidateWriteV4Key(paramsMap, writeOperation); key != "" {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{
			{FieldPath: "write_v4", Message: writecommon.T(loc, key, kp)},
		}}, nil
	}

	// max_affected_rows 是 write_v4 之外的顶层框架参数（"选项"页签），单独用自己的 FieldPath
	// 上报。
	if maxAffectedRowsOutOfRange(paramsMap) {
		errs = append(errs, &bizpb.ValidationError{FieldPath: "max_affected_rows", Message: writecommon.T(loc, "write.delete.max_affected_rows_out_of_range", nil)})
	}
	if len(errs) > 0 {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: errs}, nil
	}
	return &bizpb.ValidateParamsResponse{Valid: true}, nil
}

// maxAffectedRowsOutOfRange 校验 params.max_affected_rows 行数阈值是否越界：未配置（键缺失或
// 显式 null）视为合法；配置了则必须是非负整数（0 = 不限，与宿主写节点文档只拒负数同口径）。
// 三写插件各自独立持有一份（受控重复，理由见 create 包同名函数注释）。
func maxAffectedRowsOutOfRange(params map[string]any) bool {
	raw, ok := params["max_affected_rows"]
	if !ok || raw == nil {
		return false
	}
	n, ok := raw.(float64) // 经 structpb.Struct.AsMap() 解码，JSON 数值统一为 float64
	if !ok {
		return true
	}
	if n != math.Trunc(n) {
		return true
	}
	// 不设上限；下界为 0：负数拒、0 = 不限，与宿主写节点文档（MaxAffectedRows < 0 才拒）及
	// SDK schema minimum: 0 一致。留空同样不限。
	return n < 0
}

// —— 共用小工具（三写插件同构样板，受控重复）——

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

// failure 构造用户可见的失败响应（i18n 两级查找契约，同 extract/service.go）：message 用插件
// 语言包按透传 loc 翻译作 fallback，message_key 随线带回——插件侧 miss（空串）时宿主 HTTP
// 边界按请求 locale 经中央库兜底重译。
func failure(loc, code, key string, params map[string]any) *bizpb.ExecuteResponse {
	return biz.Fail(code, key, writecommon.T(loc, key, params))
}

// logf 关键位置分级日志：stderr 兜底 + host 结构化通道（插件须把关键日志上报宿主）。
func logf(ctx context.Context, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[record-delete][%s] %s", level, msg)
	if h := plugin.HostFrom(ctx); h != nil && h.Log() != nil {
		_ = h.Log().Write(ctx, plugin.LogEntry{
			Severity: strings.ToLower(level),
			Module:   "record-delete",
			Message:  msg,
		})
	}
}
