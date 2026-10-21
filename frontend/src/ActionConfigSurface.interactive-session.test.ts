/**
 * ActionConfigSurface interactive-session mount race + V4 收口相关断言（作用域信封 setScope /
 * 节点 ID setNodeId / host 缺失错误态）。
 *
 * 会话终态竞态覆盖 auth_ended/lost 两条终态与 model 兜底空文档。配置面挂载的编辑器是
 * 3.4 ExpressionEditor 本体（见 ActionConfigSurface.vue 文件头），故这里顶掉
 * 的是它的桩；它吃的是 ExpressionModel（由 V4 文档正向翻译而来），所以 model 断言看的是翻译产物
 * 的可观测特征（来源条数），而不是 V4 文档本身——「是否被作废清空」这件事在两侧同样可判。
 * 真组件不挂：它内部 useMessage()，需要外层 n-message-provider。
 * 另覆盖：挂载后 host.setNodeId 被调一次（真实 nodeId）、host.setScope 至少调一次且按 kind 正确
 * 分拣 sources/report_sources。
 */
import { defineComponent, h, nextTick } from 'vue';
import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import type { InteractiveSessionHandle, InteractiveSessionSnapshot } from '@kimposoft/plugin-sdk';
import { emptyExtractNodeDocument, type AliceV4Host, type V4ExtractNodeDocument } from '@kimposoft/expression-editor';
import ActionConfigSurface from './ActionConfigSurface.vue';

function snapshot(
  state: InteractiveSessionSnapshot['state'],
  patch: Partial<InteractiveSessionSnapshot> = {}
): InteractiveSessionSnapshot {
  return {
    mode: 'edit',
    state,
    lossReason: null,
    dirty: false,
    epoch: 1,
    expiresAt: null,
    serverTime: null,
    transportCredentialRevision: 1,
    conflict: null,
    canAdminRelease: false,
    canRequestEdit: false,
    ...patch
  };
}

function fakeSession(
  initial: InteractiveSessionSnapshot,
  betweenSetupAndMounted?: () => InteractiveSessionSnapshot
) {
  let current = initial;
  let listener: ((value: Readonly<InteractiveSessionSnapshot>) => void) | undefined;
  const handle: InteractiveSessionHandle = {
    get snapshot() {
      return current;
    },
    markDirty: vi.fn(),
    onChange(next) {
      listener = next;
      // 模拟 setup 已读取 active，但 mounted 注册过程中 Host 已进入终态且该次广播已经过去。
      if (betweenSetupAndMounted) current = betweenSetupAndMounted();
      return () => {
        listener = undefined;
      };
    },
    requestEdit: vi.fn(async () => undefined),
    adminRelease: vi.fn(async () => undefined),
    close: vi.fn(async () => undefined)
  };
  return {
    handle,
    publish(value: InteractiveSessionSnapshot) {
      current = value;
      listener?.(value);
    }
  };
}

/** 桩 AliceV4Host：五必选方法 + setScope/setNodeId 全 vi.fn。 */
function stubHost(): AliceV4Host {
  return {
    loadCatalog: vi.fn(async () => ({ renamed: false, sources: [], report: { tables: [] }, staging: { columns: [] }, functions: [], keywords: [] })),
    diagnose: vi.fn(async () => ({ ok: true, tokens: [], ast: null, refs: [], envelope: null, digest: '', diagnostics: [] })),
    tokenize: vi.fn(async () => ({ ok: true, source_text: '', refs: [], digest: '', diagnostics: [] })),
    restore: vi.fn(async () => ({ ok: true, text: '', digest: '', diagnostics: [] })),
    compile: vi.fn(async () => ({ ok: true, targets: [] })),
    setScope: vi.fn(),
    setNodeId: vi.fn()
  };
}

/** ExpressionEditor 桩：props 与真组件对齐（本文件只断言 modelValue/templateId/readonly 三项）。 */
const ExpressionEditorStub = defineComponent({
  props: ['modelValue', 'templateId', 'readonly', 'stagingSource'],
  emits: ['update:modelValue'],
  setup: () => () => h('div', { 'data-testid': 'expression-editor-stub' })
});

