<script setup lang="ts">
/**
 * ActionConfigSurface — 数据动作的 action-config surface 组件（extraction bundle 内）。
 *
 * 本配置面挂载的编辑器是共享包 `@kimposoft/expression-editor` 的 ExpressionEditor 本体。权威模型是动作
 * 参数键 extract_v4 承载的 V4ExtractNodeDocument；ExpressionEditor 吃的是通用查询模型 ExpressionModel，
 * 故由共享包适配器 v4DocToExpressionModel / expressionModelToV4Doc 双向翻译（见下方「模型往返」段），
 * 宿主侧再由 createV4ExpressionHost 把 ExpressionHost 转接到 V4 引擎。sources/report_sources 作用域
 * 信封（host.setScope）与 compile node_id（host.setNodeId）在本文件下发。
 * 链路归属：数据动作插件（kimpo-record-controllers）前端 · 设计态；上游由宿主提供 'domain:alice-v4'
 * 桥的生产实现，由动作设计器插件（kimpo-action-designer）的节点配置弹窗作为壳装配 hostBridge/props。
 *
 * 宿主在 createSurface() 后将响应式 surfaceInput 以 :input 传入：
 * - input.hostBridge['domain:alice-v4']：AliceV4Host 生产桥（loadCatalog/diagnose/tokenize/restore/
 *   compile 五个必选方法 + setScope/setNodeId 两个可选方法）；缺失时不抛错，走本文件错误态渲染
 *   （不阻塞整个动作设计器弹窗）。
 * - input.hostBridge.i18n：HostI18n（可选）。ExpressionEditor 的 props 契约没有 labels 类入口，故
 *   ExpressionEditor 本体不消费它；本文件自身的运行期文案经 ./i18n.ts 的 createSurfaceI18n 消费——
 *   见下方「hostBridge.i18n 的能力声明」段与 st()/staticNotices 用法。
 * - input.hostContext.templateId：当前模板 ID。
 * - input.props.modelValue：当前 V4ExtractNodeDocument（响应式，取数节点参数键 extract_v4）。
 * - input.props.onUpdateModel：回调，写回宿主 draft。
 * - input.props.nodeId：当前取数节点的真实节点 ID（供 host.setNodeId）。
 *
 * 图表目标区：图表目录由 ExpressionEditor 经 domain:expression 桥（EXPRESSION_HOST.fetchChartCatalog）
 * 自行拉取，本文件不参与。图表目标本身存活在权威模型 V4ExtractNodeDocument.targets 里——ExpressionModel
 * 装不下的 V4 独有信息由双向翻译原样保留，往返不丢（见下方「模型往返」段）。
 *
 * 🔴 hostBridge.i18n 的能力声明：宿主装配时传的桥键是裸 'i18n'，并会**静默**滤掉 manifest host_caps
 * 未声明的键，因此 manifest.json 的 host_caps 必须声明 'i18n'（裸键写法，与本插件既有的
 * request/message/log 及同族 kimpo-action-designer 一致；宿主对未映射的 cap 原样保留键名，所以裸键
 * 直达 bridge 的 i18n 属性）。守卫见 ActionConfigSurface.hostCaps.test.ts。
 * ⚠️ manifest 改动必须两份同步——cmd/plugin/manifest.json 是 go:embed 副本，退役棘轮
 * （internal/actions/extract/retirement_ratchet_test.go）判据是「存在时须与根清单逐字一致」。
 *
 * stagingSource（暂存表来源虚拟表）：本文件把壳侧传入的 input.props.stagingSource 原样透传给
 * ExpressionEditor（ExpressionEditorProps.stagingSource），由它渲染①来源段的「上一步暂存区」
 * （V4ExtractNodeDocument 的 query.sources 原生支持 kind='staging'）。
 *
 * 写模式：本 surface 同时服务四个 bind——`data.extract_record`（取数，上文所述路径）与三个写动作
 * `data.create_record`/`data.update_record`/`data.delete_record`。写/取数二选一由 `writeOperation`
 * （从 `props.input.bind` 派生，见下方同名 computed）单点判定，贯穿本文件：
 * - 权威模型为 V4WriteNodeDocument（参数键 write_v4），isWriteNodeDocument/emptyWriteNodeDocument
 *   守卫/兜底空文档；
 * - 模型往返走共享包写节点适配器 v4WriteDocToExpressionModel/expressionModelToV4WriteDoc——来源/关联/
 *   筛选段与取数节点同构（两者共用 V4QueryDefinition），故 hostScopeFromDocument/adapterContext 两段
 *   无需按模式分叉；
 * - `<ExpressionEditor>` 多传 `write-operation` prop，编辑器内部据此渲染④写目标段（新增/修改/
 *   删除）而不是取数的字段填充映射；
 * - 收口（finalizeToV4）有写模式客户端校验（insert 赋值非空、三操作统一「写入目标未选即拒」），理由与
 *   逐条注释见下方「写模式收口」段——深层结构完整性仍由宿主保存校验兜底，客户端只做用户能立刻看懂的
 *   早期拦截。父记录不是用户可配项，恒取自查询结果里目标模板主表这条隐含来源；insert 到扩展表额外产出
 *   一条 info 级只读诊断（见 noticeParentUnconstrained）；update/delete 定位条件可空（= 修改/删除目标表
 *   全部行），只产出 info 级只读提示 WRITE_WHERE_UNCONSTRAINED（见 noticeWhereUnconstrained）；
 * - notices 汇总区有「静态」层（不需要用户点保存就常显）：写目标属于当前模板的围栏提醒、定时任务
 *   场景引用本报表字段的可用性提醒，与「收口/测试」产生的一次性诊断分层叠加，见下方
 *   staticNotices/sessionNotices 两个变量的注释。
 * 两条路径共用同一份挂载骨架与状态管理，只在少数几个点按 isWriteMode 分叉。
 */
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue';
import type { HostI18n, InteractiveSessionHandle, InteractiveSessionSnapshot } from '@kimposoft/plugin-sdk';
import {
  emptyExtractNodeDocument,
  emptyWriteNodeDocument,
  hostScopeFromDocument,
  isExtractNodeDocument,
  isWriteNodeDocument,
  provideAliceV4Host,
  provideExpressionHost,
  provideRefSyntax,
  ExpressionEditor,
  createV4ExpressionHost,
  v4DocToExpressionModel,
  expressionModelToV4Doc,
  v4WriteDocToExpressionModel,
  expressionModelToV4WriteDoc,
  stripUnconfiguredOutputs,
  fillEmptyStagingNamesForPreview,
  provideSystemVariableToken,
  type AliceV4Host,
  type ExpressionEditorProps,
  type ExpressionHost,
  type ExpressionModel,
  type ExpressionNotice,
  type V4DemoCatalog,
  type V4Diagnostic,
  type V4ExpressionRefWire,
  type V4ExtractNodeDocument,
  type V4NodeDocumentDiagnosticItem,
  type V4NodeDocumentFinalizeResult,
  type V4Purpose,
  type V4SourceBinding,
  type V4SourceBindingCatalog,
  type V4WriteNodeDocument,
  type V4WriteOperation
} from '@kimposoft/expression-editor';
import { previewAutoRowMatchKeys, type FilterAstNode } from './extractRowMatchKeys';
import { createSurfaceI18n } from './i18n';
import { resolveDataActionKind, writeOperationForKind } from './actionKind';

// 类型故意宽松（Record）：避免在 bundle 内引入宿主/SDK 类型，切断编译期耦合。
const props = defineProps<{
  input: {
    surfaceType: string;
    bind?: string;
    hostContext: Record<string, unknown>;
    hostBridge: Record<string, unknown> & { interactiveSession?: InteractiveSessionHandle; i18n?: HostI18n };
    props: Record<string, unknown>;
  };
}>();

const interactiveSession = props.input.hostBridge.interactiveSession;
if (!interactiveSession) {
  throw new Error('[extraction] action-config 未继承父交互会话');
}
const sessionSnapshot = shallowRef<Readonly<InteractiveSessionSnapshot>>(interactiveSession.snapshot);
const scrubbed = shallowRef(sessionSnapshot.value.state === 'auth_ended');
let stopSessionChange: (() => void) | undefined;

// 可编辑判据按挂载场景分叉：有模板（模板设计态）要求会话 active 且 mode !== 'view'；无模板（定时
// 任务等任务级动作设计器，hostContext.templateId 为空）宿主从不打开模板设计编辑租约，snapshot.mode
// 恒非 'edit'，只看会话是否 active——与动作设计器插件侧的可编辑判据同源。否则任务级场景会恒判只读，
// 编辑器显示的是 `readonly=true` 的固定只读横幅（与真实的跨应用权限判定无关）。
const hasTemplateContext = computed(() => !!(props.input.hostContext.templateId as string | undefined));

const sessionEditable = computed(
  () =>
    !scrubbed.value &&
    props.input.props.readonly !== true &&
    sessionSnapshot.value.state === 'active' &&
    (!hasTemplateContext.value || sessionSnapshot.value.mode !== 'view')
);

// 本插件前端 i18n（见 ./i18n.ts 头注释）：host 缺席时自动降级为纯本地链，不阻塞渲染。localeTick
// 递增触发依赖它的 computed 重算——st() 本身不是响应式的（createPluginI18n 返回的是普通函数，不是 ref）。
const surfaceI18n = createSurfaceI18n(props.input.hostBridge.i18n);
const localeTick = ref(0);
let stopLocaleChange: (() => void) | undefined;
/** 取本插件文案（写模式新增的运行期用户可见文案专用，见 staticNotices/finalizeWriteV4）。 */
function st(key: string, params?: Record<string, unknown>): string {
  return surfaceI18n.t(key, params);
}

