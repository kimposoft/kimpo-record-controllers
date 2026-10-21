/**
 * ActionConfigSurface 写模式（新增/修改/删除数据配置面）。
 *
 * 覆盖范围（与 ActionConfigSurface.finalize.test.ts 的取数模式测试互补，测试基础设施同款套路：
 * stub 顶掉 ExpressionEditor 真组件、经 defineExpose 的 finalize/test duck-typing 约定驱动）：
 * ① 三个写动作 bind 各自挂载后 write-operation 正确、初始模型是对应操作的空白写文档；
 * ② 模型往返：编辑器 emit 的 ExpressionModel 经写节点适配器翻回 write_v4 文档并回写宿主草稿；
 * ③ finalize 客户端闸一条（insert 赋值必填）+ 三操作统一的「写入目标未选」闸（「目标先行」：
 *    target.data_table_id 为空即拒）+ 两条 info 级只读诊断（均不阻断保存）：insert 到扩展表且
 *    定位条件未引用目标模板主表时提示；update/delete 定位条件为空时提示将命中目标表全部记录
 *    （不写条件 = 修改/删除目标表全部行，合法，诊断码 WRITE_WHERE_UNCONSTRAINED）；
 * ④ 静态提醒 notices（写目标属于当前模板 / 定时任务引用本报表字段）不需要用户先点保存即可见；
 * ⑤ test()：finalize 收口成功后调用 host.compile，请求体是写文档（schema_version=write-node/v4）。
 *
 * 目标身份写法：经过 finalize()/test() 的用例统一按 target.data_table_id/template_id 构造
 * （与 ActionConfigSurface.vue 的 WRITE_TARGET_REQUIRED 闸同口径——该闸只认 data_table_id，
 * 不对 instance_id 做兼容折算，见其函数头注释）；「模型往返」「静态提醒」两个 describe 块里
 * 不经过该闸（不调 finalize()/test()）的用例仍用 target.instance_id 写法，验证的是共享包
 * 适配器的当前行为。
 */