/** 编辑器桩当前持有的 ExpressionModel 里的来源条数（3.4 模型的来源在 source.tables 下）——
 *  作废（scrub）后为 0，未作废时与 V4 文档的 query.sources 等长。 */
function sourceCountOf(editor: { props: (k: string) => unknown }): number {
  return ((editor.props('modelValue') as { source?: { tables?: unknown[] } }).source?.tables ?? []).length;
}

function mountSurface(session: InteractiveSessionHandle, host: AliceV4Host = stubHost(), nodeId = 'node-real-1', modelOverride?: V4ExtractNodeDocument) {
  const originalModel: V4ExtractNodeDocument = modelOverride ?? {
    ...emptyExtractNodeDocument(),
    query: {
      ...emptyExtractNodeDocument().query,
      sources: [
        { kind: 'catalog', instance_id: 'inst-c1', alias: '客户表', app_id: 'app-1', template_id: 'tpl-1', data_table_id: 'dt-1' },
        { kind: 'report', instance_id: 'inst-r1', alias: '本单主表', table_id: 'tbl-main', table_role: 'main' }
      ]
    }
  };
  const onUpdateModel = vi.fn();
  const wrapper = mount(ActionConfigSurface, {
    props: {
      input: {
        surfaceType: 'action-config',
        hostContext: { templateId: 'template-1' },
        hostBridge: { interactiveSession: session, 'domain:alice-v4': host },
        props: {
          modelValue: originalModel,
          nodeId,
          onUpdateModel
        }
      }
    },
    global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
  });
  return { wrapper, originalModel, onUpdateModel, host };
}

describe('ActionConfigSurface 可读文本还原（暂存列）', () => {
  it('挂载时暂存列表达式也送 host.restore 还原可读文本（否则重开时显示 [标签]#{rN} 碎片）', async () => {
    const host = stubHost();
    const doc: V4ExtractNodeDocument = {
      ...emptyExtractNodeDocument(),
      staging_target: {
        columns: [
          {
            column_id: 'col_1a2b3c4d',
            name: '物料名称',
            expression: { expression_id: 'stg:col_1a2b3c4d', purpose: 'output', source_text: '[物料名称]#{r1}', refs: [] }
          }
        ]
      }
    };
    mountSurface(fakeSession(snapshot('active')).handle, host, 'node-real-1', doc);
    await nextTick();
    await Promise.resolve();
    expect(host.restore).toHaveBeenCalledWith(expect.objectContaining({ source_text: '[物料名称]#{r1}', purpose: 'output' }));
  });
});

describe('ActionConfigSurface interactive-session mount race', () => {
  it('re-reads auth_ended after subscribing, scrubs before rendering readonly, and rejects writes', async () => {
    const session = fakeSession(snapshot('active'), () => snapshot('auth_ended', { lossReason: 'auth-ended' }));
    const { wrapper, onUpdateModel } = mountSurface(session.handle);
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(true);
    expect(editor.props('templateId')).toBe('');
    expect(sourceCountOf(editor)).toBe(0); // 作废：兜底空文档，翻译产物无来源

    editor.vm.$emit('update:modelValue', editor.props('modelValue'));
    expect(onUpdateModel).not.toHaveBeenCalled();
  });

  it('does not scrub or freeze an active same-authority snapshot during the post-subscribe re-read', async () => {
    const refreshed = snapshot('active', {
      dirty: true,
      transportCredentialRevision: 2,
      serverTime: '2099-08-25T00:00:30Z'
    });
    const session = fakeSession(snapshot('active'), () => refreshed);
    const { wrapper, originalModel, onUpdateModel } = mountSurface(session.handle);
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(false);
    expect(editor.props('templateId')).toBe('template-1');
    expect(sourceCountOf(editor)).toBe(originalModel.query.sources.length);

    // 回写走反向翻译：编辑器发 ExpressionModel，surface 翻译回 V4 文档再交给宿主草稿。
    editor.vm.$emit('update:modelValue', editor.props('modelValue'));
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
    expect(onUpdateModel.mock.calls[0]![0]).toMatchObject({ schema_version: 'kimpo.extract-node/v4' });
  });

  it('freezes lost without scrubbing the existing model', async () => {
    const session = fakeSession(snapshot('active'));
    const { wrapper, originalModel, onUpdateModel } = mountSurface(session.handle);
    session.publish(snapshot('lost', { lossReason: 'offline' }));
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(true);
    expect(editor.props('templateId')).toBe('template-1');
    expect(sourceCountOf(editor)).toBe(originalModel.query.sources.length); // 冻结但不作废
    editor.vm.$emit('update:modelValue', editor.props('modelValue'));
    expect(onUpdateModel).not.toHaveBeenCalled();
  });
});