// host：'domain:alice-v4' 桥缺失时不抛错——渲染本文件的错误态（见下方 template），不阻塞整个弹窗。
// provideAliceV4Host 必须在 setup() 顶层调用（Vue injection 约束），仅在 host 就绪时调用一次。
const host = props.input.hostBridge['domain:alice-v4'] as AliceV4Host | undefined;
const hostMissing = !host;
if (host) {
  provideAliceV4Host(host);
}

// 表达式域桥：宿主与 'domain:alice-v4' 一同注入，形状即完整 ExpressionHost（listTemplates/listApps/
// fetchTree/listTables/listFields/…）。来源区用共享包的来源树选择器（SourceTreePicker，内部 inject
// EXPRESSION_HOST），必须把它 provide 下去，否则子组件取不到宿主。缺桥时不抛错（不 provide）——由
// ExpressionEditor 按 inject 到的宿主是否就绪降级渲染（不阻塞整个配置面）。
const expressionHost = props.input.hostBridge['domain:expression'] as ExpressionHost | undefined;
if (expressionHost) {
  // 配置面直接挂 ExpressionEditor，它对宿主的全部要求都收在 ExpressionHost 这一个接口上。其中只有
  // lowerText（文本→结构化降级）链路语义不同，故用共享包的适配器把它转接到 V4 的 tokenize——包内
  // 组件一行不改。alice-v4 桥缺失时退回原桥（此时页面已走 hostMissing 错误态，编辑器不渲染，注入
  // 什么都不会被用到）。
  provideExpressionHost(
    host
      ? {
          ...createV4ExpressionHost(expressionHost, host),
          // compilePreview 也必须接管：编辑器用它做实时语法检查，而通用编译预览端点校验的是
          // ExpressionModel（会拒掉本适配用来承载 V4 表达式的 kind='raw' 占位，表现为常驻一条
          // 「校验未通过：expression_model.target.outputs[0].expression.kind」）。改为把当前模型
          // 翻回 V4 文档、走 V4 引擎的整节点编译——既是正确的语义，诊断也来自真正会执行的那条链路。
          compilePreview: async (_templateId, m) => {
            // 写模式与取数模式各走各自的反向翻译；写文档没有 staging_target/占位输出行那一套
            // （stripUnconfiguredOutputs/fillEmptyStagingNamesForPreview 是取数专属，类型上也只认
            // V4ExtractNodeDocument），改用本文件内的 stripUnconfiguredWriteAssignments 剔除表达式为空
            // 的赋值行——同一初衷（空 source_text 会被解码校验拒绝，淹没真正的诊断）。
            const doc = isWriteMode.value
              ? stripUnconfiguredWriteAssignments(expressionModelToV4WriteDoc(m, model.value as V4WriteNodeDocument, adapterContext.value))
              : // 占位输出行（编辑期保留的「不填充」行）不送服务端：解码校验会拒空 source_text，把真正的
                // 诊断淹没在「source_text 不能为空」里（见共享包 stripUnconfiguredOutputs）。
                // 暂存列空列名同款兜底（见共享包 fillEmptyStagingNamesForPreview 头注释）：用户刚新建一列
                // 还没起名就触发这次实时语法检查，服务端解码校验会先报「name 不能为空」，把这一刻真正该
                // 展示的诊断淹没——这里临时把空列名补成 column_id 再送去编译，不影响真实保存（保存路径
                // finalizeToV4 走真正的空列名诊断阻断，不经此函数）。
                fillEmptyStagingNamesForPreview(
                  stripUnconfiguredOutputs(expressionModelToV4Doc(m, model.value as V4ExtractNodeDocument, adapterContext.value))
                );
            const c = await host.compile(doc);
            const diags = c.targets.flatMap(t => t.diagnostics);
            return {
              valid: c.ok,
              arg_count: 0,
              error: diags.length ? diags.map(d => d.message).join('；') : undefined
            };
          }
        }
      : expressionHost
  );
}

// 字段引用语法：本配置面写的是 V4 表达式，字段引用一律 `[表].[字段]`（服务端 V4 绑定的唯一认可语法，
// 如 `[应收账款].[应收余额] > 0`）。声明后，字段树点选插入的就是该形态；其余未声明的场景恒走花括号，
// 互不影响。
provideRefSyntax('brackets');

// 系统变量的 V4 文本标记：⊞/ƒ 拾取系统变量时按目录 system_variables[].snippet_* 落
// `系统变量.当前时间`（zh）/ `system.current_time`（en）——服务端 V4 绑定只认语言包登记的无引号点路径，
// `[当前时间]` 会被当成裸字段找不到。host_var_id 与编辑器目录的 var_id 同一套 ID（宿主系统变量 ID）。
provideSystemVariableToken((varId: string): string | undefined => {
  const vars = (catalog.value as { extract?: { system_variables?: V4SystemVariableWireLite[] } } | null)?.extract?.system_variables ?? [];
  const hit = vars.find(v => v.host_var_id === varId);
  if (!hit) return undefined;
  const locale = (expressionHost?.locale?.() ?? 'zh-CN').toLowerCase();
  return locale.startsWith('en') ? hit.snippet_en : hit.snippet_zh;
});

const templateId = computed(() => (scrubbed.value ? '' : ((props.input.hostContext.templateId as string) ?? '')));
const nodeId = computed(() => (props.input.props.nodeId as string | undefined) ?? '');

// 写模式判定：单一来源=数据动作种类，由 input.bind（官方动作名 / `operation_kind:<kind>`）或宿主下发的
// hostContext.operationKind 折算（见 actionKind.ts），create/update/delete 各自映射到 ExpressionEditor
// 认识的 WriteOperation 字面量；fetch（或任何无法判定的入参，容错）落空串＝取数模式，与
// ExpressionEditor 的 `writeOp = props.writeOperation ?? ''` 同一约定。
const writeOperation = computed<V4WriteOperation | ''>(() =>
  writeOperationForKind(resolveDataActionKind({ bind: props.input.bind, hostContext: props.input.hostContext }))
);
const isWriteMode = computed(() => writeOperation.value !== '');

// 节点文档模型：写模式与取数模式各自的守卫/空文档不同源（isWriteNodeDocument/emptyWriteNodeDocument
// vs isExtractNodeDocument/emptyExtractNodeDocument），但都遵循同一兜底口径——scrubbed（会话作废）
// 或草稿值形状不合法时兜底空文档，经正向翻译交给 ExpressionEditor 渲染（与壳侧节点配置弹窗的兜底
// 口径一致）。两种文档共用后续 query.sources/joins/filter 段的处理（hostScopeFromDocument/
// adapterContext 等），故这里保留联合类型而不是拆成两个互斥的 computed。
const model = computed<V4ExtractNodeDocument | V4WriteNodeDocument>(() => {
  if (isWriteMode.value) {
    if (scrubbed.value) return emptyWriteNodeDocument(writeOperation.value as V4WriteOperation);
    const raw = props.input.props.modelValue;
    return isWriteNodeDocument(raw) ? normalizeQueryJoins(raw) : emptyWriteNodeDocument(writeOperation.value as V4WriteOperation);
  }
  if (scrubbed.value) return emptyExtractNodeDocument();
  const raw = props.input.props.modelValue;
  return isExtractNodeDocument(raw) ? normalizeQueryJoins(raw) : emptyExtractNodeDocument();
});

/** 入口归一 query.joins：后端旧版生成的单来源节点会把空关联序列化成 `"joins": null`（Go nil 切片），
 *  下游（可读文本还原、正向翻译、定位引用判定）都按数组遍历，遇 null 直接抛错导致整个配置面挂掉。
 *  这里只在非数组时浅拷贝补成空数组，不改动原草稿值；首次编辑写回时自然落成 `[]`。 */
function normalizeQueryJoins<T extends V4ExtractNodeDocument | V4WriteNodeDocument>(doc: T): T {
  if (Array.isArray(doc.query.joins)) return doc;
  return { ...doc, query: { ...doc.query, joins: [] } };
}

function onUpdateModel(doc: V4ExtractNodeDocument | V4WriteNodeDocument): void {
  if (!sessionEditable.value) return;
  (props.input.props.onUpdateModel as ((v: V4ExtractNodeDocument | V4WriteNodeDocument) => void) | undefined)?.(doc);
}

// ── 目录：host 就绪后拉取一次；失败进错误态（catalogError）。────────────────────────────────────
/** V4 目录 system_variables 条目的最小形状（宿主的完整类型不在共享包里，本文件只取三个字段）。 */
interface V4SystemVariableWireLite {
  host_var_id: string;
  snippet_zh: string;
  snippet_en: string;
}

const catalog = shallowRef<V4DemoCatalog | null>(null);
const catalogError = ref('');
async function loadCatalog(): Promise<void> {
  if (!host) return;
  try {
    catalog.value = await host.loadCatalog(false);
    catalogError.value = '';
  } catch (e) {
    catalogError.value = e instanceof Error ? e.message : String(e);
  }
}

// ── 模型往返：V4 文档 ⇄ ExpressionModel ──────────────────────────────────────────────────────
// 权威模型是 V4 节点文档（props.modelValue）；ExpressionEditor 吃的是 ExpressionModel，故每次
// 渲染正向翻译一次、每次编辑反向翻译一次写回。反向翻译以「翻译前的 V4 文档」为底（见共享包
// expressionModelToV4Doc 注释），ExpressionModel 装不下的 V4 独有信息（来源种类、中文别名、逐目标
// 排序、图表目标）原样保留，往返不丢。

