/**
 * extraction 前端 UMD 入口。
 *
 * 宿主通过 window.KimpoPlugins.extraction.createSurface(input) 挂载 surface。
 * 支持 action-config 四个 bind：取数 data.extract_record + 三写动作 data.create_record/
 * data.update_record/data.delete_record，四者共用同一份 ActionConfigSurface 组件——组件内部按种类
 * 派生 writeOperation 分流（见该文件头注释「写模式」段）。
 *
 * manifest 另有四个 `bind: "operation_kind:<kind>"` surface，官方标准面板为所有同种类数据动作（含第三方）
 * 服务。挂载判据是「能折算出种类」（见 actionKind.ts）；折算不出种类的 bind 具名拒绝。
 */
import ActionConfigSurface from './ActionConfigSurface.vue';
import { resolveDataActionKind } from './actionKind';
import {
  definePluginSurface,
  type InteractiveSessionHandle,
  type SurfaceCreateInput
} from '@kimposoft/plugin-sdk';

export const createSurface = definePluginSurface((input: SurfaceCreateInput) => {
  if (input.surfaceType !== 'action-config') {
    throw new Error(`[extraction] unsupported surface: ${input.surfaceType}`);
  }
  // 种类判定：官方名 / operation_kind:<kind> / 宿主下发的 operationKind，三者都折算不出 ⇒ 不是本面板能服务的动作。
  const hostContext = (input.hostContext ?? {}) as Record<string, unknown>;
  if (!resolveDataActionKind({ bind: input.bind, hostContext })) {
    throw new Error(`[extraction] unsupported action-config bind: ${input.bind}`);
  }
  const hostBridge = input.hostBridge as
    | (Record<string, unknown> & { interactiveSession?: InteractiveSessionHandle })
    | undefined;
  if (!hostBridge?.interactiveSession) {
    throw new Error('[extraction] 宿主未注入 platform.interactiveSession');
  }
  // 返回组件类 + 初始 props（宿主仍以响应式 :input 覆盖，此处 props 仅备用）。所有 bind 共用
  // 同一个组件类，组件内部按种类判定取数/写模式（见 ActionConfigSurface.vue）。
  return { component: ActionConfigSurface, props: { input } };
});