describe('ActionConfigSurface（V4 收口：setNodeId / setScope / host 缺失错误态）', () => {
  it('挂载后 host.setNodeId 被调一次，携带真实 nodeId', async () => {
    const session = fakeSession(snapshot('active'));
    const { host } = mountSurface(session.handle, stubHost(), 'node-abc-123');
    await nextTick();

    expect(host.setNodeId).toHaveBeenCalledWith('node-abc-123');
  });

  it('挂载后 host.setScope 至少调一次，且按 kind 把 catalog/report 两类来源分拣进 sources/report_sources', async () => {
    const session = fakeSession(snapshot('active'));
    const { host } = mountSurface(session.handle);
    await nextTick();

    expect(host.setScope).toHaveBeenCalled();
    const lastCall = (host.setScope as ReturnType<typeof vi.fn>).mock.calls.at(-1)![0];
    expect(lastCall).toEqual({
      sources: [{ instance_id: 'inst-c1', alias: '客户表', app_id: 'app-1', template_id: 'tpl-1', data_table_id: 'dt-1' }],
      report_sources: [{ instance_id: 'inst-r1', table_id: 'tbl-main', table_role: 'main', alias: '本单主表' }]
    });
  });

  // 文档含 kind='staging' 绑定 + props 传了 stagingSource 时，
  // hostScopeFromDocument 第三参数须被喂上，setScope 收到的信封才带 staging_source（否则
  // diagnose/tokenize/restore 三端点解析不到「暂存表.[列]」，报「当前作用域没有暂存区」）。
  it('文档含暂存绑定 + props 传了 stagingSource：host.setScope 收到 staging_source（列 column_id/label 正确）', async () => {
    const session = fakeSession(snapshot('active'));
    const doc: V4ExtractNodeDocument = {
      ...emptyExtractNodeDocument(),
      query: {
        ...emptyExtractNodeDocument().query,
        sources: [
          {
            kind: 'staging',
            instance_id: 'stg-node-p1-a1b2c3',
            alias: '上一步暂存区',
            producer_node_id: 'node-p1',
            output_schema_revision: 'v1'
          }
        ]
      }
    };
    const host = stubHost();
    mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: 'template-1' },
          hostBridge: { interactiveSession: session.handle, 'domain:alice-v4': host },
          props: {
            modelValue: doc,
            nodeId: 'node-real-1',
            onUpdateModel: vi.fn(),
            stagingSource: {
              producerNodeId: 'node-p1',
              producerLabel: '上一步暂存区（提取数据）',
              columns: [{ code: 'col-a', name: '物料名称', type: 'text' }]
            }
          }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    expect(host.setScope).toHaveBeenCalled();
    const lastCall = (host.setScope as ReturnType<typeof vi.fn>).mock.calls.at(-1)![0];
    expect(lastCall.staging_source).toEqual({
      instance_id: 'stg-node-p1-a1b2c3',
      producer_node_id: 'node-p1',
      alias: '上一步暂存区',
      columns: [{ column_id: 'col-a', label: '物料名称', data_type: 'text', nullable: true }]
    });
  });

  it('文档含暂存绑定但 props 未传 stagingSource：host.setScope 收到的信封不带 staging_source', async () => {
    const session = fakeSession(snapshot('active'));
    const doc: V4ExtractNodeDocument = {
      ...emptyExtractNodeDocument(),
      query: {
        ...emptyExtractNodeDocument().query,
        sources: [
          {
            kind: 'staging',
            instance_id: 'stg-node-p1-a1b2c3',
            alias: '上一步暂存区',
            producer_node_id: 'node-p1',
            output_schema_revision: 'v1'
          }
        ]
      }
    };
    const host = stubHost();
    mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: 'template-1' },
          hostBridge: { interactiveSession: session.handle, 'domain:alice-v4': host },
          props: { modelValue: doc, nodeId: 'node-real-1', onUpdateModel: vi.fn() }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    expect(host.setScope).toHaveBeenCalled();
    const lastCall = (host.setScope as ReturnType<typeof vi.fn>).mock.calls.at(-1)![0];
    expect(lastCall.staging_source).toBeUndefined();
  });

  it("host 缺失（hostBridge 无 'domain:alice-v4'）：渲染错误态，不抛异常", async () => {
    const session = fakeSession(snapshot('active'));
    const wrapper = mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: 'template-1' },
          hostBridge: { interactiveSession: session.handle },
          props: { modelValue: emptyExtractNodeDocument(), nodeId: 'node-1', onUpdateModel: vi.fn() }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    expect(wrapper.find('[data-testid="extraction-v4-host-missing"]').exists()).toBe(true);
    expect(wrapper.findComponent(ExpressionEditorStub).exists()).toBe(false);
  });
});

