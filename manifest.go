// Package recordcontrollers 暴露插件入口与测试共用的只读构建元数据。
//
// 根 manifest.json 是唯一权威：本包直接 //go:embed 根目录的 manifest.json（go:embed 不能跨目录引用 ..，
// 所以 embed 声明放在与 manifest.json 同级的根包里，cmd/plugin 再 import 本包）。这样第三方或任何人
// 直接 `go build ./cmd/plugin` 即可构建，无需在构建期把 manifest.json 拷进 cmd/plugin。
// 与 kimpo-form-controllers 的 manifest.go 同一形态。
package recordcontrollers

import (
	"bytes"
	_ "embed"
)

// rootManifestJSON 是编进 Record Controllers 二进制的唯一一份 manifest。
//
//go:embed manifest.json
var rootManifestJSON []byte

// ManifestJSON 返回根 manifest.json 字节的副本（调用方改写不影响内嵌原件）。
func ManifestJSON() []byte {
	return bytes.Clone(rootManifestJSON)
}
