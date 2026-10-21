package create

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

// —— data.create_record（新增数据）——
//
// 宿主数据语义服务的写入令牌（MutationTicket）是唯一生产写路径：宿主执行期完成配置编译、取数、
// Derive 与 Seal，并通过 query_context 投递一次性不透明 MutationTicket；插件只把 ticket 交回
// SDK Apply facade。缺少 ticket 时必须在任何宿主数据语义服务 RPC 前拒绝，绝不回落旧 plan-ref、
// 裸 sealed/hash、插件侧 Derive→Seal→Apply 或 RecordWrite。业务行、记录 ID、版本和父记录锚点
// 均不进入插件。
//
// 配置面：参数键是 V4 写节点文档 `write_v4`（schema_version=kimpo.write-node/v4），旧参数键
// `expression` 已退役。插件侧不解析 sources/target/assignments 等深层结构——那是宿主保存闸的
// 职责；本包只做浅到不能再浅的两条形状校验（write_v4 是对象且 schema_version/operation 对得上，
// 见 writecommon.ValidateWriteV4Key），深层校验（赋值非空、父锚定必填等）由设计态客户端闸
// （宿主动作配置面，基于 `@kimposoft/expression-editor` 共享包，保存前校验）负责，插件不重复。

const (
	ActionName = "data.create_record"
	// writeOperation 是本插件对应的写节点操作；ValidateParams/Execute 强制 params.write_v4.operation
	// 与之一致（三写插件共用同一份文档形状，靠 operation 分流，防止节点配置串挂）。
	writeOperation = "insert"
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
	return biz.MustDataActionParamsSchema(biz.DataActionCreate, nil)
}

// GetActions 声明新增数据动作。SideEffecting=true（写业务库，test 态被宿主拦截）；
// ExpressionBased=true（参数走标准化写配置面）；Asyncable=true（三写动作支持异步执行，插件
// 只声明"能力"，具体某个节点是否异步由设计器决定、默认同步——宿主动作管线用
// Capabilities.Asyncable 与节点级 node.Async 双层判定，节点级开关未显式勾选时 node.Async
// 零值为 false，即"全部默认同步、异步为可选"）。
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
				Title:        "新增数据",
				Description:  "由宿主数据语义服务按当前动作配置在执行期编译并新增记录，插件仅呈递一次性写入令牌。",
				ParamsSchema: schema,
				Style:        &bizpb.ActionStyle{Shape: "rounded", Color: "success"},
				// scheduled_context=true：不依赖表单会话/editor，可在定时任务 headless 上下文执行。
				Capabilities: &bizpb.ActionCapabilities{Asyncable: true, SideEffecting: true, ExpressionBased: true, ScheduledContext: true},
				// 标准动作面板排序权重 20（新增数据排在提取数据之后）。
				SortWeight: 20,
				// 动作面板功能分类（设计器右栏页签聚合依据；标准动作另有宿主 canonical 兜底防旧二进制漂移）。
				Category: "数据",
			},
		},
	}, nil
}

// Execute 执行一次新增：write_v4 浅校验 → 行数阈值 → 解析写入令牌 ticket → 经 SDK facade Apply。
func (s *Service) Execute(ctx context.Context, req *bizpb.ExecuteRequest) (*bizpb.ExecuteResponse, error) {
	ctxMap := contextMap(req.GetContext())
	// 用户 locale 由宿主 HTTP 边界协商后经 TriggerContext 透传；空=老宿主，writecommon.T 自动回落
	// 默认语言（三写动作错误信息四语 i18n，取法同 extract/service.go）。
	loc := req.GetTriggerContext().GetLocale()
	params, _ := ctxMap["params"].(map[string]any)
	if key, kp := writecommon.ValidateWriteV4Key(params, writeOperation); key != "" {
		return failure(loc, "WRITE_BAD_PARAMS", key, kp), nil
	}
	// 行数阈值越界必须先于任何宿主数据语义服务 RPC 拦下，给出专属错误码而不是任由宿主执行期
	// 静默钳位——钳位是宿主兜底，不是插件放弃早期反馈的理由。
	if maxAffectedRowsOutOfRange(params) {
		return failure(loc, "WRITE_MAX_AFFECTED_ROWS_OUT_OF_RANGE", "write.create.max_affected_rows_out_of_range", nil), nil
	}

	qc, _ := ctxMap["query_context"].(map[string]any)
	ticket, ok := plugin.ParseMutationTicket(qc)
	if !ok {
		return failure(loc, "WRITE_MUTATION_TICKET_REQUIRED", "write.common.mutation_ticket_required", nil), nil
	}
	host := plugin.HostFrom(ctx)
	if host == nil || host.Alice() == nil {
		return failure(loc, "WRITE_NO_HOST", "write.common.no_host", nil), nil
	}
	return applyMutationTicketCreate(ctx, loc, host, ticket), nil
}