/** 表达式可读文本缓存：expression_id → 可读原文。V4 文档存的是 token 化源码（`[标签]#{rN}`），
 *  直接显示是乱码，必须经 host.restore 按当前目录还原（改名后显示新名）。挂载与文档变化时重建。 */
const readableTexts = ref<Record<string, string>>({});

/** 把文档里全部表达式（筛选 + 各关联 ON + 各目标输出）并发 restore 一遍，填进可读文本缓存。
 *  单条失败不影响其余：该条回退 token 化源码本身（宁可显示原样，也不显示空白）。 */
async function refreshReadableTexts(doc: V4ExtractNodeDocument | V4WriteNodeDocument): Promise<void> {
  if (!host?.restore) return;
  const jobs: Array<{ id: string; text: string; refs: unknown[]; purpose: 'filter' | 'join_on' | 'output' }> = [];
  if (doc.query.filter) {
    jobs.push({ id: doc.query.filter.expression_id, text: doc.query.filter.source_text, refs: doc.query.filter.refs, purpose: 'filter' });
  }
  for (const j of doc.query.joins) {
    jobs.push({ id: j.on.expression_id, text: j.on.source_text, refs: j.on.refs, purpose: 'join_on' });
  }
  if (isWriteMode.value) {
    // 写模式：文档没有 targets/staging_target，赋值段（assignments，写模式的④段）承载表达式，
    // 同样是 purpose='output'（写模式输出恒不带 fill_strategy，语义上与取数填充目标输出同源，
    // 见共享包 writeModelAdapter 头注释「两侧结构的对应关系」）。父记录不由本文件写出，故不在作业
    // 清单里。
    const writeDoc = doc as V4WriteNodeDocument;
    for (const a of writeDoc.assignments) {
      jobs.push({ id: a.expression.expression_id, text: a.expression.source_text, refs: a.expression.refs, purpose: 'output' });
    }
  } else {
    const extractDoc = doc as V4ExtractNodeDocument;
    for (const t of extractDoc.targets) {
      for (const o of t.outputs) {
        jobs.push({ id: o.expression.expression_id, text: o.expression.source_text, refs: o.expression.refs, purpose: 'output' });
      }
    }
    // 暂存列表达式（收口会 tokenize 它们，见 finalizeToV4）：不还原的话重开时值格直接显示
    // `[标签]#{rN}` 的 token 形态。写模式无 staging_target（写目标不支持暂存区），此段不适用。
    for (const c of extractDoc.staging_target?.columns ?? []) {
      jobs.push({ id: c.expression.expression_id, text: c.expression.source_text, refs: c.expression.refs, purpose: 'output' });
    }
  }
  const settled = await Promise.all(
    jobs.map(async j => {
      if (!j.text) return [j.id, ''] as const;
      try {
        const r = await host!.restore!({ source_text: j.text, refs: j.refs as never, purpose: j.purpose });
        // 可读文本取 readable_text（去标记、完整显示路径）：r.text 是 token 化形态
        // `[入库时间]#{r1}`，直接显示给用户是乱码、本地轻检查还会把 `{r1}` 当字段标记报错。
        // 老服务端不带 readable_text 时退回 text。
        return [j.id, r.ok ? (r.readable_text ?? r.text) : j.text] as const;
      } catch {
        return [j.id, j.text] as const;
      }
    })
  );
  readableTexts.value = Object.fromEntries(settled);
}

/**
 * 通知汇总：所有 V4 产生的通知信息都在编辑器下方汇总提醒。
 *
 * 收口（finalize）与测试（test）两条链路产生的诊断统一收进这里，经 notices prop 并进编辑器底部校验状态条。
 * 编辑期的表达式语法提示由 ExpressionEditor 自己的底部状态条承担（那是逐表达式的即时反馈），
 * 本区承载的是「整份文档级」的结果——保存收口失败在哪一段、整节点编译报了什么。
 * 每次收口/测试都整份重置：用户改完一处再点一次，看到的必须是当前这一轮的结果，不是历史堆积。
 */
// 不自绘列表，整份交给 ExpressionEditor 的 notices prop——它把这些条目与自己的语法三态合成校验状态条
// 同一条 label（计数 + 首条明细，点击展开全部），形状即共享包导出的 ExpressionNotice。
type SurfaceNotice = ExpressionNotice;
/** 收口（finalize）/测试（test）产生的一次性诊断——每次收口/测试整份重置，见上方注释。裸的 `notices`
 *  名字由下方 computed 占用，合并本层与 staticNotices 一并喂给编辑器。 */
const sessionNotices = ref<SurfaceNotice[]>([]);

/** 暂存区来源（「上一步暂存区」）：壳侧按画布拓扑推导后经 props 传入，本文件原样透传给编辑器的
 *  ①来源段。 */
const stagingSource = computed(
  () => (props.input.props.stagingSource as {
    producerNodeId: string;
    producerLabel: string;
    columns: Array<{ code: string; name: string; type?: string }>;
  } | null | undefined) ?? null
);

/** 视图来源显示名缓存：适配器给视图来源算 V4 显示别名要经 viewLabelOf 查视图名，不接这个解析口时
 *  视图来源的 alias 会是编辑器内部索引 t1——节点摘要、关联设计器、插入文本全显示 t1（数据表来源经
 *  tableLabelOf 查目录 candidates 才有表名）。V4 目录不带视图名，这里经宿主 expressionHost.getViewSchema
 *  （与共享包 useExpressionContext.ensureViewFields 同一来源）按 (app_id, view_id) 异步取一次并缓存；
 *  缓存填入后 adapterContext 随之重算，反向翻译把 alias 换成视图名。查不到 / 旧宿主无该方法时保持
 *  原别名，不阻断。 */
const viewNames = ref<Record<string, string>>({});
const viewNamesPending = new Set<string>();

/** 双向翻译共用的上下文：正反两个方向必须同一份口径，故抽出来只造一次。
 *  fieldLabelOf 供关联 ON 由结构化谓词反生成 token 文本时取字段可读名（查不到回退 field_id）。 */
const adapterContext = computed(() => ({
  templateId: templateId.value,
  appId: (props.input.hostContext.appId as string) ?? '',
  // 视图来源显示别名（见上方 viewNames）。
  viewLabelOf: (viewId: string): string | undefined => viewNames.value[viewId],
  readableTextOf: (id: string, tokenized: string) => readableTexts.value[id] ?? tokenized,
  // 关联 ON 反生成引用时按 alias（=instance_id）反查来源种类，决定产 report_field 还是
  // source_field（见共享包 V4ModelAdapterContext.sourceOf 注释）。
  sourceOf: (alias: string) => model.value.query.sources.find(x => x.instance_id === alias),
  // 上一步暂存区的生产者节点 ID：设计器按画布拓扑算好后随 surface props 传入，反向翻译新增暂存
  // 来源时写进绑定的 producer_node_id（服务端保存校验据此找 producer 读它的暂存输出列）。
  stagingProducerNodeId: stagingSource.value?.producerNodeId,
  // 上一步暂存区的显示名：反向翻译暂存来源的 V4 显示别名（alias）据此重算，与目录来源 tableLabelOf
  // 同一原则（见共享包 V4ModelAdapterContext.stagingProducerLabel 注释）——查不到时（无上一步暂存区）
  // 留空，适配层回退编辑期占位别名。
  stagingProducerLabel: stagingSource.value?.producerLabel,
  // 目录来源的显示别名查询口：与编辑器给来源行 / 字段树 / ⊞ 拾取器用的表名同源
  // （都是数据表 table_name：编辑器经 host.listTables，这里经 V4 目录 candidates[].label），
  // 保证用户看到并插入的 `[表].[字段]` 与服务端作用域里的来源 alias 逐字一致（服务端 V4 绑定解析
  // 设计态文本时表段就按 alias 匹配）。目录未加载 / 候选缺席时返回 undefined，适配层保持原别名。
  // 本报表各表（report.tables，按 table_id）也在这里查：本报表上下文表落 report 绑定时 alias 同样取表名。
  tableLabelOf: (src: { app_id?: string; template_id: string; data_table_id: string }): string | undefined =>
    catalog.value?.candidates?.find(
      c => c.data_table_id === src.data_table_id && (!src.template_id || c.template_id === src.template_id)
    )?.label ?? catalog.value?.report.tables.find(t => t.table_id === src.data_table_id)?.label,
  fieldLabelOf: (instanceId: string, fieldId: string): string => {
    const src = model.value.query.sources.find(s => s.instance_id === instanceId);
    const tableID = src?.kind === 'report' ? src.table_id : src?.kind === 'catalog' ? src.data_table_id : '';
    const fields =
      catalog.value?.report.tables.find(t => t.table_id === tableID)?.fields ??
      catalog.value?.candidates?.find(c => c.data_table_id === tableID)?.fields ??
      [];
    return fields.find(f => f.field_id === fieldId)?.label ?? fieldId;
  }
}));

/** 喂给 ExpressionEditor 的 ExpressionModel（正向翻译，表达式取可读文本）。写/取数两条正向翻译各走
 *  各自的适配器（v4WriteDocToExpressionModel/v4DocToExpressionModel），来源/关联/筛选段内部
 *  仍是同一份纯函数（共享包 v4QueryToModelSource），两者只在目标段（写：operation/target/
 *  assignments/parent/dedupe；取数：targets/chart_targets/staging_target）分道。 */
