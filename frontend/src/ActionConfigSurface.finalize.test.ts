/**
 * ActionConfigSurface.finalize()/test() 用例。
 *
 * 背景：配置面挂载的是 3.4 ExpressionEditor 本体（见 ActionConfigSurface.vue 文件头）。finalize 走
 * duck-typing 约定：挂载点经 ref + defineExpose 暴露；**收口实现在本 surface 自己**：3.4 编辑器只把
 * 用户改动写进模型（表达式以可读文本承载），把可读文本 tokenize 成 token 化源码 + 稳定引用绑定表是
 * V4 链路自己的事，故 finalize() 不转调任何内部编辑器，而是逐条调 host.tokenize。
 *
 * 本文件不 mount 真实 ExpressionEditor（它内部 useMessage()，需要外层 n-message-provider，且依赖
 * 字段树/函数条等重组件），改用同名 stub 顶掉，覆盖：可编辑态逐条 tokenize 并回写文档；不可编辑态
 * 直接返回 undefined 且不下发调用；宿主缺 tokenize 时的具名失败；test() 收口失败/成功两条分支。
 */
import { defineComponent, h } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import type { InteractiveSessionHandle, InteractiveSessionSnapshot } from '@kimposoft/plugin-sdk';
import {
  emptyExtractNodeDocument,
  type AliceV4Host,
  type V4ExpressionRefWire,
  type V4ExtractNodeDocument,
  type V4NodeDocumentFinalizeResult,
  type V4TokenizeRequest
} from '@kimposoft/expression-editor';
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

/** 冻结的固定 active 会话：不订阅变更（本用例不测终态竞态，专测 finalize/test 收口）。 */
function activeSession(): InteractiveSessionHandle {
  return Object.freeze({
    snapshot: snapshot('active'),
    markDirty: vi.fn(),
    onChange: vi.fn(() => () => undefined),
    requestEdit: vi.fn(async () => undefined),
    adminRelease: vi.fn(async () => undefined),
    close: vi.fn(async () => undefined)
  });
}

/** 待收口的文档：一条非空筛选表达式（空表达式不下发 tokenize，见 finalizeToV4）。 */
function docWithFilter(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.query.filter = { expression_id: 'filter', purpose: 'filter', source_text: '[订单].[金额] > 100', refs: [] };
  return doc;
}

/** 节点重开、用户未编辑任何表达式时的存档态文档——source_text 已是 token 化源码
 *  `[金额]#{r1}`、refs 非空（还原结果只落进 readableTexts 显示缓存，不回写这份 model.value）。
 *  一条输出表达式版本；docWithArchivedFilter() 是同形态的筛选表达式版本。 */
function docWithArchivedOutput(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.targets = [
    {
      target_id: 'tgt-x',
      target_data_table_id: 'tbl-x',
      table_role: 'main',
      outputs: [
        {
          target_field_id: 'fld-a',
          expression: {
            expression_id: 'out:tbl-x:fld-a',
            purpose: 'output',
            source_text: '[金额]#{r1}',
            refs: [{ ref_id: 'r1', kind: 'source_field', source_instance_id: 't1', field_id: 'f1', last_label: '金额' }]
          },
          fill_strategy: 'overwrite'
        }
      ],
      clear: { mode: 'none', timing: 'before' }
    }
  ] as unknown as V4ExtractNodeDocument['targets'];
  return doc;
}

/** 暂存列待收口的文档：staging_target.columns 里一条待 tokenize 的表达式——finalizeToV4 的
 *  tokenize 遍历除 query.filter 与 targets[].outputs[] 外还须覆盖暂存列（漏掉时其 refs 表恒为空），
 *  本用例守住这一分支。 */
function docWithStagingColumn(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.staging_target = {
    columns: [
      {
        column_id: 'col_1a2b3c4d',
        name: '合计金额',
        expression: { expression_id: 'stg:col_1a2b3c4d', purpose: 'output', source_text: '[订单].[金额] + 1', refs: [] }
      }
    ]
  };
  return doc;
}

/** 暂存列列名为空（trim 后）、表达式已配置的待收口文档（规则：保存时列名为空则不允许保存）：
 *  与「表达式为空的列被剔除」互斥——本文档表达式非空，走列名校验分支。 */
function docWithStagingEmptyName(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.staging_target = {
    columns: [
      { column_id: 'col_a1', name: '   ', expression: { expression_id: 'stg:col_a1', purpose: 'output', source_text: "'X'", refs: [] } }
    ]
  };
  return doc;
}

