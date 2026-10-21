package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kimposoft/kimpo-plugin-sdk/direction/biz"
	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

// ActionName 是提取动作名（业务命名空间 data.*）。
const ActionName = "data.extract_record"

// extractV4SchemaVersion 是取数节点文档 v4（`@kimposoft/expression-editor` 共享包中
// V4ExtractNodeDocument.schema_version 常量）在插件侧的镜像值——ValidateParams 浅校验用它
// 判定 extract_v4 是否为认可的文档形态，两侧必须字面量一致，不得各自漂移。
const extractV4SchemaVersion = "kimpo.extract-node/v4"

// Service 实现 biz.BusinessPlugin：贡献并执行 data.extract_record 动作。
// 执行态经宿主反向通道（host.Query）取数，故必须运行在已注入回连令牌的实例上。
type Service struct {
	bizpb.UnimplementedBusinessPluginServer
	detailWriteChunkSize int
}

func New() *Service { return &Service{} }

func (s *Service) SetDetailWriteChunkSize(size int) {
	if size < 0 {
		size = 0
	}
	s.detailWriteChunkSize = size
}

// extractParamsSchemaMap 是 data.extract_record 参数的 JSON-Schema（**单一来源**）：GetActions 据此向
// 宿主声明、ValidateParams 据此做轻量 schema 前置校验（plugin.ValidateParamsAgainstSchema）。
// 两处共用同一份，避免 schema 与校验漂移。
func extractParamsSchemaMap() map[string]any {
	// 参数 schema 由公开 SDK helper 按种类产出，与第三方数据动作同一来源；
	// 输出与金样逐字节一致（params_schema_golden_test.go 守护）。
	// 引擎键（extract_v4 / write_v4 等）、x-editor、x-write-operation、x-tabs 的语义见 SDK direction/biz/dataaction.go。
	return biz.MustDataActionParamsSchema(biz.DataActionFetch, nil)
}

// GetActions 声明提取动作及其参数 schema（宿主 registry 据此做通用 schema 前置校验）。
//
// TODO(i18n): title/description 与 params_schema 内嵌 label（x-tabs 等）暂不做 per-user 本地化——
// 动作目录是单快照聚合（宿主一次拉取缓存，无请求 locale），须待目录支持 localized 字段后统一迁移；
// 目前后端 i18n 只覆盖 Execute/ValidateParams 用户可见文案。
func (s *Service) GetActions(_ context.Context, _ *bizpb.GetActionsRequest) (*bizpb.GetActionsResponse, error) {
	schema, err := structpb.NewStruct(extractParamsSchemaMap())
	if err != nil {
		return nil, err
	}
	return &bizpb.GetActionsResponse{
		Actions: []*bizpb.ActionDefinition{
			{
				Name: ActionName,
				// manifest.json 的 name 是插件整体名（"数据动作"），不能代表本动作的标题，故用字面量。
				Title:        "提取数据",
				Description:  "经宿主查询服务执行结构化取数，并通过宿主表单数据层原子写回目标字段。",
				ParamsSchema: schema,
				Style:        &bizpb.ActionStyle{Shape: "rounded", Color: "primary"},
				// 提取经宿主表单数据层写业务字段 = 副作用动作：test 态必须被宿主拦截而非真写；
				// 取数往返可异步；参数走标准化「来源+条件+目标」表达式编辑器（x-editor:expression）故 expression_based=true；
				// scheduled_context=true：本动作不依赖表单会话/editor，可在定时任务 headless 上下文执行。
				Capabilities: &bizpb.ActionCapabilities{Asyncable: true, SideEffecting: true, ExpressionBased: true, ScheduledContext: true},
				// 标准动作面板排序权重 10（提取数据排最前）。
				SortWeight: 10,
				// 动作面板功能分类（设计器右栏页签聚合依据；标准动作另有宿主 canonical 兜底防旧二进制漂移）。
				Category: "数据",
			},
		},
	}, nil
}