import { defineComponent, h } from 'vue';
import { flushPromises, mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import type { InteractiveSessionHandle, InteractiveSessionSnapshot } from '@kimposoft/plugin-sdk';
import {
  emptyWriteNodeDocument,
  type AliceV4Host,
  type ExpressionModel,
  type V4DemoCatalog,
  type V4NodeDocumentDiagnosticItem,
  type V4WriteNodeDocument
} from '@kimposoft/expression-editor';
import ActionConfigSurface from './ActionConfigSurface.vue';

function snapshot(state: InteractiveSessionSnapshot['state'], patch: Partial<InteractiveSessionSnapshot> = {}): InteractiveSessionSnapshot {
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

function baseCatalog(overrides: Partial<V4DemoCatalog> = {}): V4DemoCatalog {
  return { renamed: false, sources: [], report: { tables: [] }, staging: { columns: [] }, functions: [], keywords: [], ...overrides };
}

/** 桩 AliceV4Host：tokenize 默认原样回带文本（不重绑 refs），便于断言收口后的 doc 形状。 */
function stubHost(overrides: Partial<AliceV4Host> = {}): AliceV4Host {
  return {
    loadCatalog: vi.fn(async () => baseCatalog()),
    diagnose: vi.fn(async () => ({ ok: true, tokens: [], ast: null, refs: [], envelope: null, digest: '', diagnostics: [] })),
    tokenize: vi.fn(async req => ({ ok: true, source_text: req.text, refs: req.refs ?? [], digest: 'd', diagnostics: [] })),
    restore: vi.fn(async () => ({ ok: true, text: '', digest: '', diagnostics: [] })),
    compile: vi.fn(async () => ({ ok: true, targets: [] })),
    setScope: vi.fn(),
    setNodeId: vi.fn(),
    ...overrides
  };
}

/** ExpressionEditor 桩：顶掉真组件，只记录写模式用例关心的 props（write-operation/notices）。
 *  sourcePickerOptions：「①来源段放开视图+跨应用、定时任务同链路也吃到该配置」用例需要读取。 */
const WriteExpressionEditorStub = defineComponent({
  props: ['modelValue', 'templateId', 'writeOperation', 'readonly', 'stagingSource', 'notices', 'sourcePickerOptions'],
  emits: ['update:modelValue'],
  setup: () => () => h('div', { 'data-testid': 'expression-editor-stub' })
});

function mountSurface(opts: {
  bind: string;
  doc: V4WriteNodeDocument;
  host?: AliceV4Host;
  hostContext?: Record<string, unknown>;
  readonly?: boolean;
}) {
  const session = activeSession();
  const onUpdateModel = vi.fn();
  const host = opts.host ?? stubHost();
  const wrapper = mount(ActionConfigSurface, {
    props: {
      input: {
        surfaceType: 'action-config',
        bind: opts.bind,
        hostContext: { templateId: 'template-1', ...opts.hostContext },
        hostBridge: { interactiveSession: session, 'domain:alice-v4': host },
        props: {
          modelValue: opts.doc,
          readonly: opts.readonly === true,
          nodeId: 'node-1',
          onUpdateModel
        }
      }
    },
    global: { stubs: { ExpressionEditor: WriteExpressionEditorStub } }
  });
  return { wrapper, host, onUpdateModel };
}

/** 取 defineExpose 出来的 finalize/test（duck-typing 约定，壳侧同款取法）。 */
function exposed(wrapper: ReturnType<typeof mountSurface>['wrapper']) {
  return wrapper.vm as unknown as {
    finalize: () => Promise<{ ok: boolean; doc: V4WriteNodeDocument; diagnostics: V4NodeDocumentDiagnosticItem[] } | undefined>;
    test: () => Promise<{ ok: boolean; diagnostics: string[] }>;
  };
}

describe('ActionConfigSurface 写模式挂载', () => {
  it.each([
    ['data.create_record', 'insert'],
    ['data.update_record', 'update'],
    ['data.delete_record', 'delete']
  ] as const)('%s 挂载后 write-operation=%s，初始模型是对应操作的空白写文档', async (bind, op) => {
    const { wrapper } = mountSurface({ bind, doc: emptyWriteNodeDocument(op) });
    await wrapper.vm.$nextTick();
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    expect(stub.props('writeOperation')).toBe(op);
    const model = stub.props('modelValue') as ExpressionModel;
    expect(model.target.operation).toBe(op);
  });

  // 标准面板按种类服务所有同种类数据动作——宿主回落到
  // operation_kind:<kind> surface 时传入该 bind，或传第三方动作名并在 hostContext 下发 operationKind。
  it.each([
    ['operation_kind:create', undefined, 'insert'],
    ['operation_kind:update', undefined, 'update'],
    ['operation_kind:delete', undefined, 'delete'],
    ['org.example.data-actions.close_order', { operationKind: 'update' }, 'update']
  ] as const)('%s（种类回落）挂载后 write-operation=%s', async (bind, hostContext, op) => {
    const { wrapper } = mountSurface({ bind, hostContext, doc: emptyWriteNodeDocument(op) });
    await wrapper.vm.$nextTick();
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    expect(stub.props('writeOperation')).toBe(op);
  });

  it('data.extract_record（或未识别 bind）不受影响：write-operation 为 undefined（取数模式既有口径）', async () => {
    const { wrapper } = mountSurface({ bind: 'data.extract_record', doc: emptyWriteNodeDocument('insert') });
    await wrapper.vm.$nextTick();
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    expect(stub.props('writeOperation')).toBeUndefined();
  });

  // 写动作的①来源段（定位条件/关联用的来源表）同样显式放开视图+跨应用——
  // 与取数模式共用同一个 ExpressionEditor 挂载点，不受 write-operation 影响。
  it('写模式同样显式放开①来源段 sourcePickerOptions=[table,view]+跨应用', async () => {
    const { wrapper } = mountSurface({ bind: 'data.update_record', doc: emptyWriteNodeDocument('update') });
    await wrapper.vm.$nextTick();
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    expect(stub.props('sourcePickerOptions')).toEqual({ selectableKinds: ['table', 'view', 'external'], allowCrossApp: true });
  });
});

describe('ActionConfigSurface 写模式模型往返（v4WriteDocToExpressionModel / expressionModelToV4WriteDoc）', () => {
  it('编辑器 emit 更新的 ExpressionModel 经写适配器翻回 write_v4 文档并回写宿主草稿', async () => {
    const doc = emptyWriteNodeDocument('update');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-order' }
    ];
    doc.target = { instance_id: 't1' };
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.update_record', doc });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const currentModel = stub.props('modelValue') as ExpressionModel;
    const nextModel: ExpressionModel = {
      ...currentModel,
      target: { ...currentModel.target, outputs: [{ target_field_id: 'fld-x', expression: { kind: 'raw', text: "'1'" } }] }
    };
    stub.vm.$emit('update:modelValue', nextModel);
    await wrapper.vm.$nextTick();

    expect(onUpdateModel).toHaveBeenCalledTimes(1);
    const written = onUpdateModel.mock.calls[0]?.[0] as V4WriteNodeDocument;
    expect(written.schema_version).toBe('kimpo.write-node/v4');
    expect(written.operation).toBe('update');
    expect(written.assignments.map(a => a.target_field_id)).toEqual(['fld-x']);
    expect(written.assignments[0].expression.source_text).toBe("'1'");
  });
});

describe('ActionConfigSurface 写模式 finalize 客户端闸（深层结构仍由宿主保存闸兜底）', () => {
  // 修改/删除不要求定位条件——不写条件 = 修改/删除目标表全部行，合法；只给 info 级只读诊断
  // WRITE_WHERE_UNCONSTRAINED（不阻断保存），见 ActionConfigSurface.vue noticeWhereUnconstrained。
  it('update/delete 定位条件为空：finalize 不阻断，诊断码 WRITE_WHERE_UNCONSTRAINED（info 级），onUpdateModel 仍被调用', async () => {
    const doc = emptyWriteNodeDocument('update');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-order' }
    ];
    doc.target = { data_table_id: 'tbl-order', template_id: 'template-1' }; // 「目标先行」：目标身份统一走 data_table_id。
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    // query.filter 缺省未设置——判定为空。
    const host = stubHost({
      loadCatalog: vi.fn(async () =>
        baseCatalog({ report: { tables: [{ table_id: 'tbl-order', label: '订单表', role: 'main', fields: [] }] } })
      )
    });
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.update_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises(); // 等挂载期 loadCatalog() 落定，展示名解析依赖 catalog.value.report.tables。

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true); // info 级，不阻断保存。
    const notice = result?.diagnostics.find(d => d.diagnostic.code === 'WRITE_WHERE_UNCONSTRAINED');
    expect(notice?.diagnostic.severity).toBe('info');
    expect(notice?.diagnostic.message).toContain('订单表');
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('update/delete 定位条件非空：finalize 不产生 WRITE_WHERE_UNCONSTRAINED 诊断', async () => {
    const doc = emptyWriteNodeDocument('delete');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-order' }
    ];
    doc.target = { data_table_id: 'tbl-order', template_id: 'template-1' };
    doc.query.filter = { expression_id: 'f', purpose: 'filter', source_text: '[订单表].[状态] = 1', refs: [] };
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.delete_record', doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_WHERE_UNCONSTRAINED')).toBe(false);
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('insert 赋值为空：finalize 阻断，诊断码 WRITE_ASSIGNMENTS_REQUIRED', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-1' };
    // assignments 为空数组（emptyWriteNodeDocument 缺省即空）。
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_ASSIGNMENTS_REQUIRED')).toBe(true);
    expect(onUpdateModel).not.toHaveBeenCalled();
  });

  // 「目标先行」：三操作统一判「写入目标未选」——target.data_table_id 为空即拒（见
  // ActionConfigSurface.vue finalizeWriteV4 客户端闸②头注释）。
  it.each([
    ['data.create_record', 'insert'],
    ['data.update_record', 'update'],
    ['data.delete_record', 'delete']
  ] as const)('%s 写入目标未选（target.data_table_id 为空）：finalize 阻断，诊断码 WRITE_TARGET_REQUIRED', async (bind, op) => {
    const doc = emptyWriteNodeDocument(op); // target 恒空对象（emptyWriteNodeDocument 缺省即空）。
    // insert 补上闸①要求的赋值；update/delete 的定位条件不是必填项，这里仍给一条非空筛选只是
    // 为了不额外引入 WRITE_WHERE_UNCONSTRAINED 的 info 噪音，确保本例断言聚焦 WRITE_TARGET_REQUIRED。
    if (op === 'update' || op === 'delete') doc.query.filter = { expression_id: 'f', purpose: 'filter', source_text: '1 = 1', refs: [] };
    if (op === 'insert' || op === 'update') {
      doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    }
    const { wrapper, onUpdateModel } = mountSurface({ bind, doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_TARGET_REQUIRED')).toBe(true);
    expect(onUpdateModel).not.toHaveBeenCalled();
  });

  // info 级只读诊断：insert 到扩展表且定位条件
  // 未引用目标模板主表——运行期语义是"目标主表全部记录各得一份"，只提示、不阻断保存。
  it('insert 目标是扩展表且定位条件未引用目标模板主表：finalize 不阻断，诊断码 WRITE_V4_PARENT_UNCONSTRAINED（info 级）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-detail', template_id: 'template-1' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    // 定位条件引用的是「入库流水」这张目录来源，不是目标模板主表 tbl-main。
    doc.query.sources = [
      { kind: 'catalog', instance_id: 'cat-1', alias: '入库流水', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-src' }
    ];
    doc.query.filter = {
      expression_id: 'f',
      purpose: 'filter',
      source_text: '[入库流水].[状态]#{r1} = 1',
      refs: [{ ref_id: 'r1', kind: 'source_field', source_instance_id: 'cat-1', field_id: 'status', last_label: '[入库流水].[状态]' }]
    };
    const host = stubHost({
      loadCatalog: vi.fn(async () =>
        baseCatalog({
          report: {
            tables: [
              { table_id: 'tbl-main', label: '主表', role: 'main', fields: [] },
              { table_id: 'tbl-detail', label: '明细', role: 'detail', fields: [] }
            ]
          }
        })
      )
    });
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.create_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises(); // 等挂载期 loadCatalog() 落定，info 诊断依赖 catalog.value.report.tables。

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true); // info 级，不阻断保存。
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED' && d.diagnostic.severity === 'info')).toBe(true);
    expect(onUpdateModel).toHaveBeenCalledTimes(1); // 不阻断：照常回写草稿。
  });

  it('insert 目标是扩展表且定位条件引用了目标模板主表：不产生 WRITE_V4_PARENT_UNCONSTRAINED 诊断', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-detail', template_id: 'template-1' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    doc.query.sources = [{ kind: 'report', instance_id: 'rpt-main', alias: '主表', table_id: 'tbl-main', table_role: 'main' }];
    doc.query.filter = {
      expression_id: 'f',
      purpose: 'filter',
      source_text: '[主表].[单号]#{r1} = 1',
      refs: [{ ref_id: 'r1', kind: 'report_field', source_instance_id: 'rpt-main', table_id: 'tbl-main', field_id: 'no', last_label: '[主表].[单号]' }]
    };
    const host = stubHost({
      loadCatalog: vi.fn(async () =>
        baseCatalog({
          report: {
            tables: [
              { table_id: 'tbl-main', label: '主表', role: 'main', fields: [] },
              { table_id: 'tbl-detail', label: '明细', role: 'detail', fields: [] }
            ]
          }
        })
      )
    });
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED')).toBe(false);
  });

  it('insert 目标是当前模板的主表：不产生 WRITE_V4_PARENT_UNCONSTRAINED 诊断（主表恒一行不参与父锚定）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-main', template_id: 'template-1' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    const host = stubHost({
      loadCatalog: vi.fn(async () => baseCatalog({ report: { tables: [{ table_id: 'tbl-main', label: '主表', role: 'main', fields: [] }] } }))
    });
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED')).toBe(false);
  });

  it('insert 目标属于其它模板（跨模板写入）且隐含主表来源尚未注入 doc.query.sources：不产生 WRITE_V4_PARENT_UNCONSTRAINED（无来源可判，静默跳过，不阻断保存）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-detail', template_id: 'template-other' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    // 跨模板分支直接从 doc.query.sources 结构性识别隐含主表来源（不靠 listTables 查角色，
    // 见 ActionConfigSurface.vue noticeParentUnconstrained 头注释）——本用例
    // 故意不放任何来源，验证"来源段里就是没有配对主表"这一档仍正确静默跳过（不误报）。
    // mountSurface 默认也不装配 'domain:expression' 桥，双重确认本档判定不依赖该桥。
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED')).toBe(false);
  });

  it('insert 目标属于其它模板（跨模板写入），目标先行注入的隐含主表来源' +
    '已在 doc.query.sources 里且未被引用，宿主未提供 domain:expression 桥：finalize 仍产出 ' +
    'WRITE_V4_PARENT_UNCONSTRAINED（判定按来源段结构性识别，不依赖跨模板 listTables 查询，' +
    '展示名退回来源别名）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    // 目标：其它模板（出库流水）的扩展表——与「本报表」（template-1）不同模板。
    doc.target = { data_table_id: 'tbl-detail-other', template_id: 'template-other' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    doc.query.sources = [
      // 本报表主表 + 明细（当前设计模板 template-1，kind='report'）。
      { kind: 'report', instance_id: 'rpt-main', alias: '主表', table_id: 'tbl-main', table_role: 'main' },
      { kind: 'report', instance_id: 'rpt-detail', alias: '明细', table_id: 'tbl-detail-src', table_role: 'detail' },
      // 「目标先行」注入的隐含主表来源：出库流水（template-other）的主表，与目标表 tbl-detail-other
      // 不同 data_table_id——按结构性判定即被识别为父实例，不需要 listTables 查询确认它是「主表」。
      { kind: 'catalog', instance_id: 'tgt-tbl-main-other', alias: '出库流水的主表', app_id: 'app-1', template_id: 'template-other', data_table_id: 'tbl-main-other' }
    ];
    // 定位条件只引用本报表明细，不引用「出库流水的主表」——这是触发该诊断的关键条件。
    doc.query.filter = {
      expression_id: 'f',
      purpose: 'filter',
      source_text: '[明细].[数量]#{r1} > 0',
      refs: [{ ref_id: 'r1', kind: 'report_field', source_instance_id: 'rpt-detail', table_id: 'tbl-detail-src', field_id: 'qty', last_label: '[明细].[数量]' }]
    };
    // mountSurface 默认不装配 'domain:expression' 桥：即便跨模板角色查询完全走不通，
    // 诊断也必须触发（这正是本用例要验证的点）。
    const { wrapper, onUpdateModel } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true); // info 级，不阻断保存。
    const notice = result?.diagnostics.find(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED');
    expect(notice?.diagnostic.severity).toBe('info');
    // 展示名查不到真实表名（无 domain:expression 桥）时退回来源别名，消息里仍能看到具体是哪张表。
    expect(notice?.diagnostic.message).toContain('出库流水的主表');
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
  });

  it('test() 路径：同上场景经 test() 调用（finalize→host.compile 编译成功、' +
    '零编译诊断）后，「测试通过」提示不冲掉 finalize 阶段已产出的 WRITE_V4_PARENT_UNCONSTRAINED——' +
    'test() 编译成功分支须追加而非整份覆盖 sessionNotices', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-detail-other', template_id: 'template-other' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    doc.query.sources = [
      { kind: 'report', instance_id: 'rpt-main', alias: '主表', table_id: 'tbl-main', table_role: 'main' },
      { kind: 'report', instance_id: 'rpt-detail', alias: '明细', table_id: 'tbl-detail-src', table_role: 'detail' },
      { kind: 'catalog', instance_id: 'tgt-tbl-main-other', alias: '出库流水的主表', app_id: 'app-1', template_id: 'template-other', data_table_id: 'tbl-main-other' }
    ];
    doc.query.filter = {
      expression_id: 'f',
      purpose: 'filter',
      source_text: '[明细].[数量]#{r1} > 0',
      refs: [{ ref_id: 'r1', kind: 'report_field', source_instance_id: 'rpt-detail', table_id: 'tbl-detail-src', field_id: 'qty', last_label: '[明细].[数量]' }]
    };
    const host = stubHost({ compile: vi.fn(async () => ({ ok: true, targets: [] })) }); // 编译干净，零诊断。
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).test();

    expect(result.ok).toBe(true);
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    // 两条都要在——info 诊断没被「测试通过」冲掉，也确实看到了测试通过的提示。
    expect(notices.some(n => n.message.includes('出库流水的主表'))).toBe(true);
    expect(notices.some(n => n.message === '测试通过：写入规则可以正常编译')).toBe(true);
  });

  it('不可编辑态（readonly）：finalize() 直接返回 undefined，不下发 tokenize', async () => {
    const doc = emptyWriteNodeDocument('delete');
    const { wrapper, host } = mountSurface({ bind: 'data.delete_record', doc, readonly: true });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result).toBeUndefined();
    expect(host.tokenize).not.toHaveBeenCalled();
  });
});

