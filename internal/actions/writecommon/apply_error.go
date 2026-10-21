// apply_error.go — 三写动作（新增/修改/删除数据）共用的 Apply 失败分类。
//
// 背景：表单会话持有目标记录时，宿主表单数据层的写围栏会拒绝整批写入（宿主 Apply 返回 gRPC
// FailedPrecondition，文案含 "Mirror session" 或 "write fence" 字样）。若把
// plugin.ApplyMutation 的任何失败一律折成 WRITE_APPLY_FAILED「宿主未接受本次写入令牌」，
// UI/审计就看不到"记录正被表单会话占用"这层业务原因。本文件把分类逻辑收在一处，三个写动作包
// （create/update/delete）复用，避免各自独立实现导致口径漂移。
//
// 🔴 codes.FailedPrecondition 在宿主 Apply 侧同时承载多种非围栏含义（变更集过期、内容摘要
// 不匹配、幂等键冲突等），不能只按 gRPC 码分流，必须落到消息内容——这也是本文件
// 不直接用 status.Code(err) 判据的原因。
package writecommon

import (
	"errors"
	"strings"

	"google.golang.org/grpc/status"
)

// grpcStatuser 镜像 grpc-go status.FromError 内部使用的判定接口。
//
// 🔴 刻意不直接调用 status.FromError：该函数在 errors.As 命中被 fmt.Errorf("...: %w", err)
// 包裹的宿主状态错误时，会把返回 Status 的 Message 整体替换成**外层** err.Error()
// （即含 SDK facade "alice: apply change set: " 前缀 + "rpc error: code = ... desc = ..."
// 样板文本的完整串，见 grpc-go status.FromError 源码对 errors.As 分支的处理），不是宿主原始
// desc。这里直接拿 GRPCStatus() 的返回值，取到宿主围栏拒绝时真正写下的那句
// 文案，供围栏关键字匹配与 {reason} 插值使用，不带包装噪音。
type grpcStatuser interface{ GRPCStatus() *status.Status }

// hostApplyMessage 取 ApplyMutation 失败 err 链里最贴近宿主的 gRPC 状态文案。
//
// 🔴 err 链上找不到 gRPC 状态时返回空串，**不**退回 err.Error() 兜底：本函数的结果会作为
// {reason} 直接进入用户可见文案，只有"宿主明确签发的 gRPC 状态描述"才是可信、不带调用方
// 敏感上下文的文本（宿主 Apply 的具名错误分支都不回显 command_grant/ticket）。
// err 是别的什么东西（本地/传输层原始错误、测试桩直接构造的 errors.New）时，没有这份
// 保证，宁可不展示 reason，也不能透传可能夹带令牌的原始文本——三个写动作包 service_test.go
// 的 TestExecute_ApplyFailureDoesNotFallBack 泄漏防护用例正是用一个不带 gRPC 状态、
// 内容形似令牌的 errors.New 钉住这条底线。
func hostApplyMessage(err error) (msg string, ok bool) {
	var gs grpcStatuser
	if errors.As(err, &gs) {
		if st := gs.GRPCStatus(); st != nil {
			return st.Message(), true
		}
	}
	return "", false
}

// ClassifyApplyError 判定一次 plugin.ApplyMutation 失败是否为宿主表单数据层写围栏拒绝，并带回可展示
// 的宿主状态文案（reason）。
//
//   - rejected=true：err 消息含 "Mirror session" 或 "write fence"，对应宿主围栏
//     的三种拒绝文案（目标记录被他方表单会话持有 / 围栏鉴权权威
//     不可用 / 围栏拒绝且带底层原因）——调用方应改用 WRITE_FENCE_REJECTED +
//     write.common.fence_rejected 固定文案（不透传 reason，围栏场景的用户动作是"关闭编辑页
//     重试"，不需要底层细节）。
//   - rejected=false：其它 Apply 失败，reason 是宿主状态文案（err 链上找不到 gRPC 状态时
//     reason=""，调用方按空文案渲染，见 hostApplyMessage 注释），供调用方作为 {reason} 插值
//     追加进 write.common.apply_failed，帮助 UI/审计定位具体原因；err 为 nil 时返回
//     rejected=false、reason=""（调用方不应在 err==nil 时调用本函数，这里仅做防御）。
func ClassifyApplyError(err error) (rejected bool, reason string) {
	if err == nil {
		return false, ""
	}
	msg, ok := hostApplyMessage(err)
	if !ok {
		return false, ""
	}
	if strings.Contains(msg, "Mirror session") || strings.Contains(msg, "write fence") {
		return true, msg
	}
	return false, msg
}
