// 本 surface 消费的宿主桥键，必须在 manifest.json 的 host_caps 里逐个声明。
//
// 为什么需要这条守卫：宿主前端装配 hostBridge 时会按 manifest 声明的 host_caps 裁剪，
// **未声明的键被静默滤掉
// ——不报错、不告警，桥上就是没有那个属性**。ActionConfigSurface 这边又都写成可选消费
// （`if (!i18nHost) return undefined` 之类的回落），于是漏声明的后果不是崩溃，是功能悄悄不生效。
//
// 这正是 'i18n' 出过的事故：manifest 从未声明它，`input.hostBridge.i18n` 长期恒为 undefined，
// 当时的 labels/chartLabels 两套逐键取值全程走组件内置中文默认文案，谁也没发现——直到有人去读
// 桥裁剪的实现才反查出来。补 'i18n' 的同时留下这条测试，让下一次"新增桥键消费却忘了改 manifest"
// 在跑测试时就红，而不是等到线上文案不生效。
//
// 本 surface 同时服务四个 action-config bind（取数 + 三个写动作，见
// ActionConfigSurface.vue/index.ts 头注释），四条 surface 声明各自独立、必须逐一校验——
// 漏给某个写动作补全 host_caps 的后果与当年漏声明 'i18n' 同款（静默裁剪，功能悄悄不生效）。
//
// 维护方式：本文件新增一个 hostBridge.<key> 的消费点时，把该键加进 CONSUMED_HOST_BRIDGE_KEYS。
import { describe, expect, it } from 'vitest';

import manifest from '../../manifest.json';

/**
 * ActionConfigSurface.vue 实际读取的 hostBridge 键（取数/写两种模式共用同一组件，消费键集合
 * 相同——写模式的 st()/createSurfaceI18n 消费的正是 'i18n'）。
 *
 * - platform.interactiveSession：编辑租约与会话快照（`interactiveSession`，宿主侧刻意保留平台级
 *   能力名不做映射，故此处按协议名断言）
 * - domain:alice-v4：V4 表达式宿主（catalog/tokenize/restore/compile/fetchChartCatalog）
 * - domain:expression：3.4 表达式宿主（本 surface 仍在用的正向翻译通道）
 * - i18n：写模式新增运行期文案的取值来源（./i18n.ts createSurfaceI18n，见 ActionConfigSurface.vue
 *   的 st()/staticNotices 用法）
 */
const CONSUMED_HOST_BRIDGE_KEYS = [
  'platform.interactiveSession',
  'domain:alice-v4',
  'domain:expression',
  'i18n'
] as const;

/** 四个 action-config bind：取数 + 三写动作，逐条断言 host_caps。 */
const ACTION_CONFIG_BINDS = ['data.extract_record', 'data.create_record', 'data.update_record', 'data.delete_record'] as const;

/** 取指定 bind 的 action-config surface 的 host_caps 声明。 */
function actionConfigSurfaceHostCaps(bind: string): string[] {
  const surfaces = manifest.capabilities?.frontend?.surfaces ?? [];
  const surface = surfaces.find(s => s.bind === bind);
  expect(surface, `manifest 里找不到 bind=${bind} 的 surface`).toBeTruthy();
  return surface!.host_caps ?? [];
}

describe.each(ACTION_CONFIG_BINDS)('ActionConfigSurface host_caps 声明（bind=%s）', bind => {
  it.each(CONSUMED_HOST_BRIDGE_KEYS)('声明了消费的桥键 %s', key => {
    expect(actionConfigSurfaceHostCaps(bind)).toContain(key);
  });

  it('未声明的键会被宿主静默裁掉，所以声明只能多不能少', () => {
    // 反向说明：这里不断言 host_caps 恰好等于消费清单——manifest 可以合法地多声明几项
    // （如 request/message/log 由 SDK 基座而非本组件直接消费）。只保证消费清单是它的子集。
    const declared = actionConfigSurfaceHostCaps(bind);
    const missing = CONSUMED_HOST_BRIDGE_KEYS.filter(k => !declared.includes(k));
    expect(missing, `manifest host_caps 缺少这些已被消费的键：${missing.join(', ')}`).toEqual([]);
  });
});