describe('ActionConfigSurface 写模式 dedupe.field_ids 随赋值行剔除同步收窄（唯一键防重）', () => {
  // stripUnconfiguredWriteAssignments（同名函数注释）：finalize 收口时剔除表达式为空的赋值行，
  // 同时把 dedupe.field_ids 收窄成剩余 assignments 的子集——键引用的字段若已被剔，键也要跟着摘除。
  it('某防重键引用的字段表达式被清空（剔除）：dedupe.field_ids 同步摘除该键，保留仍存在的键', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-1' };
    doc.assignments = [
      { target_field_id: 'fld-a', expression: { expression_id: 'asg_fld-a', purpose: 'output', source_text: "'1'", refs: [] } },
      // fld-b 表达式为空，finalize 会把这一行整体剔除（stripUnconfiguredWriteAssignments）。
      { target_field_id: 'fld-b', expression: { expression_id: 'asg_fld-b', purpose: 'output', source_text: '', refs: [] } }
    ];
    doc.dedupe = { field_ids: ['fld-a', 'fld-b'], scope: 'all' };
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).finalize();

    expect(result?.doc.assignments.map(a => a.target_field_id)).toEqual(['fld-a']);
    expect((result?.doc as V4WriteNodeDocument).dedupe).toEqual({ field_ids: ['fld-a'], scope: 'all' });
  });

  it('防重键引用的字段全部被剔除：dedupe 段整体删除（服务端不接受空 field_ids）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-1' };
    doc.assignments = [{ target_field_id: 'fld-a', expression: { expression_id: 'asg_fld-a', purpose: 'output', source_text: '', refs: [] } }];
    doc.dedupe = { field_ids: ['fld-a'], scope: 'all' };
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    // insert 赋值收口后为空触发 WRITE_ASSIGNMENTS_REQUIRED（ok:false），顺带验证 dedupe 段仍被清空——
    // finalizeWriteV4 无论 ok 与否都返回 doc（见其函数体 `return { ok, doc, diagnostics }`）。
    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(false);
    expect((result?.doc as V4WriteNodeDocument).dedupe).toBeUndefined();
  });
});

