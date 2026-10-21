/**
 * 记录控制器插件（extraction bundle）前端 i18n 出口。
 *
 * 本 surface 在 host_caps 里声明了 'i18n'（供宿主装配桥键）；写模式（新增/修改/删除数据配置面）有若干
 * 本插件独有的运行期用户可见文案（客户端保存前校验提示、静态提醒等，见 ActionConfigSurface.vue 的
 * staticNotices/finalizeWriteV4），统一经本文件走 SDK 标准四级查找链（createPluginI18n：① 插件自带包
 * [当前 locale] → ② 宿主中央包 → ③ 插件自带包[默认 locale] → ④ key 原样），不散落硬编码字面量。
 *
 * 语言覆盖：manifest.json 的 `i18n.locales` 为 `["zh-CN", "zh-TW", "en-US", "ja-JP"]`。插件前端与
 * 后端（三写动作 Go 侧 writecommon 语言包）的 i18n 各自独立维护，本文件装的是前端四语言消息表。
 */
import { createPluginI18n, type HostI18n, type PluginI18n } from '@kimposoft/plugin-sdk';
import enUS from '../locales/en-US.json';
import zhCN from '../locales/zh-CN.json';
import zhTW from '../locales/zh-TW.json';
import jaJP from '../locales/ja-JP.json';

/** 供 ActionConfigSurface.vue 挂载时按当前 hostBridge.i18n（可能 undefined）构造一份本插件的
 *  四级查找器；host 缺席（老宿主/单测）时自动降级为①③④纯本地链（无宿主中央包兜底）。 */
export function createSurfaceI18n(host?: HostI18n): PluginI18n {
  return createPluginI18n({
    host,
    namespace: 'com.kimposoft.recordcontrollers',
    defaultLocale: 'zh-CN',
    messages: { 'zh-CN': zhCN, 'en-US': enUS, 'zh-TW': zhTW, 'ja-JP': jaJP }
  });
}