// Execute 是插件侧「误调用」的安全网，而不是正常执行路径：V4 取数节点由宿主在网关执行链
// 里直接编译执行并写回宿主表单数据层。🔴 宿主 handler 始终会调用插件 Execute，只在已处理时经
// query_context 注入回执键让插件自己短路；因此本方法在 V4 形态下**会**被触达，必须先认回执再报错。
// kimpo_sql（V2）与 expression（V3.4）两个历史配置若绕过宿主网关误达本方法，一律具名拒绝
// （插件内已无可执行的旧引擎）；除此之外（即当前唯一合法形态 extract_v4）落到最后一行，返回具名错误
// EXTRACT_HOST_HANDLED_ONLY——保留可观测的错误比让 Execute 整段退役、误调时静默消失更安全。
func (s *Service) Execute(ctx context.Context, req *bizpb.ExecuteRequest) (*bizpb.ExecuteResponse, error) {
	ctxMap := contextMap(req.GetContext())
	// 用户 locale 由宿主 HTTP 边界协商后经 TriggerContext 透传；空=宿主未透传，tr.T 自动回落默认语言。
	loc := req.GetTriggerContext().GetLocale()

	// 顶层 KimpoSQL 必须在 expression/Host 能力探测前具名拒绝。不保留
	// “旧配置原样执行”或空字符串豁免。
	if params, ok := ctxMap["params"].(map[string]any); ok {
		if _, present := params["kimpo_sql"]; present {
			return failure(loc, publicSchemaCodeKimpoSQLRetired, publicSchemaMessageKey(publicSchemaCodeKimpoSQLRetired), nil), nil
		}
		// params 带 expression＝V3.4 旧格式取数节点。插件已无对应执行路径，宿主网关也会对旧格式
		// 节点先行具名拒绝，这里是误达插件时的安全网：与 kimpo_sql 同款，在任何 Host IO 之前
		// 具名拒绝，不静默改判为 V4。
		// 宿主「已处理回执」只认 V4 形态（见下方），旧格式节点不享受回执短路。
		if _, hasExpr := params["expression"]; hasExpr {
			logf(ctx, "warn", "legacy V3.4 expression extract node reached plugin Execute; rejected with %s", publicSchemaCodeExpressionLegacyRetired)
			return failure(loc, publicSchemaCodeExpressionLegacyRetired, publicSchemaMessageKey(publicSchemaCodeExpressionLegacyRetired), nil), nil
		}
	}
	// 走到这里＝既非 kimpo_sql 也非 expression（即当前 V4 唯一合法形态 extract_v4）。
	//
	// 🔴 这里必须先看宿主的 handled 回执，再决定是短路还是报错：宿主 handler 始终会调用插件
	// Execute，只在已处理时额外往 query_context 注入回执键，指望插件自己短路。若直接落具名错误，
	// 宿主已把数据写进表单数据层（画布可见、保存能落库），动作却对外报失败，用户一重试就重复写。
	//
	// 插件不解析取数文档（extract_v4 的图表目标由宿主处理），本地没有可与 receipt.ChartCount
	// 比对的图表目标，故只认 handled 本身。
	qc, _ := ctxMap["query_context"].(map[string]any)
	if receipt, ok := plugin.ParseRecordExtractExecutionTimeHandled(qc); ok {
		return attachOutcome(&bizpb.ExecuteResponse{Success: true}, receipt.RowCount > 0), nil
	}
	// 没有回执＝宿主确实没处理却调了插件：保留具名错误使"路由误调用"可观测（这才是本错误码
	// 当初被设计出来的用途）。
	return failure(loc, "EXTRACT_HOST_HANDLED_ONLY", "extract.host_handled_only", nil), nil
}

// attachOutcome 把出口分支结果（filled=有数据 / empty=无数据）写进 ExecuteResponse.result.outcome
// （网关 outcomeFromResult 约定键）：Result 已存在（如图表 effects）则并入，不存在则新建。
// 序列化失败静默放行原响应——outcome 是路由增强，不应因编码问题打断本已成功的执行结果。
func attachOutcome(resp *bizpb.ExecuteResponse, filled bool) *bizpb.ExecuteResponse {
	outcome := "empty"
	if filled {
		outcome = "filled"
	}
	if resp.Result == nil {
		result, err := structpb.NewStruct(map[string]any{"outcome": outcome})
		if err != nil {
			return resp
		}
		resp.Result = result
		return resp
	}
	if resp.Result.Fields == nil {
		resp.Result.Fields = map[string]*structpb.Value{}
	}
	resp.Result.Fields["outcome"] = structpb.NewStringValue(outcome)
	return resp
}