describe('ActionConfigSurface 写模式静态提醒（notices 常显层，不需要用户先点保存）', () => {
  it('写目标属于当前模板：notices 常显一条 warning', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-1' }; // 与 hostContext.templateId 相同
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices.some(n => n.severity === 'warning' && n.message.includes('当前模板'))).toBe(true);
  });

  it('写目标属于其它模板：不出现该 warning', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-other' };
    const { wrapper } = mountSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices.some(n => n.message.includes('当前模板'))).toBe(false);
  });

  it('定时任务场景（hostContext.bandEventKeys 含 scheduled）且引用本报表字段：notices 追加一条 warning', async () => {
    const doc = emptyWriteNodeDocument('update');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-other', data_table_id: 'tbl-order' }
    ];
    doc.target = { instance_id: 't1' };
    doc.query.filter = {
      expression_id: 'filter',
      purpose: 'filter',
      source_text: '本报表.[明细].[数量] > 0',
      refs: [{ ref_id: 'r1', kind: 'report_field', source_instance_id: 'rpt-detail', table_id: 'tbl-detail', field_id: 'fld-qty', last_label: '本报表.[明细].[数量]' }]
    };
    const { wrapper } = mountSurface({ bind: 'data.update_record', doc, hostContext: { bandEventKeys: ['scheduled'] } });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices.some(n => n.severity === 'warning' && n.message.includes('定时任务'))).toBe(true);
  });

  it('非定时任务场景（bandEventKeys 未透传）：即使引用本报表字段也不出现该 warning', async () => {
    const doc = emptyWriteNodeDocument('update');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-other', data_table_id: 'tbl-order' }
    ];
    doc.target = { instance_id: 't1' };
    doc.query.filter = {
      expression_id: 'filter',
      purpose: 'filter',
      source_text: '本报表.[明细].[数量] > 0',
      refs: [{ ref_id: 'r1', kind: 'report_field', source_instance_id: 'rpt-detail', table_id: 'tbl-detail', field_id: 'fld-qty', last_label: '本报表.[明细].[数量]' }]
    };
    const { wrapper } = mountSurface({ bind: 'data.update_record', doc });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices.some(n => n.message.includes('定时任务'))).toBe(false);
  });
});