// applyMutationTicketCreate 只经 SDK facade 呈递不透明 ticket，不构造宿主 wire 请求。
func applyMutationTicketCreate(ctx context.Context, loc string, host plugin.Host, ticket plugin.MutationTicket) *bizpb.ExecuteResponse {
	logf(ctx, "INFO", "宿主数据语义服务: 收到不透明写入令牌，直接 Apply")
	receipt, err := plugin.ApplyMutation(ctx, host, ticket)
	if err != nil {
		logf(ctx, "ERROR", "宿主数据语义服务新增 Apply 失败")
		// 表单会话持有目标记录时，宿主表单数据层的写围栏会拒绝整批写入。按
		// writecommon.ClassifyApplyError 分流，让用户看出"记录正被表单会话占用"：围栏拒绝给固定
		// 文案（不透传底层细节——此时用户该做的是"关闭编辑页重试"）；其它 Apply 失败把宿主状态
		// 文案作为 {reason} 插值带出，帮助 UI/审计定位（reason 只进入 failure() 的展示文案参数，
		// 不进 logf——不违反 mutation_ticket_log_ratchet_test 的泄漏防护纪律）。
		if rejected, reason := writecommon.ClassifyApplyError(err); rejected {
			return failure(loc, "WRITE_FENCE_REJECTED", "write.common.fence_rejected", nil)
		} else {
			return failure(loc, "WRITE_APPLY_FAILED", "write.common.apply_failed", map[string]any{"reason": reason})
		}
	}
	if !receipt.Succeeded() {
		return failure(loc, "WRITE_FAILED", "write.common.failed", map[string]any{"status": receipt.Status})
	}
	logf(ctx, "INFO", "新增完成: 宿主数据语义服务 Apply 成功（rows=%d, trace=%s）", receipt.AffectedRows, receipt.TraceID)
	for _, warning := range receipt.Warnings {
		logf(ctx, "WARN", "宿主数据语义服务新增告警: code=%s message_key=%s", warning.Code, warning.MessageKey)
	}
	return &bizpb.ExecuteResponse{Success: true}
}

// ValidateParams 两层校验：① SDK schema 浅校验（required=write_v4、additionalProperties=false
// 等）；② write_v4 浅校验（operation 与本动作一致）+ 行数阈值。深层结构（来源/关联/筛选/赋值/
// 父锚定）由宿主保存闸 + 设计态客户端闸负责，插件侧不重复一份、避免两处校验口径漂移。
//
// loc 取自 req.GetLocale()（biz.proto 的 locale 字段）：宿主保存动作文档时把请求 locale 一路
// 透传到这条 RPC。空串（老宿主未透传/未识别到 locale）时 writecommon.T 自动回落默认语言。
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

	// write_v4 浅校验：schema_version 与 operation 都对得上（旧 expression 键在 ①schema 层已被
	// additionalProperties:false 结构性拒绝，走不到这里）。
	if key, kp := writecommon.ValidateWriteV4Key(paramsMap, writeOperation); key != "" {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{
			{FieldPath: "write_v4", Message: writecommon.T(loc, key, kp)},
		}}, nil
	}

	// max_affected_rows 是 write_v4 之外的顶层框架参数（"选项"页签），单独用自己的 FieldPath
	// 上报，不与 write_v4 段的错误混在一起。
	if maxAffectedRowsOutOfRange(paramsMap) {
		errs = append(errs, &bizpb.ValidationError{FieldPath: "max_affected_rows", Message: writecommon.T(loc, "write.create.max_affected_rows_out_of_range", nil)})
	}
	if len(errs) > 0 {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: errs}, nil
	}
	return &bizpb.ValidateParamsResponse{Valid: true}, nil
}

// maxAffectedRowsOutOfRange 校验 params.max_affected_rows 行数阈值是否越界：未配置（键缺失或
// 显式 null）视为合法——该参数可选，见 paramsSchemaMap；配置了则必须是非负整数（0 = 不限，与
// 宿主写节点文档只拒负数同口径），非数值类型 / 非整数 / 负数都判定为越界。
//
// 三写插件各自独立持有一份本函数（受控重复，同 writecommon.ValidateWriteV4Key 等既有小工具的
// 既定做法）：逻辑本身足够简单，重复成本低于跨包共享的耦合成本。
//
// 宿主执行期另有行数钳位兜底，本函数只是插件侧的早期反馈，不改变宿主兜底。
func maxAffectedRowsOutOfRange(params map[string]any) bool {
	raw, ok := params["max_affected_rows"]
	if !ok || raw == nil {
		return false
	}
	// 经 structpb.Struct.AsMap()（Execute 的 ctxMap / ValidateParams 的 paramsMap 都走这条解码
	// 路径）解码，JSON 数值统一落地为 float64；非 float64 类型本身就是非法配置，直接判越界。
	n, ok := raw.(float64)
	if !ok {
		return true
	}
	if n != math.Trunc(n) {
		return true // 非整数
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

// logf 关键位置分级日志：stderr 兜底 + host 结构化通道（插件须把关键日志上报宿主）。host 未注入时不阻断。
func logf(ctx context.Context, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[record-create][%s] %s", level, msg)
	if h := plugin.HostFrom(ctx); h != nil && h.Log() != nil {
		_ = h.Log().Write(ctx, plugin.LogEntry{
			Severity: strings.ToLower(level),
			Module:   "record-create",
			Message:  msg,
		})
	}
}