const expressionModel = computed<ExpressionModel>(() =>
  isWriteMode.value
    ? v4WriteDocToExpressionModel(model.value as V4WriteNodeDocument, adapterContext.value)
    : v4DocToExpressionModel(model.value as V4ExtractNodeDocument, adapterContext.value)
);

/** 编辑器改动回写：翻回 V4 文档后走既有 onUpdateModel 通道（会话不可编辑时由它拦截）。 */
function onExpressionModelUpdate(next: ExpressionModel): void {
  const doc = isWriteMode.value
    ? expressionModelToV4WriteDoc(next, model.value as V4WriteNodeDocument, adapterContext.value)
    : expressionModelToV4Doc(next, model.value as V4ExtractNodeDocument, adapterContext.value);
  onUpdateModel(doc);
}

// 视图来源显示名的异步装载（声明在 onExpressionModelUpdate 之后：名字到位要回调它重转一次）。
watch(
  () =>
    (model.value?.query?.sources ?? []).flatMap((s: V4SourceBinding) =>
      s.kind === 'view' && s.view_id ? [`${s.app_id ?? ''}\u0000${s.view_id}`] : []
    ),
  (keys: string[]) => {
    if (!expressionHost?.getViewSchema) return;
    for (const key of keys) {
      const [appId, viewId] = key.split('\u0000');
      if (!viewId || viewNames.value[viewId] || viewNamesPending.has(key)) continue;
      viewNamesPending.add(key);
      expressionHost
        .getViewSchema(viewId, appId || undefined)
        .then(schema => {
          const name = schema?.view_name?.trim();
          if (!name) return;
          viewNames.value = { ...viewNames.value, [viewId]: name };
          // 名字到位前文档可能已经按 t<n> 转换并保存过一轮（配置行对了、画布摘要与表达式仍是 t1）——
          // 名字一到就用当前编辑器模型重做一次正向翻译，把视图来源 alias 换成视图名。
          onExpressionModelUpdate(expressionModel.value);
        })
        .catch(() => undefined)
        .finally(() => viewNamesPending.delete(key));
    }
  },
  { immediate: true }
);

// ── 写模式静态提醒（同样并入下方通知汇总区）────────────────────────────────────────────────
// 与 sessionNotices（收口/测试的一次性诊断）不同，这两条提醒只要满足条件就常显，不需要用户先点
// 保存/测试——它们描述的是"当前配置在运行期会遇到什么"，越早看到越好。

/** 写目标所属模板 ID（insert 直接读 target.template_id；update/delete 的目标是 query.sources 里
 *  某条来源实例的别名，需要反查该来源的模板归属——report 来源恒属当前模板，catalog 来源按自身
 *  template_id）。非写模式/未选定目标/目标是 staging 来源（不该发生，写目标不支持暂存区）时
 *  返回 undefined。 */
const writeTargetTemplateId = computed<string | undefined>(() => {
  if (!isWriteMode.value) return undefined;
  const doc = model.value as V4WriteNodeDocument;
  if (doc.operation === 'insert') return doc.target.template_id;
  const src = doc.query.sources.find(s => s.instance_id === doc.target.instance_id);
  if (!src) return undefined;
  if (src.kind === 'report') return templateId.value; // 本报表来源恒属当前模板。
  if (src.kind === 'catalog') return src.template_id;
  return undefined;
});

/** 定时任务场景判定：宿主（动作设计器插件）经 hostContext.bandEventKeys 透传当前触发根所属的
 *  事件族分组键，本 surface 不解释其完整取值域（那是设计器侧的分类词表），只按"包含 scheduled
 *  字样"这一宽松判据识别定时类根——缺省 undefined（老宿主未透传）时视为非定时场景，不误报。 */
const isScheduledBandContext = computed(() => {
  const keys = props.input.hostContext.bandEventKeys as string[] | undefined;
  return keys?.some(k => /scheduled/i.test(k)) ?? false;
});

/** 写文档是否引用了本报表字段（refs 里含 kind='report_field'）：定时任务（无表单会话）场景下
 *  本模板字段不可取值，扫筛选/各赋值/父锚定表达式的绑定表，命中一条即判真。 */
const writeDocReferencesReportField = computed(() => {
  if (!isWriteMode.value) return false;
  const doc = model.value as V4WriteNodeDocument;
  const exprs = [doc.query.filter, ...doc.assignments.map(a => a.expression), doc.parent?.kind === 'expression' ? doc.parent.expression : undefined];
  return exprs.some(e => e?.refs?.some(r => r.kind === 'report_field'));
});

/** 静态提醒列表：①写目标属于当前模板——若该记录正被表单会话持有（正在填写），运行期会被宿主表单
 *  数据层的围栏拒绝写入；②定时任务没有表单会话，本模板字段在该场景下不可取值。两条互不排斥，可同时
 *  出现。 */
const staticNotices = computed<SurfaceNotice[]>(() => {
  void localeTick.value; // 强制随宿主语言切换重算文案（st() 本身非响应式，见 localeTick 声明处注释）。
  if (!isWriteMode.value) return [];
  const list: SurfaceNotice[] = [];
  if (writeTargetTemplateId.value && writeTargetTemplateId.value === templateId.value) {
    list.push({ severity: 'warning', message: st('write.surface.current_template_warning') });
  }
  if (isScheduledBandContext.value && writeDocReferencesReportField.value) {
    list.push({ severity: 'warning', message: st('write.surface.scheduled_report_field_warning') });
  }
  return list;
});

/** 喂给编辑器的最终提醒列表：静态提醒在前（常显）、一次性收口/测试诊断在后。 */
const notices = computed<SurfaceNotice[]>(() => [...staticNotices.value, ...sessionNotices.value]);

/** host 缺失时的错误态文案（写/取数各一条 key），经 computed 包一层使其随 localeTick 重算——
 *  模板里直接调用 st() 不会自动随宿主语言切换重渲染（st() 本身非响应式）。 */
const hostMissingMessage = computed(() => {
  void localeTick.value;
  // 键名在中性命名空间 surface.* 下：该文案取数/写模式共用同一份判定逻辑（hostMissing 恒不分模式），
  // 四语同步见 locales/*.json。
  return st(isWriteMode.value ? 'surface.host_missing_write' : 'surface.host_missing_extract');
});

// query.sources 变化、或目录（含"本报表"全部表清单）加载完成，都要重新同步给 host（immediate：
// 挂载即先给一次当前态，不等首次变更/目录到位）。分拣逻辑本体是共享包纯函数 hostScopeFromDocument
// （'catalog' 进 sources 段、'report' 进 report_sources 段、'staging' 走独立的 staging_source 段），
// 不在本文件重复实现；第二参数传目录的 report.tables 做「本报表」兜底合并、第三参数传 stagingSource
// 让暂存来源也随信封下发——不带这段时 diagnose/tokenize/restore 三端点解析不到「暂存表.[列]」，
// 报「当前作用域没有暂存区」。
//
// 🔴 为什么必须把 catalog.value.report.tables 也喂给 hostScopeFromDocument——只按 query.sources
// 分拣时，report_sources 只含"已被某条表达式引用过、已由编辑器 reconcile() 落库"的本报表表；用户在
// 条件编辑器/表达式框里第一次引用某张此前从未用过的本报表表（例如取数目标本身的明细表）时，防抖诊断
// 读到的作用域里还没有它，必报「本报表下找不到数据表」，而「确定」按钮又因诊断失败被禁用——该表本该
// 在确认之后才由 reconcile() 落库，但确认本身先被这次诊断挡死，形成死锁。目录（catalog.value.report.tables）
// 本就是当前模板"本报表"全部表的权威清单（与字段树 ctx.tableFields、tableLabelOf 同源），把它一并喂给
// hostScopeFromDocument 即可让"本报表.[任意表].[字段]"从一开始就可解析，不再依赖是否已被引用过；
// 已被 query.sources 登记的表仍以登记值为准，不受目录覆盖（见该函数实现）。
//
// 🔴 必须声明在下方「重建可读文本」的 watch **之前**：两者都是 immediate，Vue 按声明顺序同步执行；
// restore 请求体在调用当下就读作用域信封，作用域若还没给到 host，首次还原会因「找不到来源实例」失败、
// 退回 token 化源码——表现为打开节点时 ƒ 面板/输出行显示 `[入库时间]#{r1}` 这种乱码，编辑一下才恢复可读。
watch(
  () => [model.value.query.sources, catalog.value] as const,
  () => {
    host?.setScope?.(hostScopeFromDocument(model.value, catalog.value?.report.tables ?? [], stagingSource.value ?? undefined));
  },
  { immediate: true }
);

// 文档变化即重建可读文本（immediate：挂载就先还原一次，不等首次编辑）。声明顺序见上方 setScope 注释。
watch(() => model.value, doc => void refreshReadableTexts(doc), { immediate: true, deep: false });

function applyInteractiveSessionSnapshot(snapshot: Readonly<InteractiveSessionSnapshot>): void {
  // auth_ended 是不可逆终态：先切换 scrubbed，使 model/template 同步清空，再由响应式渲染发布只读状态。
  // lost 只冻结写入，保留既有模型供人工查看。
  if (snapshot.state === 'auth_ended') scrubbed.value = true;
  sessionSnapshot.value = snapshot;
}

onMounted(async () => {
  stopSessionChange = interactiveSession.onChange(applyInteractiveSessionSnapshot);
  // setup 首读与 onMounted 订阅之间可能已经发生 auth_ended；订阅完成后必须立即重读当前快照，
  // 否则终态不会再发布第二次，组件会永久停留在旧 active 快照并继续接受写入。
  applyInteractiveSessionSnapshot(interactiveSession.snapshot);
  host?.setNodeId?.(nodeId.value);
  stopLocaleChange = surfaceI18n.onChange(() => {
    localeTick.value += 1;
  });
  await loadCatalog();
});

