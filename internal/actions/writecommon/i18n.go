// i18n.go — 三写动作（新增/修改/删除数据）共用的插件后端多语言支持（两级查找方案，见 SDK
// 多语言文档）。结构与 extract/i18n.go 一致（同一方案的另一份实现）。
//
// 与 extract/i18n.go 的唯一差别：extract 的 tr/trDefault 是同包私有（extract/service.go 与
// i18n.go 同属 package extract，直接引用包级变量即可）；本包被 create/update/delete 三个
// 各自独立的 Go 包共同 import，故 T/DefaultLocale 两个入口必须导出。
//
// 🔴 本包不得 import 宿主 wire 包 hostpb（internal/actions/hostwire_import_ratchet_test.go
// 钉住生产源码不得直接消费宿主 wire 协议——插件看得到的类型就是对外契约面，直接暴露 wire 协议
// 等于把它变成公开 API）。本包只做语言文件加载与查表，不持有任何写入令牌或宿主应答类型，
// 天然无需该 import，新增代码时须保持这条边界。
package writecommon

import (
	"embed"

	kimpoi18n "github.com/kimposoft/kimpo-plugin-sdk/i18n"
)

//go:embed locales/*.json
var localeFS embed.FS

// tr 是插件内嵌翻译器（构造后只读，并发安全）。默认 locale=zh-CN，宿主未透传
// TriggerContext.locale 时用户看到中文文案。
var tr = kimpoi18n.MustNew(localeFS, "locales", "zh-CN")

// T 按两级查找链取三写动作共用文案：语言包[loc] → 语言包[默认 locale] → 空串。
// loc 取自宿主 HTTP 边界协商后透传的请求 locale：Execute 走 req.GetTriggerContext().GetLocale()，
// ValidateParams 走 req.GetLocale()（两条 RPC 同构）；
// 空值＝宿主未透传，自动回落默认语言。返回空串表示插件侧无翻译——调用方（biz.Fail 的
// messageKey 参数）应把 message_key 原样带回宿主，由宿主 HTTP 边界经中央库按请求 locale
// 兜底重译，不得用 key 顶文案位。
func T(loc, key string, params map[string]any) string {
	return tr.T(loc, key, params)
}

// DefaultLocale 返回本包翻译器构造时声明的默认 locale（归一后），供测试等场景取用。
func DefaultLocale() string {
	return tr.DefaultLocale()
}