/** 暂存列列名与表达式均为空：剔除优先于空列名诊断——列被剔除后，走到列名
 *  校验时它已经不在数组里，不应再报「第 1 列列名为空」。 */
function docWithStagingEmptyNameAndExpression(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.staging_target = {
    columns: [
      { column_id: 'col_b1', name: '', expression: { expression_id: 'stg:col_b1', purpose: 'output', source_text: '', refs: [] } }
    ]
  };
  return doc;
}

/** 两个暂存列列名（trim 后）相同：服务端保存闸同样拒绝，但服务端错误指不出是哪一列——
 *  本用例覆盖客户端先给出的 STAGING_COLUMN_NAME_DUPLICATE 诊断。 */
function docWithStagingDuplicateNames(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.staging_target = {
    columns: [
      { column_id: 'col_c1', name: '金额', expression: { expression_id: 'stg:col_c1', purpose: 'output', source_text: "'X'", refs: [] } },
      { column_id: 'col_c2', name: '金额', expression: { expression_id: 'stg:col_c2', purpose: 'output', source_text: "'Y'", refs: [] } }
    ]
  };
  return doc;
}

/** 同上，筛选表达式（purpose=filter）版本。 */
function docWithArchivedFilter(): V4ExtractNodeDocument {
  const doc = emptyExtractNodeDocument();
  doc.query.filter = {
    expression_id: 'filter',
    purpose: 'filter',
    source_text: '[金额]#{r1} > 100',
    refs: [{ ref_id: 'r1', kind: 'source_field', source_instance_id: 't1', field_id: 'f1', last_label: '金额' }]
  };
  return doc;
}

/** 桩 AliceV4Host：五必选方法 + setScope/setNodeId 全 vi.fn，供 provideAliceV4Host 消费。 */
function stubHost(overrides: Partial<AliceV4Host> = {}): AliceV4Host {
  return {
    loadCatalog: vi.fn(async () => ({ renamed: false, sources: [], report: { tables: [] }, staging: { columns: [] }, functions: [], keywords: [] })),
    diagnose: vi.fn(async () => ({ ok: true, tokens: [], ast: null, refs: [], envelope: null, digest: '', diagnostics: [] })),
    tokenize: vi.fn(async () => ({ ok: true, source_text: '', refs: [], digest: '', diagnostics: [] })),
    restore: vi.fn(async () => ({ ok: true, text: '', digest: '', diagnostics: [] })),
    compile: vi.fn(async () => ({ ok: true, targets: [] })),
    setScope: vi.fn(),
    setNodeId: vi.fn(),
    ...overrides
  };
}

/** ExpressionEditor 桩：顶掉真组件（useMessage 需外层 provider），只占位不渲染任何内容。 */
const ExpressionEditorStub = defineComponent({
  // notices：「test() 成功文案按模式分叉」用例需要读取 sessionNotices 回传的文案
  // （同 ActionConfigSurface.write.test.ts 的 WriteExpressionEditorStub）。
  // sourcePickerOptions：「①来源段放开视图+跨应用」用例需要读取。
  props: ['modelValue', 'templateId', 'readonly', 'stagingSource', 'notices', 'sourcePickerOptions'],
  emits: ['update:modelValue'],
  setup: () => () => h('div', { 'data-testid': 'expression-editor-stub' })
});

/** 收口剔除判据用文档：一条已配好的输出 + 一条编辑期占位输出（source_text 为空）。 */
function docWithPlaceholderOutput(): V4ExtractNodeDocument {
  const doc = docWithFilter();
  doc.targets = [
    {
      target_id: 'tgt-x',
      target_data_table_id: 'tbl-x',
      table_role: 'main',
      outputs: [
        { target_field_id: 'fld-a', expression: { expression_id: 'out:tbl-x:fld-a', purpose: 'output', source_text: "'W4'", refs: [] }, fill_strategy: 'overwrite' },
        { target_field_id: 'fld-b', expression: { expression_id: 'out:tbl-x:fld-b', purpose: 'output', source_text: '', refs: [] }, fill_strategy: 'overwrite' }
      ],
      clear: { mode: 'none', timing: 'before' }
    }
  ] as unknown as V4ExtractNodeDocument['targets'];
  return doc;
}