onBeforeUnmount(() => {
  // 嵌套 action-config 不拥有会话，绝不 close；父 action-designer 统一收尾。
  stopSessionChange?.();
  stopLocaleChange?.();
  surfaceI18n.dispose();
});

// 壳（动作设计器插件的节点配置弹窗）经 ref + defineExpose 以 duck-typing 拿到本组件实例——下方
// defineExpose 的 finalize/test 即该通道；收口实现见本文件的 finalizeToV4。

/** 一条诊断的展示消息：兼容 finalize() 的 V4NodeDocumentDiagnosticItem（{source, diagnostic}）与
 *  compile() 的裸 V4Diagnostic 两种形状（见 test() 实现——两条分支的诊断来源不同）。 */
function diagnosticMessage(d: V4NodeDocumentDiagnosticItem | V4Diagnostic): string {
  return 'diagnostic' in d ? d.diagnostic.message : d.message;
}

/** 收口前剔除表达式为空的赋值行（写模式）：同取数节点 stripUnconfiguredOutputs 的初衷（见共享包
 *  该函数注释）——编辑期允许用户先选目标字段、还没来得及配表达式，此时不应以 source_text:'' 落进
 *  草稿，否则会被服务端解码校验整份拒绝；写文档没有 targets/staging_target，故不能直接复用取数专属
 *  的那个函数（类型也不兼容），另起一份最小实现。共享包内判空的同类逻辑作用于 ExpressionModel 层的
 *  ValueRef（{kind,value,text,...}），而本函数操作的是反向翻译后的 V4WriteNodeDocument 赋值行（表达式
 *  sidecar 形状是 {expression_id, purpose, source_text, refs}），两者结构不同源，故保留本地判据。 */
/*  唯一键防重：dedupe.field_ids 须 ⊆ 剔除后的 assignments[].target_field_id（共享包 ExpressionEditor
 *  写模式收口同一约束、同一收口时机）——本函数已把「表达式为空的赋值行」剔掉，若防重键里恰好引用了
 *  被剔的字段，须同步从 dedupe.field_ids 摘除；摘完一个不剩就整个 dedupe 段删掉（服务端 dedupe 段
 *  不允许 field_ids 为空数组）。 */
function stripUnconfiguredWriteAssignments(doc: V4WriteNodeDocument): V4WriteNodeDocument {
  const assignments = doc.assignments.filter(a => (a.expression?.source_text ?? '').trim() !== '');
  const next: V4WriteNodeDocument = { ...doc, assignments };
  if (next.dedupe) {
    const kept = new Set(assignments.map(a => a.target_field_id));
    const fieldIds = next.dedupe.field_ids.filter(id => kept.has(id));
    if (fieldIds.length > 0) {
      next.dedupe = { ...next.dedupe, field_ids: fieldIds };
    } else {
      delete next.dedupe;
    }
  }
  return next;
}

// ── 写模式「目标模板主表是否被引用」判定 ─────────────────────────────────────────────────────
// 供 noticeParentUnconstrained 的只读诊断使用：insert 到扩展表时，父记录恒取自查询结果里目标模板
// 主表（隐含来源）的记录，本节只判「定位条件是否引用了它」，不改写文档、不产出任何 parent 字段。

/** 某来源实例是否指向给定模板下的给定表：catalog 来源按 data_table_id + template_id 匹配；
 *  report 来源恒属当前模板（该类型本身没有 template_id 字段，故不比较）按 table_id 匹配。 */
function sourceMatchesTable(src: V4SourceBinding, tableId: string, tplId: string): boolean {
  if (src.kind === 'catalog') return src.data_table_id === tableId && src.template_id === tplId;
  if (src.kind === 'report') return src.table_id === tableId;
  return false;
}

/** 按模板 ID 缓存「该模板主表身份」的解析结果（同节点内多次收口/测试复用，不重复请求）。
 *  本缓存及其唯一读者 resolveTemplateMainTable 只用来查「展示名」，不参与「诊断是否触发」的判定——
 *  即使查不到/查失败，诊断依旧按 doc.query.sources 的结构性判据触发，只是展示名退化成来源别名
 *  （见下方 noticeParentUnconstrained 内的调用处）。 */
const templateMainTableCache = new Map<string, Promise<{ table_id: string; label: string } | undefined>>();

/**
 * 解析某模板的主表身份 + 显示名：优先用已加载的目录快照（catalog.value.report.tables，只有目标
 * 模板 = 当前模板时才命中，零网络）；命中失败（跨模板写目标，或目录尚未把该模板的表带出）时回退
 * 宿主既有 ExpressionHost.listTables 端点按模板查（不新增端点）。两者都拿不到时返回 undefined——
 * **仅影响展示名是否好看**，不影响诊断本身是否产出（诊断由 noticeParentUnconstrained 按来源段结构性
 * 判定，见其头注释）。
 */
function resolveTemplateMainTable(tplId: string): Promise<{ table_id: string; label: string } | undefined> {
  const cached = templateMainTableCache.get(tplId);
  if (cached) return cached;
  const promise = (async () => {
    if (tplId === templateId.value) {
      const hit = catalog.value?.report.tables.find(t => t.role === 'main');
      if (hit) return { table_id: hit.table_id, label: hit.label };
    }
    if (!expressionHost) return undefined;
    try {
      const tables = await expressionHost.listTables(tplId);
      const hit = tables.find(t => t.table_role === 'main');
      return hit ? { table_id: hit.data_table_id, label: hit.table_name } : undefined;
    } catch {
      return undefined; // 跨模板拉取失败（权限/网络）：展示名退化，不影响诊断本身。
    }
  })();
  templateMainTableCache.set(tplId, promise);
  return promise;
}

/** 按「模板 ID + 表 ID」缓存写目标表显示名的解析结果（同节点内多次收口/测试复用，见
 *  resolveWriteTargetTableLabel）。 */
const writeTargetTableLabelCache = new Map<string, Promise<string | undefined>>();

/**
 * 解析写目标表（doc.target.data_table_id/template_id）的显示名——供 noticeWhereUnconstrained
 * 的 info 级只读提示展示「将修改/删除哪张表」用。
 * 与 resolveTemplateMainTable 同一手法：目标模板 = 当前设计模板时优先查已加载目录（零网络，
 * report.tables 与 candidates 两处都查一遍，因为目标表未必是"主表"）；跨模板/未命中时回退
 * 宿主既有 ExpressionHost.listTables 端点按模板查。两者都拿不到时返回 undefined——仅影响
 * 展示名是否好看，调用处退回表 ID 本身，不影响提示本身是否产出。
 */
function resolveWriteTargetTableLabel(tplId: string, tableId: string): Promise<string | undefined> {
  const cacheKey = `${tplId}::${tableId}`;
  const cached = writeTargetTableLabelCache.get(cacheKey);
  if (cached) return cached;
  const promise = (async () => {
    if (tplId === templateId.value) {
      const hit =
        catalog.value?.report.tables.find(t => t.table_id === tableId) ??
        catalog.value?.candidates?.find(c => c.data_table_id === tableId && c.template_id === tplId);
      if (hit) return hit.label;
    }
    if (!expressionHost) return undefined;
    try {
      const tables = await expressionHost.listTables(tplId);
      return tables.find(t => t.data_table_id === tableId)?.table_name;
    } catch {
      return undefined; // 跨模板拉取失败（权限/网络）：展示名退化，不影响提示本身。
    }
  })();
  writeTargetTableLabelCache.set(cacheKey, promise);
  return promise;
}

/** 定位条件（筛选 + 关联 ON）里是否引用了给定的来源实例集合中的任意一个。 */
function anyInstanceReferenced(doc: V4WriteNodeDocument, instanceIds: Set<string>): boolean {
  if (instanceIds.size === 0) return false;
  const exprs = [doc.query.filter, ...doc.query.joins.map(j => j.on)];
  return exprs.some(e =>
    e?.refs?.some(r => (r.kind === 'source_field' || r.kind === 'report_field') && r.source_instance_id && instanceIds.has(r.source_instance_id))
  );
}

/**
 * info 级只读诊断：insert 目标是扩展表、且定位条件（筛选 + 关联 ON）都没有引用「目标模板主表」的
 * 来源实例时，运行期语义是"目标主表全部记录各得一份新增行"——不阻断保存，只把诊断并入 diagnostics
 * （供 finalizeWriteV4 汇总进 sessionNotices，在「测试」结果与保存收口通知里展示）。
 * 目标就是主表本身、或找不到主表相关来源时，静默跳过。
 *
 * 「诊断触不触发」只按来源段结构性判定，不依赖额外的宿主往返：目标模板主表按「目标先行」的构造方式
 * （共享包 ExpressionEditor 的写目标来源对账，push 新来源时 role 恒按"是否为目标表自身"二选一定为
 * main）已经作为「隐含来源」落进了 doc.query.sources——同一目标模板里，除目标表自身外只会有它的配对
 * 主表这一种可能，直接从来源段识别即可。
 * - 目标模板 = 当前设计模板：主表身份从已加载目录（catalog.value.report.tables）取，零网络。
 * - 目标模板 ≠ 当前设计模板：直接在 doc.query.sources 里找「与目标同模板、但不是目标表本身」的
 *   catalog 来源——找到即视为主表实例，判定是否被引用；listTables 查询只影响提示文案里的表名是否
 *   好看（resolveTemplateMainTable，查不到退回来源别名），不决定诊断是否产出。
 */