// ValidateParams 只接受 extract_v4 文档：先具名拒绝已退役的公开形状，再做 SDK schema
// 校验与 extract_v4 / extract_v4_plans 浅校验。
func (s *Service) ValidateParams(ctx context.Context, req *bizpb.ValidateParamsRequest) (*bizpb.ValidateParamsResponse, error) {
	paramsMap, _ := structAsMap(req.GetParams()).(map[string]any)
	// 已退役的 public schema 形状不能被 SDK schema 或 exprModel typed decode 抢先折叠成通用错误。
	if _, present := paramsMap["kimpo_sql"]; present {
		violation := &publicSchemaViolation{Code: publicSchemaCodeKimpoSQLRetired, Path: "$.kimpo_sql"}
		return invalid("kimpo_sql", publicSchemaValidationMessage(violation)), nil
	}
	if expression, present := paramsMap["expression"]; present {
		if violation, err := detectRetiredExpressionValue(expression); err == nil && violation != nil {
			return invalid(publicSchemaFieldPath(violation), publicSchemaValidationMessage(violation)), nil
		}
	}

	var errs []*bizpb.ValidationError
	// ① schema 层：复用 SDK helper，违规项映射为 biz.ValidationError（required=extract_v4、
	// additionalProperties=false 等）。旧 expression 键（3.4）不在本 schema 白名单内，携带该键的
	// 旧格式节点会在此处被结构性拒绝（"不接受的参数 expression" + "缺少必填参数 extract_v4"）；
	// 具名的「旧格式已停用」用户提示由宿主保存检查负责给出。
	schemaJSON, _ := json.Marshal(extractParamsSchemaMap())
	for _, v := range plugin.ValidateParamsAgainstSchema(schemaJSON, paramsMap) {
		errs = append(errs, &bizpb.ValidationError{FieldPath: v.FieldPath, Message: v.Message})
	}
	if len(errs) > 0 {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: errs}, nil
	}

	// ② extract_v4 浅校验：只确认是对象且 schema_version 与 v4 常量一致。sources/joins/targets 等
	// 结构化深校验归宿主保存检查实现，插件侧不重复一份、避免两处校验口径漂移——ctx 在本方法
	// 内无其它用途，保留形参仅为满足 biz.BusinessPlugin 接口签名。
	v4, _ := paramsMap["extract_v4"].(map[string]any)
	if sv, _ := v4["schema_version"].(string); sv != extractV4SchemaVersion {
		return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{
			{FieldPath: "extract_v4.schema_version", Message: trDefault("extract.validate.extract_v4_schema_version_mismatch", nil)},
		}}, nil
	}

	// ③ extract_v4_plans 浅校验：宿主在模板
	// 发布时把冻结好的发布物钉入键写进本节点 params 的**兄弟键**，形如
	// [{"target_id"|"chart_id": "...", "publication_id": "..."}]。① 的 schema 层已确认它（若在场）
	// 顶层类型是 array——这里只再补一层它自己不做的检查：数组元素须都是对象。插件不解释键值本身
	// （不校验 target_id/chart_id 取值域、不校验 publication_id 格式、不做去重），形状不对说明宿主
	// 装配出错，比静默放行更安全；不在场（未发布过的节点）视为合法。
	if raw, present := paramsMap["extract_v4_plans"]; present {
		invalidShape := false
		if plans, ok := raw.([]any); ok {
			for _, item := range plans {
				if _, isObj := item.(map[string]any); !isObj {
					invalidShape = true
					break
				}
			}
		} else {
			invalidShape = true
		}
		if invalidShape {
			return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{
				{FieldPath: "extract_v4_plans", Message: trDefault("extract.validate.extract_v4_plans_shape_invalid", nil)},
			}}, nil
		}
	}
	return &bizpb.ValidateParamsResponse{Valid: true}, nil
}

// —— 上下文 / 编码小工具 ——

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

// failure 构造用户可见的失败响应（i18n 两级查找契约）：message 用插件语言包按透传 loc 翻译作
// fallback，message_key 随线带回——插件侧 miss（空串）时宿主 HTTP 边界按请求 locale 经中央库兜底重译。
func failure(loc, code, key string, params map[string]any) *bizpb.ExecuteResponse {
	return biz.Fail(code, key, tr.T(loc, key, params))
}

func invalid(field, msg string) *bizpb.ValidateParamsResponse {
	return &bizpb.ValidateParamsResponse{Valid: false, Errors: []*bizpb.ValidationError{{FieldPath: field, Message: msg}}}
}

// logf 在关键位置按级别上报日志：已收敛到 SDK 分级日志器 host.Logger()（源头级别 guard +
// 客户端攒批 + stderr 双写），在关键位置把不同级别日志上报给宿主。
// 实例级别来自 config_json 顶层 log_level（默认 warn）：低于阈值的日志被源头整条丢弃。
// host 未注入（裸进程/无回连令牌）时 fallback 仅写 stderr，不阻断。
func logf(ctx context.Context, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if h := plugin.HostFrom(ctx); h != nil {
		if lg := h.Logger(); lg != nil {
			// stderr 由 SDK 分级器双写承担，此处不再自打，避免重复两行。
			lg.Emit(ctx, strings.ToLower(level), "plugin", "extraction", "", msg, nil)
			return
		}
	}
	log.Printf("[extraction][%s] %s", level, msg)
}