function mountSurface(opts: { readonly?: boolean; host?: AliceV4Host; omitHost?: boolean; doc?: V4ExtractNodeDocument } = {}) {
  const session = activeSession();
  const onUpdateModel = vi.fn();
  const host = opts.omitHost ? undefined : (opts.host ?? stubHost());
  const wrapper = mount(ActionConfigSurface, {
    props: {
      input: {
        surfaceType: 'action-config',
        hostContext: { templateId: 'template-1' },
        hostBridge: { interactiveSession: session, ...(host ? { 'domain:alice-v4': host } : {}) },
        props: {
          modelValue: opts.doc ?? docWithFilter(),
          readonly: opts.readonly === true,
          nodeId: 'node-1',
          onUpdateModel
        }
      }
    },
    global: { stubs: { ExpressionEditor: ExpressionEditorStub } }
  });
  return { wrapper, host, onUpdateModel };
}

/** 取 defineExpose 出来的 finalize/test（duck-typing 约定，壳侧同款取法）。 */
function exposed(wrapper: ReturnType<typeof mountSurface>['wrapper']) {
  return wrapper.vm as unknown as {
    finalize: () => Promise<V4NodeDocumentFinalizeResult | undefined>;
    test: () => Promise<{ ok: boolean; diagnostics: string[] }>;
  };
}