async function noticeParentUnconstrained(doc: V4WriteNodeDocument, diagnostics: V4NodeDocumentDiagnosticItem[]): Promise<void> {
  const tplId = doc.target.template_id ?? '';
  const tableId = doc.target.data_table_id ?? '';
  if (!tplId || !tableId) return; // 目标未选（校验②已报）：本诊断无从谈起。

  let mainTableId: string | undefined;
  let mainTableLabel: string | undefined;
  if (tplId === templateId.value) {
    const hit = catalog.value?.report.tables.find(t => t.role === 'main');
    if (!hit) return; // 目录未加载/无主表：拿不到元数据，静默跳过。
    mainTableId = hit.table_id;
    mainTableLabel = hit.label;
  } else {
    // 跨模板：来源段里「与目标同模板、非目标表自身」的 catalog 来源即隐含主表来源（见头注释），
    // 不存在即说明目标本身就是主表（无需锚定）或隐含来源尚未注入——两种情况都静默跳过。
    // 🔴 用 for/if 而非 Array.find(predicate) 手写窄化：find 的回调不是类型谓词（无 `s is X`），
    // TS 不会把返回值窄化成 V4SourceBindingCatalog，直接 find 拿到的仍是联合类型 V4SourceBinding，
    // 取不到 data_table_id/alias 两个 catalog 专属字段；if 分支内的判别式收窄则是 TS 认可的窄化点。
    let parent: V4SourceBindingCatalog | undefined;
    for (const s of doc.query.sources) {
      if (s.kind === 'catalog' && s.template_id === tplId && s.data_table_id !== tableId) {
        parent = s;
        break;
      }
    }
    if (!parent) return;
    mainTableId = parent.data_table_id;
    mainTableLabel = (await resolveTemplateMainTable(tplId))?.label ?? parent.alias;
  }
  if (mainTableId === tableId) return; // 目标就是主表本身。

  const mainInstanceIds = new Set(doc.query.sources.filter(s => sourceMatchesTable(s, mainTableId!, tplId)).map(s => s.instance_id));
  if (anyInstanceReferenced(doc, mainInstanceIds)) return;

  diagnostics.push({
    source: st('write.surface.where.filter'),
    diagnostic: {
      code: 'WRITE_V4_PARENT_UNCONSTRAINED',
      phase: 'finalize',
      severity: 'info',
      message: st('write.surface.parent_unconstrained', { table: mainTableLabel })
    }
  });
}

/**
 * info 级只读诊断：修改/删除不要求定位条件——不写条件 = 修改/删除目标表全部行，合法。update/delete
 * 的定位条件（筛选）为空时，只提示用户运行期会命中目标表全部记录，不阻断保存——与
 * noticeParentUnconstrained 同一「不阻断，只汇总进通知区」的处理方式。目标未选
 * （target.data_table_id 为空，校验②已报 WRITE_TARGET_REQUIRED）时静默跳过，避免展示名为空的
 * 重复噪音。展示名解析手法与 noticeParentUnconstrained 同款，见 resolveWriteTargetTableLabel。
 */
async function noticeWhereUnconstrained(doc: V4WriteNodeDocument, diagnostics: V4NodeDocumentDiagnosticItem[]): Promise<void> {
  if (doc.operation !== 'update' && doc.operation !== 'delete') return;
  if ((doc.query.filter?.source_text ?? '').trim()) return; // 已配置定位条件，不提示。
  const tableId = doc.target.data_table_id ?? '';
  if (!tableId) return; // 目标未选：校验②已报 WRITE_TARGET_REQUIRED，本提示无展示名可用，静默跳过。
  const tplId = doc.target.template_id ?? templateId.value;
  const tableLabel = (await resolveWriteTargetTableLabel(tplId, tableId)) ?? tableId;
  diagnostics.push({
    source: st('write.surface.where.filter'),
    diagnostic: {
      code: 'WRITE_WHERE_UNCONSTRAINED',
      phase: 'finalize',
      severity: 'info',
      message: st('write.surface.where_unconstrained', { table: tableLabel })
    }
  });
}

/**
 * 保存前收口（写模式）：与取数节点同构的 tokenize 流程（可读文本 → token 化源码 + 稳定引用绑定表，
 * 见下方 finalizeExtractV4 头注释的详细说明，两者共用同一套 tokenizeInPlace 语义），额外叠加两条
 * 写模式专属的客户端校验 + 两条 info 级只读诊断——深层结构完整性仍由宿主保存校验兜底，这里只做
 * "用户能立刻看懂、不必等一次保存往返"的早期拦截（与服务端写动作业务约束对等）：
 * ①insert 的赋值（assignments）不能为空——没有要写的字段，新增没有意义；
 * ②三操作统一判「写入目标未选」——target.data_table_id 为空即报 WRITE_TARGET_REQUIRED。父记录不是
 *   用户可配的独立项，恒取自查询结果里目标模板主表（隐含来源）的记录。
 * insert 目标是扩展表时另有一条 info 级只读诊断（noticeParentUnconstrained）：定位条件（筛选 + 关联
 * ON）都没有引用「目标模板主表」的来源实例时，运行期语义是"目标主表全部记录各得一份新增行"——只提示、
 * 不阻断保存。
 * update/delete 的定位条件（筛选）可空（空筛选在写核编译器里等价"命中全部未删行"，即修改/删除目标表
 * 全部行，合法），只产出 info 级只读诊断 WRITE_WHERE_UNCONSTRAINED（noticeWhereUnconstrained），
 * 不阻断保存。
 */
async function finalizeWriteV4(): Promise<{ ok: boolean; doc: V4WriteNodeDocument; diagnostics: V4NodeDocumentDiagnosticItem[] }> {
  const doc = stripUnconfiguredWriteAssignments(JSON.parse(JSON.stringify(model.value)) as V4WriteNodeDocument);
  const diagnostics: V4NodeDocumentDiagnosticItem[] = [];
  if (typeof host?.tokenize !== 'function') {
    return {
      ok: false,
      doc,
      diagnostics: [
        {
          source: 'host',
          diagnostic: {
            code: 'HOST_TOKENIZE_MISSING',
            phase: 'host',
            severity: 'error',
            // 与 finalizeExtractV4 的同款兜底共用中性键 surface.host_tokenize_missing。
            message: st('surface.host_tokenize_missing')
          }
        }
      ]
    };
  }
  // purpose 类型取宽为 V4Purpose：doc.query.filter/assignments[].expression 等字段的声明类型即
  // V4Purpose（含本文件不产出的 'condition' 变体），host.tokenize 的 V4TokenizeRequest.purpose 也是
  // V4Purpose，不改变任何运行期行为。
  async function tokenizeInPlace(expr: { expression_id: string; purpose: V4Purpose; source_text: string; refs: unknown[] }, where: string): Promise<void> {
    if (!expr.source_text) return; // 空表达式不下发绑定请求（空即未配置，不是错误）。
    const r = await host!.tokenize({ text: expr.source_text, purpose: expr.purpose, refs: (expr.refs ?? []) as V4ExpressionRefWire[] });
    for (const d of r.diagnostics ?? []) diagnostics.push({ source: where, diagnostic: d });
    if (!r.ok) return; // 绑定失败：保留用户原文不覆盖，诊断已收集。
    expr.source_text = r.source_text;
    expr.refs = r.refs as never;
  }

  // 客户端校验①：insert 赋值不能为空（stripUnconfiguredWriteAssignments 已剔除表达式为空的行，
  // 这里只需看剩余行数是否为零）。文案经本插件 i18n（st()）取，带行动指引（不只说「缺什么」，还说
  // 「去哪儿补」），四语同步见 locales/*.json。
  if (doc.operation === 'insert' && doc.assignments.length === 0) {
    diagnostics.push({
      source: st('write.surface.where.assignments'),
      diagnostic: { code: 'WRITE_ASSIGNMENTS_REQUIRED', phase: 'host', severity: 'error', message: st('write.surface.assignments_required') }
    });
  }

  // 客户端校验②：三操作统一判「写入目标未选」——target.data_table_id 为空即报（见函数头注释）。
  // target.instance_id 仅作历史读兼容：只落了 instance_id、未落 data_table_id/template_id 的存量草稿
  // 会被本校验报出，本文件内不做兼容折算。
  if (!(doc.target.data_table_id ?? '').trim()) {
    diagnostics.push({
      source: st('write.surface.where.target'),
      diagnostic: { code: 'WRITE_TARGET_REQUIRED', phase: 'host', severity: 'error', message: st('write.surface.target_required') }
    });
  }

  if (doc.query.filter) await tokenizeInPlace(doc.query.filter, st('write.surface.where.filter'));
  for (const a of doc.assignments) await tokenizeInPlace(a.expression, `${st('write.surface.where.assignments')} ${a.target_field_id}`);
  // parent 不由本文件写出、也不在这里收口：存量 parent.kind='expression' 文档由宿主保存校验具名拒绝。

  // info 级只读诊断：insert 到扩展表且定位条件未引用目标模板主表时，提示运行期会对该主表的每条记录
  // 各填充一份——不阻断保存，只并入通知汇总区。必须在筛选 tokenize 之后调用（依赖
  // doc.query.filter.refs 已绑定，关联 ON 的 refs 本就在结构化关联 UI 产出时绑定好，见
  // finalizeExtractV4 内「关联 ON 不参与 tokenize」的同款说明）。
  if (doc.operation === 'insert') {
    await noticeParentUnconstrained(doc, diagnostics);
  }
  // info 级只读诊断：update/delete 定位条件为空时提示将命中目标表全部记录，不阻断保存——判据只看
  // source_text 是否为空，不依赖 tokenize 后的 refs，调用顺序无关，放在这里只为与上方
  // noticeParentUnconstrained 的「两条 info 提示相邻」排布一致。
  await noticeWhereUnconstrained(doc, diagnostics);

  const ok = !diagnostics.some(d => d.diagnostic.severity === 'error');
  // 整份重置汇总区的"一次性"层（静态提醒层 staticNotices 不受影响，见其注释）。
  sessionNotices.value = diagnostics.map(d => ({
    severity: d.diagnostic.severity === 'warning' || d.diagnostic.severity === 'info' ? d.diagnostic.severity : 'error',
    message: d.diagnostic.message,
    where: d.source && d.source !== 'host' ? d.source : undefined
  }));
  if (ok) onUpdateModel(doc);
  return { ok, doc, diagnostics };
}

