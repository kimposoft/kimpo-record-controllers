# kimpo-record-controllers

Kimpo 官方「数据动作」插件的源码。插件 ID：`com.kimposoft.recordcontrollers`。

它是一个动作类（biz 方向）插件，后端 sidecar 为 Kimpo 动作引擎提供五个数据动作，前端提供四个数据动作的配置面板：

| 动作 | 种类（operation_kind） | 说明 |
|---|---|---|
| `data.extract_record` | fetch | 取数：按来源、关联与筛选条件从其他数据表取数，回填到当前表单（主表字段或明细行） |
| `data.create_record` | create | 新增：按字段赋值规则在目标数据表新增记录 |
| `data.update_record` | update | 修改：按条件找到目标记录并改写字段 |
| `data.delete_record` | delete | 删除：按条件删除目标记录 |
| `data.sort_detail` | — | 明细排序：按一个或多个字段对当前单据某张明细表的行重新排序 |

- 取数的查询与预填求值由宿主在派发动作前完成；写类动作通过宿主数据语义服务 `AliceDataService/ApplyChangeSet` 整批提交，明细排序通过宿主表单数据层 `MirrorService/ReadTable`、`ReorderDetailRows` 完成。插件不直连数据库，也不持有业务数据。
- 前端配置面板（`action-config` surface）复用共享表达式编辑器 [`@kimposoft/expression-editor`](https://www.npmjs.com/package/@kimposoft/expression-editor)，同时绑定四个官方动作名和四个 `operation_kind:<kind>`，第三方同种类数据动作也可使用这套标准面板。
- 插件配置 `extraction_detail_write_chunk_size`（见 `config.schema.json`）：取数写明细时每批提交的字段写入条数上限，0 表示不分批。

## 依赖

- Go ≥ 1.23
- Node.js 18+ 与 npm
- Kimpo 插件 SDK：Go 模块 [`github.com/kimposoft/kimpo-plugin-sdk`](https://github.com/kimposoft/kimpo-plugin-sdk) `v1.1.0-tp.20`（见 `go.mod`；不使用 `replace` / `go.work`），前端包 [`@kimposoft/plugin-sdk`](https://www.npmjs.com/package/@kimposoft/plugin-sdk) `1.1.0-tp.20`
- 前端共享包：`@kimposoft/expression-editor` `1.1.0-tp.2`（打进 bundle）、`@kimposoft/ui-kit` `1.1.0-tp.2`（见下文「运行期依赖」）
- 打包与预检：`zip`，以及 SDK 自带的签名 / 预检工具 `kimpo-plugin-sign`

以上版本均为预览版（见 `go.mod` 与 `frontend/package.json`）。

## 运行期依赖（宿主提供）

前端 UMD 产物把下面三个依赖外部化，不打进 bundle，运行时由 Kimpo 宿主以全局变量注入：

| 包 | 全局变量 |
|---|---|
| `vue` | `Vue` |
| `naive-ui` | `naive` |
| `@kimposoft/ui-kit` | `KimpoUi` |

`frontend/package.json` 里把它们列为 devDependencies / peerDependencies，只用于本地类型检查、单测与构建；脱离 Kimpo 宿主单独加载 `plugin.umd.js` 不能运行。

## 从源码构建 .kpp

```sh
# 可选：先装好预检工具（不装时 build.sh 会用 go run 临时编译）
GOWORK=off go install github.com/kimposoft/kimpo-plugin-sdk/cmd/kimpo-plugin-sign@v1.1.0-tp.20

bash build.sh                          # 本机平台
GOOS=linux GOARCH=amd64 bash build.sh  # 实例跑在 Linux x86-64 服务器上时
```

`build.sh` 依次执行 `go vet` → `go test` → `go build`（产出 `plugin.bin`）→ 前端 `npm ci` → `npm test`（vitest）→ `npm run build`（vue-tsc 类型检查 + vite 构建单个 UMD 文件 `frontend/dist/plugin.umd.js`，样式内联）→ 组装 `.kpp` → `kimpo-plugin-sign lint`，产物在 `release/<插件ID>-<版本>-<GOOS>-<GOARCH>.kpp`。`plugin.bin` 必须与 Kimpo 实例运行的平台一致。只想跑后端单测：`make test`；前端开发在 `frontend/` 下用 `npm test`、`npm run typecheck`、`npm run build`。

包内结构：`manifest.json`、`plugin.bin`、`schema/config.schema.json`、`frontend/plugin.umd.js`、`frontend/locales/`（插件语言包）、`capability-knowledge/`（动作能力说明，供 Kimpo 内的 AI 助手检索）。

## 关于签名与插件 ID

- `com.kimposoft.*` / `com.kimpo.*` 是 Kimposoft 保留命名空间。**官方签名的安装包只由 Kimposoft 发布**，本仓库不含任何签名私钥。
- 用本仓库源码自行构建的包没有官方签名，宿主会按官方命名空间拦截安装；`kimpo-plugin-sign lint` 也会报 `reserved_plugin_namespace` 错误。
- 如需安装自己构建的版本，请把 `manifest.json` 的 `plugin_id`、`i18n.namespace` 和 `capability-knowledge/provider.json` 的 `provider` 改成你自己的插件 ID（如 `com.example.recordcontrollers`），以第三方插件身份安装；签名用你自己的密钥（`KIMPO_PLUGIN_SIGNING_KEY_FILE=<私钥文件> bash build.sh`），流程见 SDK 文档 `docs/third-party/`。
- 本插件申请的数据模型与写入 RPC 属于敏感级权限，第三方身份安装后需管理员在插件授权里补授。
- 只改插件 ID 还不够以第三方身份安装：`data.` 前缀的动作名是官方保留名（lint `reserved_action_name`），需要同时改 `capabilities.data_actions` 与 `internal/router` 里的动作名；前端配置面板用到的 `domain:alice-v4`、`domain:expression` 宿主能力只对官方插件开放（lint `host_cap_official_only`），按 `operation_kind:<kind>` 回落的绑定对第三方也不生效——第三方数据动作的配置面板由宿主的官方标准面板提供，因此第三方构建应去掉 manifest 里的 `entry.frontend` 与 `capabilities.frontend`，只装后端。改动后请相应调整 `manifest_test.go` 与前端测试里对官方 manifest 的断言。

## 许可证

[Apache License 2.0](LICENSE)，Copyright 2026 金宝量行（无锡）软件有限公司，附加声明见 [NOTICE](NOTICE)。Kimpo 名称与 logo 图形是金宝量行（无锡）软件有限公司的商标标识，Apache-2.0 不授予商标使用权。