describe('ActionConfigSurface 无模板（定时任务/任务级动作设计器）', () => {
  // 场景：定时任务动作设计器（hostContext.templateId 为空串）新建「新增数据」节点。宿主
  // （kimpo-action-designer）对任务级场景从不打开 template_design 编辑租约，snapshot.mode 恒非
  // 'edit'（这里用 'view' 模拟最坏情形）；若一律要求 mode!=='view' 才可编辑，任务级场景会被恒判只读，
  // 并显示共享包 ExpressionEditor 对 readonly=true 的固定「权限不足」横幅（与真实跨应用权限无关）。
  it('无 templateId + 会话 mode=view（宿主未开模板编辑租约）+ state=active：可编辑（不触发只读横幅）', async () => {
    const session = fakeSession(snapshot('active', { mode: 'view' }));
    const wrapper = mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: '' }, // 定时任务/任务级动作设计器：无模板上下文。
          hostBridge: { interactiveSession: session.handle, 'domain:alice-v4': stubHost() },
          props: { modelValue: emptyExtractNodeDocument(), nodeId: 'node-1', onUpdateModel: vi.fn() }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(false);
  });

  it('有 templateId（模板设计态）+ 会话 mode=view：仍判只读（模板设计态口径不变）', async () => {
    const session = fakeSession(snapshot('active', { mode: 'view' }));
    const wrapper = mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: 'template-1' },
          hostBridge: { interactiveSession: session.handle, 'domain:alice-v4': stubHost() },
          props: { modelValue: emptyExtractNodeDocument(), nodeId: 'node-1', onUpdateModel: vi.fn() }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(true);
  });

  it('无 templateId + state≠active（如 opening）：仍判只读——无模板场景只放开 mode 判据，不放开 state 判据', async () => {
    const session = fakeSession(snapshot('opening', { mode: 'view' }));
    const wrapper = mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          hostContext: { templateId: '' },
          hostBridge: { interactiveSession: session.handle, 'domain:alice-v4': stubHost() },
          props: { modelValue: emptyExtractNodeDocument(), nodeId: 'node-1', onUpdateModel: vi.fn() }
        }
      },
      global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
    });
    await nextTick();

    const editor = wrapper.findComponent(ExpressionEditorStub);
    expect(editor.props('readonly')).toBe(true);
  });
});