/**
 * 保存前收口（取数模式）：把文档里各表达式的「可读原文」tokenize 成 token 化源码 + 稳定引用绑定表。
 *
 * ExpressionEditor 只管把用户改动写进模型（表达式以可读文本承载），token 化是 V4 链路自己的事，
 * 故收口在本文件完成。逐条 tokenize、诊断按出处归集；任一条出 error 即整体 ok=false，但仍返回已尽力
 * 收口的文档，供壳侧展示诊断而不丢用户输入。
 *
 * 🔴 文档里的表达式不恒是「可读文本」：节点刚重开、用户未编辑任何一条时，`model.value` 仍是
 * refreshReadableTexts 还原前的存档态——source_text 是 `[标签]#{rN}` token 化源码、refs 是非空
 * 绑定表（还原结果只落进 readableTexts 显示缓存，不回写 model）。此时若 tokenize 请求丢了 refs，
 * 服务端按「有稳定标记则完全依赖 input.Refs」的规则找不到绑定，报「未提供稳定引用 #{r1} 的绑定」。
 * 故 tokenizeInPlace 必须原样带上 expr.refs：可读文本场景 refs 为空、服务端走文本路径解析；
 * 存档态场景 refs 非空、服务端按 refs 反查并刷新 last_label——两种形态都能收口，服务端对已是
 * `#{rN}` 形态的出现保留原 ID、幂等。
 */
async function finalizeExtractV4(): Promise<V4NodeDocumentFinalizeResult> {
  // 收口 = 送往服务端的边界：先剔除编辑期保留的占位输出行，否则它们会以 source_text:'' 落进草稿、
  // 在「保存当前事件」时被服务端解码校验整份拒绝。
  const doc = stripUnconfiguredOutputs(JSON.parse(JSON.stringify(model.value)) as V4ExtractNodeDocument);
  const diagnostics: V4NodeDocumentDiagnosticItem[] = [];
  // 出处标签统一经 st() 取一次，供本函数内多处诊断复用（含下方 noticeAutoRowMatchKeys 闭包内的引用）。
  const filterSourceLabel = st('surface.extract.where.filter');
  const stagingSourceLabel = st('surface.extract.staging.label');
  if (typeof host?.tokenize !== 'function') {
    return {
      ok: false,
      doc,
      diagnostics: [
        {
          source: 'host',
          diagnostic: {
            code: 'HOST_TOKENIZE_MISSING',
            phase: 'host',
            severity: 'error',
            // 与 finalizeWriteV4 的同款兜底共用中性键 surface.host_tokenize_missing。
            message: st('surface.host_tokenize_missing')
          }
        }
      ]
    };
  }
  // purpose 类型取宽为 V4Purpose：与 finalizeWriteV4 内同名函数同一理由，不改变任何运行期行为。
  async function tokenizeInPlace(expr: { expression_id: string; purpose: V4Purpose; source_text: string; refs: unknown[] }, where: string): Promise<void> {
    if (!expr.source_text) return; // 空表达式不下发绑定请求（空即未配置，不是错误）。
    // refs 原样带上（见本函数头注释「存档态」段）：可读文本场景 expr.refs 为空数组，服务端按纯文本
    // 路径解析；未经编辑的存档态场景 expr.refs 非空，服务端按稳定引用反查绑定、刷新 last_label——
    // 两种形态都收口，不能只支持前者。
    const r = await host!.tokenize({ text: expr.source_text, purpose: expr.purpose, refs: (expr.refs ?? []) as V4ExpressionRefWire[] });
    for (const d of r.diagnostics ?? []) diagnostics.push({ source: where, diagnostic: d });
    if (!r.ok) return; // 绑定失败：保留用户原文不覆盖，诊断已收集。
    expr.source_text = r.source_text;
    expr.refs = r.refs as never;
  }
  /**
   * 自动按行列匹配——**只读预览**（抽键是生成 IR 和运行期的行为，不应影响用户的设置）。权威在宿主
   * 后端：展开期从筛选里「来源字段 = 本报表明细字段」等式派生匹配键，等式保留在筛选里继续作为 WHERE
   * 生效，文档零改写。这里只调 diagnose 拿 token 化筛选文本的 AST，按同一口径识别等式，把「执行时哪些
   * 等式会作为匹配键」以 info 收进通知汇总区。绝不改 doc、不重新 tokenize；diagnose 失败/AST 缺席
   * 一律静默跳过——筛选本身已由上一步 tokenize 诊断过，这里不重复报错，也不让预览失败阻断收口。
   */
  async function noticeAutoRowMatchKeys(target: V4ExtractNodeDocument): Promise<void> {
    const filter = target.query.filter;
    if (!filter?.source_text || typeof host?.diagnose !== 'function') return;
    // 目标级「自动按行列匹配」开关：缺省即开（与服务端 FillTarget.AutoRowMatchEnabled 同口径：nil/true=开），
    // 显式 false 关；主表结构性禁匹配不看。
    const detailTargets = target.targets.filter(t => t.table_role !== 'main' && t.auto_row_match !== false);
    if (detailTargets.length === 0) return;
    const detailTables = new Set(detailTargets.map(t => t.target_data_table_id));
    // tokenize 回带的绑定表 → diagnose 的引用描述符（system_var 不经 #{rN} 引用表，跳过）。
    const diagnoseRefs = (filter.refs ?? [])
      .filter(r => r.kind !== 'system_var')
      .map(r => ({
        ref_id: r.ref_id,
        kind: r.kind as 'source_field' | 'report_field' | 'staging_field',
        source_instance_id: r.source_instance_id,
        table_id: r.table_id,
        field_id: r.field_id,
        column_id: r.staging_column_id
      }));
    let ast: FilterAstNode | null = null;
    try {
      const res = await host.diagnose({ text: filter.source_text, purpose: 'filter', refs: diagnoseRefs });
      if (res?.ok && res.ast && typeof res.ast === 'object') ast = res.ast as FilterAstNode;
    } catch {
      return;
    }
    for (const m of previewAutoRowMatchKeys(filter.source_text, filter.refs ?? [], ast, detailTables)) {
      // 服务端同口径：该字段已由用户手动配成「按行列匹配」输出时不再派生（手动声明优先），预览也不报。
      const manual = detailTargets.some(
        t => t.target_data_table_id === m.targetTableId && t.outputs.some(o => o.target_field_id === m.targetFieldId && o.fill_strategy === 'row_match')
      );
      if (manual) continue;
      diagnostics.push({
        source: filterSourceLabel,
        diagnostic: {
          code: 'EXTRACT_AUTO_ROW_MATCH_KEY',
          phase: 'finalize',
          severity: 'info',
          message: st('surface.extract.auto_row_match_notice', { source: m.sourceRef.last_label, report: m.reportRef.last_label })
        }
      });
    }
  }
  // 暂存列名收口校验：stripUnconfiguredOutputs 已把表达式为空白的列剔除（输出表达式未设置的列自动
  // 删除），剔除优先——同一列若既没起名也没配表达式，走到这里时已经不在 doc.staging_target.columns 里了，
  // 不会被误判成「空列名」。这里只管剩下的、真正配了表达式却漏了列名的情况：列名（trim 后）为空则不允许
  // 保存（服务端解码校验已有「name 不能为空」兜底，这里在客户端先给可读诊断、阻断保存回写）；列名与
  // 前面某列重复——服务端保存校验同样拒绝，客户端诊断能指出是哪一列。序号 N 从 1 计、按 columns 数组
  // 顺序，与暂存区③目标 tab 的列表顺序一致。
  const stagingColumns = doc.staging_target?.columns ?? [];
  const seenStagingNames = new Map<string, number>(); // 列名（trim）→ 首次出现的序号（1 起）
  stagingColumns.forEach((col, i) => {
    const idx = i + 1;
    const name = col.name.trim();
    if (!name) {
      diagnostics.push({
        source: stagingSourceLabel,
        diagnostic: {
          code: 'STAGING_COLUMN_NAME_EMPTY',
          phase: 'host',
          severity: 'error',
          message: st('surface.extract.staging.name_empty', { index: idx })
        }
      });
      return;
    }
    const firstIdx = seenStagingNames.get(name);
    if (firstIdx) {
      diagnostics.push({
        source: stagingSourceLabel,
        diagnostic: {
          code: 'STAGING_COLUMN_NAME_DUPLICATE',
          phase: 'host',
          severity: 'error',
          message: st('surface.extract.staging.name_duplicate', { index: idx, name, firstIndex: firstIdx })
        }
      });
    } else {
      seenStagingNames.set(name, idx);
    }
  });
  if (doc.query.filter) await tokenizeInPlace(doc.query.filter, filterSourceLabel);
  // 自动按行列匹配只读预览：在筛选 tokenize 之后做（等式两侧身份靠回带的 refs 反查），只在筛选本身
  // 绑定成功时做；不改 doc、不重新 tokenize、不影响随后输出行的绑定。
  if (doc.query.filter && !diagnostics.some(d => d.source === filterSourceLabel && d.diagnostic.severity === 'error')) {
    await noticeAutoRowMatchKeys(doc);
  }
  // 关联 ON 不参与 tokenize：它由编辑器的结构化关联 UI 编辑，经 predicatesToJoinOn 直接产出
  // token 化源码 + 绑定表（结构化谓词里的 instance_id/field_id 已是绑定所需全部信息）。再走一遍
  // tokenize 会把已 token 化的文本当可读原文重新解析，轻则多一次往返、重则因标签重名而绑错。
  // 判据见共享包 expressionModelAdapter.predicatesToJoinOn 的头注释。
  for (const t of doc.targets) {
    for (const o of t.outputs) await tokenizeInPlace(o.expression, st('surface.extract.where.output', { field: o.target_field_id }));
  }
  // 暂存列表达式与 targets 输出行同一 'output' 口径 tokenize，否则暂存列表达式里的引用（如 [表].[字段]）
  // 永远绑不到稳定 #{rN}，服务端只能按纯文本解析，改名/重排来源表时无法感知引用漂移。where 用列显示名，
  // 缺省回落 column_id 便于诊断定位。
  for (const col of doc.staging_target?.columns ?? []) {
    await tokenizeInPlace(col.expression, st('surface.extract.where.staging_column', { name: col.name || col.column_id || '' }));
  }
  const ok = !diagnostics.some(d => d.diagnostic.severity === 'error');
  // 整份重置汇总区的"一次性"层（见 sessionNotices 注释）：成功时清空，失败时逐条带出处。
  sessionNotices.value = diagnostics.map(d => ({
    severity: d.diagnostic.severity === 'warning' || d.diagnostic.severity === 'info' ? d.diagnostic.severity : 'error',
    message: d.diagnostic.message,
    where: d.source && d.source !== 'host' ? d.source : undefined
  }));
  if (ok) onUpdateModel(doc); // 收口成功才回写：失败时不用半绑定的文档覆盖用户输入。
  return { ok, doc, diagnostics };
}