describe('ActionConfigSurface 无模板（定时任务/任务级动作设计器）', () => {
  // 场景：定时任务里新建「新增数据」节点。宿主对任务级场景从不打开模板编辑租约，会话 mode 恒非
  // 'edit'；sessionEditable 若一律要求 mode!=='view'，配置面会整体只读并弹出共享包 ExpressionEditor
  // 对 readonly=true 的固定「权限不足」横幅（并非真正的跨应用权限判定，见 sessionEditable 上方注释）。
  // 默认 activeSession()（mode='edit'）覆盖不到这一档，故直接构造 mode='view' 的会话。
  function viewModeActiveSession(): InteractiveSessionHandle {
    return Object.freeze({
      snapshot: snapshot('active', { mode: 'view' }),
      markDirty: vi.fn(),
      onChange: vi.fn(() => () => undefined),
      requestEdit: vi.fn(async () => undefined),
      adminRelease: vi.fn(async () => undefined),
      close: vi.fn(async () => undefined)
    });
  }

  function mountJobSurface(opts: { bind: string; doc: V4WriteNodeDocument; host?: AliceV4Host }) {
    const session = viewModeActiveSession();
    const onUpdateModel = vi.fn();
    const host = opts.host ?? stubHost();
    const wrapper = mount(ActionConfigSurface, {
      props: {
        input: {
          surfaceType: 'action-config',
          bind: opts.bind,
          hostContext: { templateId: '' }, // 定时任务/任务级动作设计器：无模板上下文。
          hostBridge: { interactiveSession: session, 'domain:alice-v4': host },
          props: { modelValue: opts.doc, nodeId: 'node-1', onUpdateModel }
        }
      },
      global: { stubs: { ExpressionEditor: WriteExpressionEditorStub } }
    });
    return { wrapper, host, onUpdateModel };
  }

  it('无 templateId + 会话 mode=view：可编辑（不再被误判只读，超管也能选目标表）', async () => {
    const { wrapper } = mountJobSurface({ bind: 'data.create_record', doc: emptyWriteNodeDocument('insert') });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    expect(stub.props('readonly')).toBe(false);
    expect(stub.props('templateId')).toBe('');
    // 定时任务（templateId=''）与模板设计态走同一条 ExpressionEditor 挂载路径，
    // 同样吃到①来源段的 sourcePickerOptions 显式配置。
    expect(stub.props('sourcePickerOptions')).toEqual({ selectableKinds: ['table', 'view', 'external'], allowCrossApp: true });
  });

  it('目录只含 catalog 候选（report 为空）：不崩溃，目标已选（data_table_id 非空）不触发 WRITE_TARGET_REQUIRED，也不误判 WRITE_V4_PARENT_UNCONSTRAINED（无模板上下文，拿不到目标模板角色信息，静默跳过）', async () => {
    const doc = emptyWriteNodeDocument('insert');
    doc.target = { data_table_id: 'tbl-x', template_id: 'template-other' };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    const host = stubHost({
      loadCatalog: vi.fn(async () =>
        baseCatalog({
          candidates: [{ app_id: 'app-1', template_id: 'template-other', data_table_id: 'tbl-x', label: '候选表', fields: [] }]
        })
      )
    });
    const { wrapper } = mountJobSurface({ bind: 'data.create_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises();

    const result = await exposed(wrapper).finalize();

    expect(result?.ok).toBe(true);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_TARGET_REQUIRED')).toBe(false);
    expect(result?.diagnostics.some(d => d.diagnostic.code === 'WRITE_V4_PARENT_UNCONSTRAINED')).toBe(false);
  });

  it('写目标选择器选中的表（onSelectWriteTarget 已给 target_template_id）经模型往返落进 write_v4.target.template_id', async () => {
    const doc = emptyWriteNodeDocument('insert');
    const { wrapper, onUpdateModel } = mountJobSurface({ bind: 'data.create_record', doc });
    await wrapper.vm.$nextTick();

    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const currentModel = stub.props('modelValue') as ExpressionModel;
    // 模拟④写目标段选中了「候选表」（写目标不看当前模板，可跨模板选择，见共享包
    // ExpressionEditor.vue onSelectWriteTarget：命中候选即写 target_template_id/target_data_table_id）。
    const nextModel: ExpressionModel = {
      ...currentModel,
      target: {
        ...currentModel.target,
        operation: 'insert',
        target_template_id: 'template-picked',
        target_data_table_id: 'tbl-picked',
        outputs: [{ target_field_id: 'fld-x', expression: { kind: 'raw', text: "'1'" } }]
      }
    };
    stub.vm.$emit('update:modelValue', nextModel);
    await wrapper.vm.$nextTick();

    // 模型往返（onExpressionModelUpdate）是「收口」把用户选择落进权威文档的唯一通道——finalize()
    // 本身不改写 target 段（只 tokenize 各表达式），故这里断言的是 onUpdateModel 收到的 write_v4。
    expect(onUpdateModel).toHaveBeenCalledTimes(1);
    const written = onUpdateModel.mock.calls[0]?.[0] as V4WriteNodeDocument;
    expect(written.target).toMatchObject({ template_id: 'template-picked', data_table_id: 'tbl-picked' });
  });
});

describe('ActionConfigSurface.test()（写模式：finalize → host.compile）', () => {
  it('收口成功后调用 host.compile，请求体是写文档（schema_version=kimpo.write-node/v4）', async () => {
    // 显式声明入参类型（不是无参箭头）：vi.fn 的无参箭头推不出 mock.calls 的元组元素类型
    // （TS2493）。
    const compile = vi.fn(async (_doc: V4WriteNodeDocument) => ({ ok: true, targets: [] }));
    const doc = emptyWriteNodeDocument('delete');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-order' }
    ];
    doc.target = { data_table_id: 'tbl-order', template_id: 'template-1' }; // 「目标先行」：目标身份统一走 data_table_id。
    doc.query.filter = { expression_id: 'filter', purpose: 'filter', source_text: '[订单].[状态] = 1', refs: [] };
    const { wrapper } = mountSurface({ bind: 'data.delete_record', doc, host: stubHost({ compile }) });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();

    expect(compile).toHaveBeenCalledTimes(1);
    const sentDoc = compile.mock.calls[0]?.[0] as V4WriteNodeDocument;
    expect(sentDoc.schema_version).toBe('kimpo.write-node/v4');
    expect(sentDoc.operation).toBe('delete');
    expect(result.ok).toBe(true);
  });

  // 修改/删除定位条件可空、不阻断——test() 应正常收口→编译，收口阶段的
  // WRITE_WHERE_UNCONSTRAINED info 提示随「测试通过」一并保留（同上方 WRITE_V4_PARENT_UNCONSTRAINED
  // 的 test() 路径用例：编译成功分支是追加而非整份覆盖 sessionNotices）。
  it('定位条件为空（不阻断）：test() 仍调用 host.compile 且测试通过，notices 含 WRITE_WHERE_UNCONSTRAINED 提示', async () => {
    // 显式声明入参类型（不是无参箭头）：vi.fn 的无参箭头推不出 mock.calls 的元组元素类型
    // （TS2493）。
    const compile = vi.fn(async (_doc: V4WriteNodeDocument) => ({ ok: true, targets: [] }));
    const doc = emptyWriteNodeDocument('delete');
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-1', data_table_id: 'tbl-order' }
    ];
    doc.target = { data_table_id: 'tbl-order', template_id: 'template-1' }; // 「目标先行」：目标身份统一走 data_table_id。
    // query.filter 缺省未设置——不再判定为错误。
    const host = stubHost({
      compile,
      loadCatalog: vi.fn(async () =>
        baseCatalog({ report: { tables: [{ table_id: 'tbl-order', label: '订单表', role: 'main', fields: [] }] } })
      )
    });
    const { wrapper } = mountSurface({ bind: 'data.delete_record', doc, host });
    await wrapper.vm.$nextTick();
    await flushPromises(); // 等挂载期 loadCatalog() 落定，展示名解析依赖 catalog.value.report.tables。

    const result = await exposed(wrapper).test();

    expect(compile).toHaveBeenCalledTimes(1);
    expect(result.ok).toBe(true);
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices.some(n => n.message.includes('订单表'))).toBe(true);
    expect(notices.some(n => n.message === '测试通过：写入规则可以正常编译')).toBe(true);
  });
});

