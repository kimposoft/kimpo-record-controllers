// write_v4.go — 三写动作（新增/修改/删除数据）共用的写节点文档 v4（write_v4）浅校验。
//
// 三写动作配置使用 V4 写节点文档参数键 `write_v4`（schema_version 常量 kimpo.write-node/v4，
// 与 `@kimposoft/expression-editor` 共享包的 WRITE_NODE_DOCUMENT_SCHEMA_VERSION 逐字镜像，
// 两侧必须一致）。插件侧不解析 sources/target/assignments 等深层结构——那是宿主保存检查的职责；
// 插件只做两条形状校验：write_v4 是对象且 schema_version 对得上、operation 与本动作一致。
// 定位条件（筛选）是否为空等业务语义校验由设计态配置面板负责，插件侧不重复。
//
// 三写动作 paramsSchemaMap() 是 additionalProperties:false，旧格式节点（参数键 `expression`）
// 在 plugin.ValidateParamsAgainstSchema 通用 schema 校验阶段就会被结构性拒绝，报出的是一句
// 无业务含义的「不接受参数 expression」。宿主保存检查对同一形状有具名判定 EXPR_WRITE_LEGACY_RETIRED
// （口径「旧配置已退役，请重新配置」），RejectLegacyExpression 在插件侧给出同一口径、且必须跑在
// schema 校验之前，见该函数注释。
package writecommon

import "github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"

// WriteV4SchemaVersion 是写节点文档 v4 的 schema_version 字面量镜像值，供三写插件 ValidateParams/
// Execute 的浅校验直接引用，避免各插件包各自手写字符串走样。
const WriteV4SchemaVersion = "kimpo.write-node/v4"

// RejectLegacyExpression 检测旧格式写节点（参数键 `expression`）：
// 若 params 含 expression 键且不含 write_v4 键，说明这是尚未重新配置的旧节点，必须在
// 调用方进入 plugin.ValidateParamsAgainstSchema 这道通用浅校验之前，先具名拒绝——同宿主保存检查
// EXPR_WRITE_LEGACY_RETIRED 的口径「旧配置已退役，请重新配置」，而不是被
// additionalProperties:false 结构性拒绝成一句「不接受该参数 expression」。
//
// 三写插件 ValidateParams 必须先调用本函数、命中(ok=true)即直接返回，再走既有的 schema 校验 +
// ValidateWriteV4Key；顺序颠倒则本函数永远不可达（schema 校验会先行拒绝）。
//
// 返回值：ok=true 表示命中旧配置，verr 即应返回给调用方的具名校验错误（FieldPath="expression"）；
// ok=false 表示未命中——包括完全没有 expression 键，以及 expression、write_v4 两键同时存在的情形
// （此时视为已带上合法新格式，交给后续 ValidateWriteV4Key 校验 write_v4 本身，不在本函数拦截，
// 避免把"正在从旧格式过渡、临时保留旧键"的情形误判为退役）。
func RejectLegacyExpression(loc string, params map[string]any) (verr *bizpb.ValidationError, ok bool) {
	if params == nil {
		return nil, false
	}
	_, hasExpression := params["expression"]
	_, hasWriteV4 := params["write_v4"]
	if !hasExpression || hasWriteV4 {
		return nil, false
	}
	return &bizpb.ValidationError{
		FieldPath: "expression",
		Message:   T(loc, "write.common.validate.legacy_retired", nil),
	}, true
}

// ValidateWriteV4Key 校验 params.write_v4 的最浅形状：存在、是对象、schema_version 命中
// WriteV4SchemaVersion、operation 与调用方声明的 wantOp 一致。
//
// 返回值是 (i18n key, 插值参数)：key=""表示校验通过；调用方（Execute 走 failure()，ValidateParams
// 走 writecommon.T）按各自场景把 key 翻译成当前 locale 文案——同 create/update/delete 三包既有
// validateOpExtra 的"返回 key 不返回文案"约定，避免校验函数与 locale 查找耦合。
func ValidateWriteV4Key(params map[string]any, wantOp string) (string, map[string]any) {
	raw, ok := params["write_v4"]
	if !ok {
		return "write.common.bad_params.write_v4_missing", nil
	}
	wv4, ok := raw.(map[string]any)
	if !ok {
		return "write.common.bad_params.write_v4_missing", nil
	}
	if sv, _ := wv4["schema_version"].(string); sv != WriteV4SchemaVersion {
		return "write.common.bad_params.write_v4_missing", nil
	}
	op, _ := wv4["operation"].(string)
	if op != wantOp {
		return "write.common.bad_params.operation_mismatch", map[string]any{"got": op, "want": wantOp}
	}
	return "", nil
}