describe('ActionConfigSurface.finalize()（收口在 surface 自身）', () => {
  it('可编辑态：逐条 tokenize 非空表达式，把 token 化结果写回文档并回写宿主草稿', async () => {
    // 请求体单独记一份：vi.fn 的无参箭头推不出 mock.calls 的元组元素类型（TS2493）。
    const seen: V4TokenizeRequest[] = [];
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => {
      seen.push(req);
      return {
        ok: true,
        source_text: '[订单].[金额]#{r1} > 100',
        refs: [{ ref_id: 'r1', kind: 'source_field' as const, source_instance_id: 'o', field_id: 'f1', last_label: '金额' }],
        digest: 'd1',
        diagnostics: []
      };
    });
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }) });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(tokenize).toHaveBeenCalledTimes(1);
    expect(seen[0]).toMatchObject({ text: '[订单].[金额] > 100', purpose: 'filter' });
    expect(result?.ok).toBe(true);
    expect(result?.doc.query.filter?.source_text).toBe('[订单].[金额]#{r1} > 100');
    // 收口成功才回写：失败时不用半绑定的文档覆盖用户输入（见 finalizeToV4）。
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('存档态（重开未编辑）输出表达式：tokenize 原样带上文档里的 refs，不当成可读文本裸传', async () => {
    // 场景：节点刚重开、用户未改动任何一条表达式时，model.value 仍是
    // refreshReadableTexts 还原前的存档态——source_text 是 token 化源码、refs 非空绑定表
    // （还原结果只落进 readableTexts 显示缓存，不回写 model）。此时直接保存，finalizeToV4 若把
    // refs 丢在请求外，服务端按「有稳定标记则完全依赖请求里的 refs」找不到
    // 绑定，报「未提供稳定引用 #{r1} 的绑定」。
    const doc = docWithArchivedOutput();
    const archivedRefs = doc.targets[0].outputs[0].expression.refs;
    const seen: V4TokenizeRequest[] = [];
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => {
      seen.push(req);
      // 服务端 tokenize：已是 #{rN} 形态的出现按 refs 反查、保留原 ID，幂等；last_label 借此刷新。
      return {
        ok: true,
        source_text: '[金额]#{r1}',
        refs: [{ ref_id: 'r1', kind: 'source_field' as const, source_instance_id: 't1', field_id: 'f1', last_label: '金额（改名后）' }],
        digest: 'd2',
        diagnostics: []
      };
    });
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    // tokenize 被调用时：text 为原 source_text（未被误当可读文本裸传），refs 深等于文档里的 refs。
    expect(seen[0]).toMatchObject({ text: '[金额]#{r1}', purpose: 'output' });
    expect(seen[0].refs).toEqual(archivedRefs);
    expect(result?.ok).toBe(true);
    // stub 返回 ok 后文档 source_text/refs 被响应覆盖（last_label 刷新）。
    expect(result?.doc.targets[0].outputs[0].expression.source_text).toBe('[金额]#{r1}');
    expect(result?.doc.targets[0].outputs[0].expression.refs).toEqual([
      { ref_id: 'r1', kind: 'source_field', source_instance_id: 't1', field_id: 'f1', last_label: '金额（改名后）' }
    ]);
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('存档态的筛选表达式（purpose=filter）同形态：tokenize 同样原样带上 refs', async () => {
    const doc = docWithArchivedFilter();
    const archivedRefs = doc.query.filter!.refs;
    const seen: V4TokenizeRequest[] = [];
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => {
      seen.push(req);
      return { ok: true, source_text: req.text, refs: req.refs ?? [], digest: 'd3', diagnostics: [] };
    });
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(seen[0]).toMatchObject({ text: '[金额]#{r1} > 100', purpose: 'filter' });
    expect(seen[0].refs).toEqual(archivedRefs);
    expect(result?.ok).toBe(true);
    expect(result?.doc.query.filter?.source_text).toBe('[金额]#{r1} > 100');
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('暂存列表达式（staging_target.columns[].expression）参与 tokenize', async () => {
    const seen: V4TokenizeRequest[] = [];
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => {
      seen.push(req);
      return {
        ok: true,
        source_text: '[订单].[金额]#{r1} + 1',
        refs: [{ ref_id: 'r1', kind: 'source_field' as const, source_instance_id: 'o', field_id: 'amount', last_label: '金额' }],
        digest: 'd4',
        diagnostics: []
      };
    });
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc: docWithStagingColumn() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(tokenize).toHaveBeenCalledTimes(1);
    expect(seen[0]).toMatchObject({ text: '[订单].[金额] + 1', purpose: 'output' });
    expect(result?.ok).toBe(true);
    // tokenize 回带的 token 化源码 + refs 写回该暂存列表达式。
    expect(result?.doc.staging_target?.columns[0]?.expression.source_text).toBe('[订单].[金额]#{r1} + 1');
    expect(result?.doc.staging_target?.columns[0]?.expression.refs).toEqual([
      { ref_id: 'r1', kind: 'source_field', source_instance_id: 'o', field_id: 'amount', last_label: '金额' }
    ]);
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('暂存列列名为空：finalize ok=false，diagnostics 含 STAGING_COLUMN_NAME_EMPTY，不回写草稿', async () => {
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => ({ ok: true, source_text: req.text, refs: [], digest: 'd', diagnostics: [] }));
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc: docWithStagingEmptyName() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect(result?.diagnostics.some(d => 'diagnostic' in d && d.diagnostic.code === 'STAGING_COLUMN_NAME_EMPTY')).toBe(true);
    // 收口失败：不用半绑定的文档覆盖用户输入（与既有失败路径同一口径）。
    expect(onUpdateModel).not.toHaveBeenCalled();
  });

  it('暂存列表达式为空：该列被剔除、不报空列名错（剔除优先——即使列名也是空的）', async () => {
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => ({ ok: true, source_text: req.text, refs: [], digest: 'd', diagnostics: [] }));
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc: docWithStagingEmptyNameAndExpression() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.doc.staging_target?.columns).toEqual([]); // 列已被 stripUnconfiguredOutputs 剔除
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('两个暂存列同名：STAGING_COLUMN_NAME_DUPLICATE，不回写草稿', async () => {
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => ({ ok: true, source_text: req.text, refs: [], digest: 'd', diagnostics: [] }));
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc: docWithStagingDuplicateNames() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect(result?.diagnostics.some(d => 'diagnostic' in d && d.diagnostic.code === 'STAGING_COLUMN_NAME_DUPLICATE')).toBe(true);
    expect(onUpdateModel).not.toHaveBeenCalled();
  });

  it('占位输出行（source_text 为空）在收口时剔除：不下发 tokenize，收口结果与回写草稿都不含它', async () => {
    // ⊞ 选空值后 ✕ 清除会留下 source_text:'' 的占位行；若落进草稿，服务端解码闸会整份拒绝
    // （「source_text 不能为空」）。收口是送服务端的边界，必须在这里剔掉。
    const seen: V4TokenizeRequest[] = [];
    const tokenize = vi.fn(async (req: V4TokenizeRequest) => {
      seen.push(req);
      return { ok: true, source_text: req.text, refs: [], digest: 'd', diagnostics: [] };
    });
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize }), doc: docWithPlaceholderOutput() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(seen.map(r => r.text)).toEqual(['[订单].[金额] > 100', "'W4'"]);
    expect(result?.ok).toBe(true);
    expect(result?.doc.targets[0].outputs.map(o => o.target_field_id)).toEqual(['fld-a']);
    const written = onUpdateModel.mock.calls[0]?.[0] as V4ExtractNodeDocument;
    expect(written.targets[0].outputs.map(o => o.target_field_id)).toEqual(['fld-a']);
  });

  it('不可编辑态（readonly）：finalize() 直接返回 undefined，不下发 tokenize', async () => {
    const { wrapper, host } = mountSurface({ readonly: true });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result).toBeUndefined();
    expect(host!.tokenize).not.toHaveBeenCalled();
  });

  it('宿主缺 tokenize：整体失败并带具名诊断，不抛异常也不回写草稿', async () => {
    const host = stubHost();
    // 显式抹掉 tokenize，模拟老宿主未升级（AliceV4Host 类型上它是必选方法，故用类型断言构造）。
    (host as unknown as Record<string, unknown>).tokenize = undefined;
    const { wrapper, onUpdateModel } = mountSurface({ host });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect(result?.diagnostics[0]?.diagnostic.code).toBe('HOST_TOKENIZE_MISSING');
    expect(onUpdateModel).not.toHaveBeenCalled();
  });
});