/** finalize/test 暴露给壳侧的统一结果形状：doc 按当前模式二选一，`
 *  V4NodeDocumentFinalizeResult`（取数专属，doc 严格为 V4ExtractNodeDocument）与写模式的
 *  {ok, doc: V4WriteNodeDocument, diagnostics} 都结构性满足本类型，故 finalizeExtractV4/
 *  finalizeWriteV4 两者的返回值都能直接赋给它，不需要额外包装。 */
type SurfaceFinalizeResult = { ok: boolean; doc: V4ExtractNodeDocument | V4WriteNodeDocument; diagnostics: V4NodeDocumentDiagnosticItem[] };

/** 收口分发：写模式与取数模式各走各自的收口实现，产出形状统一为 SurfaceFinalizeResult，供
 *  defineExpose 的 finalize/test 统一消费，调用方（壳侧节点配置弹窗）不需要关心当前是哪种模式。 */
async function finalizeToV4(): Promise<SurfaceFinalizeResult> {
  return isWriteMode.value ? finalizeWriteV4() : finalizeExtractV4();
}

// 来源选择器选项：共享包 sourcePickerOptions 缺省只允许 { selectableKinds: ['table'] }，本配置面显式
// 打开视图、外部数据表与跨应用（kind=view / kind=external 都是 V4 正式来源形态），供①来源段「＋添加」
// 使用。仅作用于 ExpressionEditor 内部①来源段的 SourceTreePicker（取数来源表 / 写动作的关联来源表二者
// 共用同一挂载点，见模板注释）；写目标段是另一个独立 SourceTreePicker（内部固定 selectableKinds=['table']、
// 不接 sourcePickerOptions），不受本处影响，仍然只能选可写数据表。external 的具体业务逻辑（信封装配/
// 正反向适配）全在共享包 @kimposoft/expression-editor 侧，插件只开这一个开关。
const sourcePickerOptions: NonNullable<ExpressionEditorProps['sourcePickerOptions']> = {
  selectableKinds: ['table', 'view', 'external'],
  allowCrossApp: true
};

defineExpose({
  // finalize 内部会同步 emit update:modelValue → 本文件 onUpdateModel → 壳的 setExpressionModel
  // 已把结果写回 draft；这里再显式返回一次，供壳侧不依赖事件时序、直接拿到收口后的结果。
  // 会话不可编辑（只读/已回收）时不下发收口动作，直接返回 undefined。
  finalize: async (): Promise<SurfaceFinalizeResult | undefined> => {
    if (!sessionEditable.value) return undefined;
    return finalizeToV4();
  },
  // test：先收口（finalize），收口失败直接汇总其诊断；收口成功再拿收口后的文档整节点编译
  // （host.compile，接口按 V4ExtractNodeDocument | V4WriteNodeDocument 统一签名，见共享包
  // AliceV4Host.compile），把各目标诊断汇总返回。不下发编译动作时（会话不可编辑/host 缺失）视为失败。
  test: async (): Promise<{ ok: boolean; diagnostics: string[] }> => {
    if (!sessionEditable.value || !host) return { ok: false, diagnostics: [] };
    const r = await finalizeToV4();
    if (!r) return { ok: false, diagnostics: [] };
    if (!r.ok) return { ok: false, diagnostics: r.diagnostics.map(diagnosticMessage) };
    const c = await host.compile(r.doc);
    const compiled = c.targets.flatMap(t => t.diagnostics);
    // 编译诊断追加进"一次性"汇总层（finalize 已把收口诊断写进去，这里是同一轮的第二段结果；
    // 静态提醒层 staticNotices 不受影响，随 notices computed 一起并入最终展示列表）。
    sessionNotices.value = [
      ...sessionNotices.value,
      ...compiled.map(d => ({
        severity: (d.severity === 'warning' || d.severity === 'info' ? d.severity : 'error') as SurfaceNotice['severity'],
        message: d.message,
        where: '编译'
      }))
    ];
    if (c.ok && !compiled.length) {
      // 测试通过文案按 isWriteMode 分叉（surface.test.success_extract/success_write）。追加而不是整份
      // 覆盖：保留 finalize 阶段已产出的 info 级只读诊断（如 WRITE_V4_PARENT_UNCONSTRAINED），否则
      // 「测试」结果只看得到「测试通过」一行。
      sessionNotices.value = [...sessionNotices.value, { severity: 'info', message: st(isWriteMode.value ? 'surface.test.success_write' : 'surface.test.success_extract') }];
    }
    return { ok: c.ok, diagnostics: compiled.map(diagnosticMessage) };
  }
});
</script>

<template>
  <div v-if="hostMissing" class="extraction-surface-error" data-testid="extraction-v4-host-missing">
    {{ hostMissingMessage }}
  </div>
  <div v-else-if="catalogError" class="extraction-surface-error" data-testid="extraction-v4-catalog-error">
    {{ catalogError }}
  </div>
  <!-- 直接挂共享包 ExpressionEditor 本体。它吃 ExpressionModel，由 expressionModel/
       onExpressionModelUpdate 与权威的 V4 节点文档双向翻译（见上方「模型往返」段）。
       筛选段恒为表达式模式，与 V4「仅表达式输入」边界一致。write-operation：非空时编辑器④段
       渲染「写目标」UI（新增/修改/删除数据）而不是取数的字段填充映射；取数模式不传（undefined）。
       source-picker-options：只作用于①来源段的「＋添加」选择器（取数模式=查询来源表，写模式=定位
       条件/关联的来源表），显式放开数据表+视图+外部数据表、允许跨应用；④写目标段另有独立选择器、不接
       此 prop，见上方 sourcePickerOptions 常量注释。 -->
  <div v-else class="extraction-surface">
    <ExpressionEditor
      :model-value="expressionModel"
      :template-id="templateId"
      :write-operation="writeOperation || undefined"
      :readonly="!sessionEditable"
      :staging-source="stagingSource"
      :notices="notices"
      :source-picker-options="sourcePickerOptions"
      @update:model-value="onExpressionModelUpdate"
    />
  </div>
</template>

<style scoped>
/* 整体布局：编辑器占满可用高度（通知已并进编辑器自己的校验状态条，见 notices prop）。 */
.extraction-surface {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.extraction-surface > :first-child {
  flex: 1 1 auto;
  min-height: 0;
}

/* 嵌入取数节点面板：外层面板已给过一圈留白，共享编辑器 .qb-layout 自带的 16px 内边距去掉，
   避免两层叠加；右栏（③目标）补与来源表/筛选条件 .qb-panel 同款的描边、圆角与内边距，三块卡片观感一致。
   :deep 限定在本组件渲染树内，不影响共享包的其他消费方。 */
.extraction-surface :deep(.qb-layout) {
  padding: 0;
}
.extraction-surface :deep(.qb-right) {
  border: 1px solid rgb(var(--border-color));
  border-radius: 8px;
  padding: 10px 12px;
  box-sizing: border-box;
}

/* 错误态样式沿用共享包编辑器错误提示的视觉语言（同一套 --error-color CSS 变量），
   保持取数节点面内错误提示观感一致。 */
.extraction-surface-error {
  padding: 6px 10px;
  border-radius: 4px;
  font-size: 12px;
  color: rgb(var(--error-color));
  background: rgb(var(--error-color) / 0.1);
}
</style>