/**
 * test() 成功文案按模式分叉：取数措辞「测试通过：查询可以正常编译」用在写模式不准确（写模式不是
 * 「查询」），故按 isWriteMode 经 st() 分叉 surface.test.success_extract / surface.test.success_write
 * 两把四语键。经 notices 常显层断言
 * （sessionNotices 不直接对外暴露，走 WriteExpressionEditorStub 记录的 notices prop 同款先例）。
 */
describe('ActionConfigSurface.test() 成功文案按模式分叉', () => {
  it('写模式（data.update_record）测试通过：notices 文案为「写入规则可以正常编译」', async () => {
    const doc = emptyWriteNodeDocument('update');
    // 写目标来源模板故意与 hostContext.templateId 不同（'template-2' vs 'template-1'）：避免触发
    // staticNotices 的「写目标属于当前模板」常显警告，使 notices 只剩本用例要断言的测试通过文案。
    doc.query.sources = [
      { kind: 'catalog', instance_id: 't1', alias: '订单表', app_id: 'app-1', template_id: 'template-2', data_table_id: 'tbl-order' }
    ];
    doc.target = { data_table_id: 'tbl-order', template_id: 'template-2' }; // 「目标先行」：目标身份统一走 data_table_id。
    doc.query.filter = { expression_id: 'filter', purpose: 'filter', source_text: '[订单].[状态] = 1', refs: [] };
    doc.assignments = [{ target_field_id: 'fld-x', expression: { expression_id: 'asg_fld-x', purpose: 'output', source_text: "'1'", refs: [] } }];
    const { wrapper } = mountSurface({ bind: 'data.update_record', doc, host: stubHost({ compile: vi.fn(async () => ({ ok: true, targets: [] })) }) });
    await wrapper.vm.$nextTick();

    const result = await exposed(wrapper).test();

    expect(result.ok).toBe(true);
    const stub = wrapper.findComponent(WriteExpressionEditorStub);
    const notices = stub.props('notices') as Array<{ severity: string; message: string }>;
    expect(notices).toEqual([{ severity: 'info', message: '测试通过：写入规则可以正常编译' }]);
  });
});