describe('ActionConfigSurface 挂载期可读文本还原（作用域先于首次 restore）', () => {
  it('首次 restore 之前 host.setScope 已带着文档来源被调用（否则还原因找不到来源实例而退回 token 化源码）', async () => {
    const host = stubHost();
    const { wrapper } = mountSurface({ host });
    await wrapper.vm.$nextTick();

    const setScope = host.setScope as ReturnType<typeof vi.fn>;
    const restore = host.restore as ReturnType<typeof vi.fn>;
    expect(setScope).toHaveBeenCalled();
    expect(restore).toHaveBeenCalled();
    // vi.fn 的 invocationCallOrder 是跨 mock 的全局序号：setScope 的首次调用必须排在 restore 的首次调用之前。
    expect(setScope.mock.invocationCallOrder[0]).toBeLessThan(restore.mock.invocationCallOrder[0]);
  });
});

describe('ActionConfigSurface 本报表来源目录兜底（防止条件编辑器引用未使用过的本报表明细表时死锁）', () => {
  it('目录加载完成后，setScope 的 report_sources 补齐目录里尚未被 query.sources 登记的本报表表', async () => {
    // 最小前置条件：文档从未引用过「采购明细」（query.sources 里没有它），
    // 但它是目录（当前模板全部表）的一员——与字段树 ctx.tableFields 同源，用户能在字段树里点选到它。
    const host = stubHost({
      loadCatalog: vi.fn(async () => ({
        renamed: false,
        sources: [],
        report: {
          tables: [
            { table_id: 'tbl-main', label: '采购订单的主表', role: 'main' as const, fields: [] },
            { table_id: 'tbl-detail', label: '采购明细', role: 'detail' as const, fields: [] }
          ]
        },
        staging: { columns: [] },
        functions: [],
        keywords: []
      }))
    });
    const { wrapper } = mountSurface({ host });
    await wrapper.vm.$nextTick();
    // loadCatalog() 是 onMounted 里的 await 调用：给挂载期的微任务队列一次机会走完，
    // 让 catalog.value 真正被赋值、watch 再次同步触发。
    await flushPromises();

    const setScope = host.setScope as ReturnType<typeof vi.fn>;
    // 在目录到位之后的某一次 setScope 调用里，report_sources 必须已经含「采购明细」——
    // 不要求用户先在任何表达式里引用过它（这正是死锁的成因：引用它需要它先在作用域里，
    // 它进作用域却又依赖"已被引用并确认"）。
    const sawDetailTable = setScope.mock.calls.some(([scope]) =>
      (scope as { report_sources: Array<{ table_id: string; table_role: string; alias: string }> }).report_sources.some(
        r => r.table_id === 'tbl-detail' && r.table_role === 'detail' && r.alias === '采购明细'
      )
    );
    expect(sawDetailTable).toBe(true);
  });
});

