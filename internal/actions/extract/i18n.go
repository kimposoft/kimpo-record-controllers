// i18n.go — 插件后端多语言（两级查找方案，见 SDK 多语言文档）。
// 语言文件随二进制 go:embed 打包（无需进 .kpp）；查找链：
// 透传 locale → 默认 zh-CN → 空串（空串时宿主按 message_key 经中央库兜底重译）。
package extract

import (
	"embed"

	kimpoi18n "github.com/kimposoft/kimpo-plugin-sdk/i18n"
)

//go:embed locales/*.json
var localeFS embed.FS

// tr 是插件内嵌翻译器（构造后只读，并发安全）。默认 locale=zh-CN。
var tr = kimpoi18n.MustNew(localeFS, "locales", "zh-CN")

// trDefault 用插件默认 locale 取文案：供 ValidateParams 等**无 locale 透传**的 RPC 使用
// （该 RPC 的请求线上无 TriggerContext.locale，属已知限制——设计态校验消息暂按默认语言返回）。
func trDefault(key string, params map[string]any) string {
	return tr.T(tr.DefaultLocale(), key, params)
}
