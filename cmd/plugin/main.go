// Record Controllers Plugin —— 数据动作聚合插件（extract/create/update/delete 四合一）。
// 单进程贡献四个数据类动作：取数 data.extract_record / 新增 data.create_record / 修改
// data.update_record / 删除 data.delete_record。
//
// 四个子包执行态均经 plugin.HostFrom(ctx) 取宿主反向通道：三项写动作只经宿主数据语义服务
// （host.Alice()）的 mutation ticket Apply，extract 的 V4 取数由宿主动作网关代办、插件 Execute 只认「已处理」回执
// （见 internal/actions/extract/service.go）。
// host 由 SDK 按每次调用的 ctx 注入，无需额外接线。
package main

import (
	"encoding/json"
	"log"

	"github.com/kimposoft/kimpo-plugin-sdk/direction/biz"
	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
	recordcontrollers "github.com/kimposoft/kimpo-record-controllers"
	"github.com/kimposoft/kimpo-record-controllers/internal/router"
)

func main() {
	opts := plugin.ParseFlags()
	// 插件 manifest 取自根包内嵌的根 manifest.json（单文件权威，见 ../../manifest.go）。经 Options.Manifest
	// 传给 Serve，启用 SDK 方向一致性自检（断言实际注册方向 ⊆ manifest.capabilities.directions）。
	opts.Manifest = recordcontrollers.ManifestJSON()
	svc := router.New()
	// extraction_detail_write_chunk_size 是取数动作的实例级配置项（config.schema.json），
	// 由本进程读取实例配置并下发给 extract 子服务。
	if raw, err := opts.LoadConfig(); err != nil {
		log.Fatalf("[record-controllers] load config: %v", err)
	} else if len(raw) > 0 {
		var cfg struct {
			DetailWriteChunkSize int `json:"extraction_detail_write_chunk_size"`
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			log.Fatalf("[record-controllers] parse config: %v", err)
		}
		svc.SetExtractionDetailWriteChunkSize(cfg.DetailWriteChunkSize)
	}
	if err := plugin.Serve(opts, biz.Register(svc)); err != nil {
		log.Fatalf("[record-controllers] %v", err)
	}
}