describe('ActionConfigSurface 可读文本（显示 restore 的 readable_text 而不是 token 化 text）', () => {
  it('输出行表达式显示去标记的完整显示路径；老服务端不带 readable_text 时退回 text', async () => {
    const doc = docWithFilter();
    doc.targets = [
      {
        target_id: 'tgt-x',
        target_data_table_id: 'tbl-x',
        table_role: 'main',
        outputs: [
          {
            target_field_id: 'fld-a',
            expression: {
              expression_id: 'out:tbl-x:fld-a',
              purpose: 'output',
              source_text: '[金额]#{r1}',
              refs: [{ ref_id: 'r1', kind: 'source_field', source_instance_id: 'o', field_id: 'f1', last_label: '[订单].[金额]' }]
            },
            fill_strategy: 'overwrite'
          }
        ],
        clear: { mode: 'none', timing: 'before' }
      }
    ] as unknown as V4ExtractNodeDocument['targets'];
    const restore = vi.fn(async (req: { source_text: string }) =>
      req.source_text.includes('#{')
        ? { ok: true, text: req.source_text, readable_text: '[订单].[金额]', digest: 'd', diagnostics: [] }
        : { ok: true, text: req.source_text, digest: 'd', diagnostics: [] }
    );
    const { wrapper } = mountSurface({ host: stubHost({ restore: restore as unknown as AliceV4Host['restore'] }), doc });
    const stub = () => wrapper.findComponent(ExpressionEditorStub);
    await vi.waitFor(() => {
      const model = stub().props('modelValue') as { targets: Array<{ outputs: Array<{ expression: { text_source?: { text: string } } }> }> };
      expect(model.targets[0].outputs[0].expression.text_source?.text).toBe('[订单].[金额]');
    });
    // 筛选表达式（无标记、restore 不带 readable_text）退回 text 原样。
    const model = stub().props('modelValue') as { where_text_source?: { text: string }; where?: unknown };
    expect(JSON.stringify(model)).toContain('[订单].[金额] > 100');
    wrapper.unmount();
  });
});

describe('ActionConfigSurface.test()（两段式 finalize → compile）', () => {
  it('收口未通过：直接汇总其诊断，不调用 host.compile', async () => {
    const tokenize = vi.fn(async () => ({
      ok: false,
      source_text: '',
      refs: [],
      digest: '',
      diagnostics: [{ code: 'E1', phase: 'tokenize' as const, severity: 'error' as const, message: '筛选表达式非法' }]
    }));
    const { wrapper, host } = mountSurface({ host: stubHost({ tokenize }) });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();

    expect(result.ok).toBe(false);
    expect(result.diagnostics).toEqual(['筛选表达式非法']);
    expect(host!.compile).not.toHaveBeenCalled();
  });

  it('收口通过：调用 host.compile(收口后的文档) 并汇总各目标诊断', async () => {
    const compile = vi.fn(async () => ({
      ok: false,
      targets: [{ kind: 'fill' as const, index: 0, owner_id: 'target-1', diagnostics: [{ code: 'E2', phase: 'compile' as const, severity: 'error' as const, message: '输出字段类型不匹配' }] }]
    }));
    const { wrapper } = mountSurface({ host: stubHost({ compile }) });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();

    expect(compile).toHaveBeenCalledTimes(1);
    expect(result.ok).toBe(false);
    expect(result.diagnostics).toEqual(['输出字段类型不匹配']);
  });

  it('host 缺失（domain:alice-v4 未注入）：test() 直接返回失败，不抛异常', async () => {
    const { wrapper } = mountSurface({ omitHost: true });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();
    expect(result.ok).toBe(false);
  });

  it('测试通过文案按模式分叉：取数模式为「查询可以正常编译」，' +
    '与写模式（ActionConfigSurface.write.test.ts 同名用例）「写入规则可以正常编译」不同', async () => {
    const { wrapper } = mountSurface({ doc: docWithFilter() });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();

    expect(result.ok).toBe(true);
    const stub = wrapper.findComponent(ExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices).toEqual([{ severity: 'info', message: '测试通过：查询可以正常编译' }]);
  });
});

describe('ActionConfigSurface 自动按行列匹配只读预览（抽键在服务端展开期，收口不得改写用户设置）', () => {
  const TBL_DETAIL = 'tbl-detail';
  const FLD_CODE = 'fld-code';
  const filterTokenized = '[物料编号]#{r1} = [物料编号]#{r2}';
  const filterRefs: V4ExpressionRefWire[] = [
    { ref_id: 'r1', kind: 'source_field', source_instance_id: 't1', table_id: 'tbl-src', field_id: 'fld-src-code', last_label: '[来源].[物料编号]' },
    { ref_id: 'r2', kind: 'report_field', source_instance_id: 'rpt-tbl-detail-abc', table_id: TBL_DETAIL, field_id: FLD_CODE, last_label: '本报表.[明细].[物料编号]' }
  ];
  /** 服务端 bind.astSummary 形状：Comparison(EQUAL) 两侧 FieldRef 带 stableId 与 rune span。 */
  const filterAst = {
    kind: 'Comparison', operator: 'EQUAL', span: { start: 0, end: filterTokenized.length },
    left: { kind: 'FieldRef', stableId: 'r1', span: { start: 0, end: 12 } },
    right: { kind: 'FieldRef', stableId: 'r2', span: { start: 15, end: 27 } }
  };
  function docWithKeyEquality(): V4ExtractNodeDocument {
    const doc = emptyExtractNodeDocument();
    doc.query.filter = { expression_id: 'filter', purpose: 'filter', source_text: '[来源].[物料编号] = 本报表.[明细].[物料编号]', refs: [] };
    doc.targets = [
      {
        target_id: 'tgt-d',
        target_data_table_id: TBL_DETAIL,
        table_role: 'detail',
        outputs: [
          { target_field_id: 'fld-price', expression: { expression_id: `out:${TBL_DETAIL}:fld-price`, purpose: 'output', source_text: '[来源].[单价]', refs: [] }, fill_strategy: 'overwrite' }
        ],
        clear: { mode: 'mapped', timing: 'before' }
      }
    ] as unknown as V4ExtractNodeDocument['targets'];
    return doc;
  }
  /** tokenize 桩：筛选原文 → token 化文本 + 绑定；其它表达式原样回带（refs 保留）。 */
  function tokenizeStub() {
    return vi.fn(async (req: V4TokenizeRequest) => {
      if (req.purpose === 'filter' && !req.text.includes('#{')) return { ok: true, source_text: filterTokenized, refs: filterRefs, digest: 'd1', diagnostics: [] };
      return { ok: true, source_text: req.text, refs: req.refs ?? [], digest: 'd2', diagnostics: [] };
    });
  }
  const finalizeOf = (wrapper: ReturnType<typeof mountSurface>['wrapper']) =>
    (wrapper.vm as unknown as { finalize: () => Promise<V4NodeDocumentFinalizeResult | undefined> }).finalize();

  it('等式只产生一条 info 预览：筛选原样保留、不新增/改写任何输出行，回写草稿的文档与收口结果一致', async () => {
    const tokenize = tokenizeStub();
    const diagnose = vi.fn(async () => ({ ok: true, tokens: [], ast: filterAst, refs: [], envelope: null, digest: 'd', diagnostics: [] }));
    const { wrapper, onUpdateModel } = mountSurface({ host: stubHost({ tokenize, diagnose }), doc: docWithKeyEquality() });
    const result = await finalizeOf(wrapper);
    expect(result?.ok).toBe(true);
    // diagnose 收到的是 token 化文本 + 按 tokenize 绑定表换算的引用描述符。
    expect(diagnose).toHaveBeenCalledWith(expect.objectContaining({ text: filterTokenized, purpose: 'filter', refs: expect.arrayContaining([expect.objectContaining({ ref_id: 'r2', kind: 'report_field', table_id: TBL_DETAIL, field_id: FLD_CODE })]) }));
    // 用户设置零改写：等式留在筛选里；输出行仍只有用户配的那一条 overwrite，没有凭空多出 row_match 行。
    expect(result?.doc.query.filter?.source_text).toBe(filterTokenized);
    expect(result?.doc.query.filter?.refs.map(r => r.ref_id)).toEqual(['r1', 'r2']);
    expect(result!.doc.targets[0].outputs.map(o => [o.target_field_id, o.fill_strategy])).toEqual([['fld-price', 'overwrite']]);
    // 筛选只 tokenize 一次（不再因"剩余筛选改写"二次 tokenize）。
    expect(tokenize.mock.calls.filter(([req]) => req.purpose === 'filter')).toHaveLength(1);
    const info = result!.diagnostics.find(d => d.diagnostic.code === 'EXTRACT_AUTO_ROW_MATCH_KEY');
    expect(info).toBeDefined();
    expect(info!.source).toBe('筛选条件');
    expect(info!.diagnostic.severity).toBe('info');
    expect(info!.diagnostic.message).toContain('按行列匹配键');
    expect(info!.diagnostic.message).toContain('配置不会被改写');
    const written = onUpdateModel.mock.calls[0]?.[0] as V4ExtractNodeDocument;
    expect(written.query.filter?.source_text).toBe(filterTokenized);
    expect(written.targets[0].outputs).toHaveLength(1);
    wrapper.unmount();
  });

  it('diagnose 不可用或未回 AST：无预览通知，筛选与输出照样原样、收口照常成功', async () => {
    const tokenize = tokenizeStub();
    const diagnose = vi.fn(async () => ({ ok: false, tokens: [], ast: null, refs: [], envelope: null, digest: '', diagnostics: [] }));
    const { wrapper } = mountSurface({ host: stubHost({ tokenize, diagnose }), doc: docWithKeyEquality() });
    const result = await finalizeOf(wrapper);
    expect(result?.ok).toBe(true);
    expect(result?.doc.query.filter?.source_text).toBe(filterTokenized);
    expect(result!.doc.targets[0].outputs).toHaveLength(1);
    expect(result!.diagnostics.some(d => d.diagnostic.code === 'EXTRACT_AUTO_ROW_MATCH_KEY')).toBe(false);
    wrapper.unmount();
  });

  it('目标「自动按行列匹配」关闭（auto_row_match=false）：不调 diagnose、无预览、筛选原样', async () => {
    const tokenize = tokenizeStub();
    const diagnose = vi.fn(async () => ({ ok: true, tokens: [], ast: filterAst, refs: [], envelope: null, digest: 'd', diagnostics: [] }));
    const doc = docWithKeyEquality();
    (doc.targets[0] as unknown as { auto_row_match?: boolean }).auto_row_match = false;
    const { wrapper } = mountSurface({ host: stubHost({ tokenize, diagnose }), doc });
    const result = await finalizeOf(wrapper);
    expect(diagnose).not.toHaveBeenCalled();
    expect(result?.doc.query.filter?.source_text).toBe(filterTokenized);
    expect(result!.diagnostics.some(d => d.diagnostic.code === 'EXTRACT_AUTO_ROW_MATCH_KEY')).toBe(false);
    wrapper.unmount();
  });

  it('只有主表目标：不调 diagnose（主表结构性禁匹配）', async () => {
    const tokenize = tokenizeStub();
    const diagnose = vi.fn(async () => ({ ok: true, tokens: [], ast: filterAst, refs: [], envelope: null, digest: 'd', diagnostics: [] }));
    const doc = docWithKeyEquality();
    (doc.targets[0] as unknown as { table_role: string }).table_role = 'main';
    const { wrapper } = mountSurface({ host: stubHost({ tokenize, diagnose }), doc });
    const result = await finalizeOf(wrapper);
    expect(diagnose).not.toHaveBeenCalled();
    expect(result?.doc.query.filter?.source_text).toBe(filterTokenized);
    wrapper.unmount();
  });

  it('等式字段已被用户手动配成「按行列匹配」输出：不重复预览（手动声明优先，与服务端同口径），手动行原样保留', async () => {
    const tokenize = tokenizeStub();
    const diagnose = vi.fn(async () => ({ ok: true, tokens: [], ast: filterAst, refs: [], envelope: null, digest: 'd', diagnostics: [] }));
    const doc = docWithKeyEquality();
    doc.targets[0].outputs.push({
      target_field_id: FLD_CODE,
      expression: { expression_id: `out:${TBL_DETAIL}:${FLD_CODE}`, purpose: 'output', source_text: '[来源].[物料编号]', refs: [] },
      fill_strategy: 'row_match'
    } as unknown as V4ExtractNodeDocument['targets'][number]['outputs'][number]);
    const { wrapper } = mountSurface({ host: stubHost({ tokenize, diagnose }), doc });
    const result = await finalizeOf(wrapper);
    expect(result?.ok).toBe(true);
    expect(diagnose).toHaveBeenCalled();
    expect(result!.diagnostics.some(d => d.diagnostic.code === 'EXTRACT_AUTO_ROW_MATCH_KEY')).toBe(false);
    expect(result!.doc.targets[0].outputs.map(o => [o.target_field_id, o.fill_strategy])).toEqual([['fld-price', 'overwrite'], [FLD_CODE, 'row_match']]);
    wrapper.unmount();
  });
});

// kind=view 是正式来源形态，且允许跨应用来源；但共享包 `@kimposoft/expression-editor` 的
// sourcePickerOptions 缺省只有 { selectableKinds: ['table'] }，本配置面必须显式打开视图 + 跨应用，
// 否则①来源段「＋添加」只能选数据表。
describe('①来源段 sourcePickerOptions（放开视图与跨应用来源）', () => {
  it('显式放开 selectableKinds=[table,view] 且 allowCrossApp=true（取数模式）', () => {
    const { wrapper } = mountSurface();
    const stub = wrapper.findComponent(ExpressionEditorStub);
    expect(stub.props('sourcePickerOptions')).toEqual({ selectableKinds: ['table', 'view', 'external'], allowCrossApp: true });
    wrapper.unmount();
  });
});
