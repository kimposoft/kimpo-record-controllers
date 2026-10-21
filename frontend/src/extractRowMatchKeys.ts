/**
 * extractRowMatchKeys —— V4 取数配置面对「自动按行列匹配」的**只读预览**。
 * 自动按行列匹配是生成 IR 与运行期的行为，不应改写用户的配置。
 *
 * 权威实现在宿主后端：展开期（保存校验 / 发布冻结 / 执行期同一纯函数）从筛选条件里识别「来源字段 = 本报表
 * 明细目标字段」等式并派生匹配键，等式**保留在筛选里**继续作为 WHERE 生效，文档零改写。本文件只在收口时按
 * 同一口径识别出这些等式，供 ActionConfigSurface 以 info 通知告诉用户「执行时哪些等式会作为匹配键」；
 * **绝不改文档**（若在前端把等式改写成 row_match 输出并从筛选里删除，用户保存再打开会看到被改写过的配置）。
 *
 * 识别口径（与宿主后端同一）：
 *   - 只看筛选条件里**顶层 AND** 连接的等式（OR / NOT / 括号里的子式一律不看，宁可少报也不误报）；
 *   - 等式两侧一侧是**本报表明细目标表**的字段（report_field，且 table_id 是本节点某个明细填充目标），
 *     另一侧是**显式来源**字段（source_field）；主表目标结构性禁止匹配，不看；
 *   - V4 只有文本，本文件不自己解析：结构由服务端 diagnose 回带的 AST 摘要（bind.astSummary：
 *     Logical/Comparison/FieldRef + rune span + stableId）给出，两侧身份按 `#{rN}` 反查绑定表。
 *
 * 本文件是纯函数，不碰 host/组件；调用方（ActionConfigSurface.finalizeToV4）负责调 diagnose 与产出通知。
 */
import type { V4ExpressionRefWire } from '@kimposoft/expression-editor';
import { runeToUtf16Index } from '@kimposoft/expression-editor';

/** 服务端 bind.astSummary 的节点形状（只列本文件用到的字段，其余透传不读）。 */
export interface FilterAstNode {
  kind?: string;
  operator?: string;
  span?: { start: number; end: number };
  left?: FilterAstNode;
  right?: FilterAstNode;
  stableId?: string;
  [key: string]: unknown;
}

/** 一条将由服务端派生为匹配键的等式：目标表/目标字段 + 等式两侧绑定 + 原文（供通知文案）。 */
export interface AutoRowMatchKeyPreview {
  targetTableId: string;
  targetFieldId: string;
  /** 等式来源侧的 token 化文本（如 `[物料编号]#{r1}`）——服务端合成输出列的表达式即此。 */
  sourceText: string;
  sourceRef: V4ExpressionRefWire;
  reportRef: V4ExpressionRefWire;
  /** 整条等式（token 化文本）。 */
  equalityText: string;
}

/** 顶层 AND 展平：Logical(AND) 递归取左右；其它节点（含 OR / NOT / 括号 / 单个比较）作为一个子式。 */
function topLevelConjuncts(node: FilterAstNode): FilterAstNode[] {
  if (node.kind === 'Logical' && node.operator === 'AND' && node.left && node.right) {
    return [...topLevelConjuncts(node.left), ...topLevelConjuncts(node.right)];
  }
  return [node];
}

function sliceBySpan(text: string, node: FilterAstNode): string | null {
  const span = node.span;
  if (!span || typeof span.start !== 'number' || typeof span.end !== 'number' || span.end < span.start) return null;
  return text.slice(runeToUtf16Index(text, span.start), runeToUtf16Index(text, span.end));
}

/**
 * 从筛选条件识别「来源字段 = 本报表明细目标字段」等式（只读，不产生任何改写）。
 *
 * @param filterText  token 化后的筛选文本（tokenize 回带的 source_text，含 `#{rN}`）；AST 摘要必须
 *                    是对这份文本 diagnose 得到的（span 按它计）。
 * @param refs        该文本的绑定表（tokenize 回带的 refs）。
 * @param ast         diagnose 回带的 AST 摘要根节点。
 * @param detailTargetTableIds 本节点开启自动匹配的明细填充目标的 target_data_table_id 集合。
 * @returns 识别到的等式（按出现顺序；同一目标表同一字段只报首条，与服务端「首个等式胜出」一致），无则空数组。
 */
export function previewAutoRowMatchKeys(
  filterText: string,
  refs: V4ExpressionRefWire[],
  ast: FilterAstNode | null | undefined,
  detailTargetTableIds: ReadonlySet<string>
): AutoRowMatchKeyPreview[] {
  if (!ast || !filterText || detailTargetTableIds.size === 0) return [];
  const refsById = new Map(refs.map(r => [r.ref_id, r]));
  const seen = new Set<string>();
  const matches: AutoRowMatchKeyPreview[] = [];
  for (const conjunct of topLevelConjuncts(ast)) {
    const match = matchKeyOf(filterText, conjunct, refsById, detailTargetTableIds);
    if (!match) continue;
    const key = `${match.targetTableId} ${match.targetFieldId}`;
    if (seen.has(key)) continue;
    seen.add(key);
    matches.push(match);
  }
  return matches;
}

/** 一个子式若是「来源字段 = 本报表明细目标字段」（任一侧序）则返回预览项，否则 null。 */
function matchKeyOf(
  filterText: string,
  node: FilterAstNode,
  refsById: ReadonlyMap<string, V4ExpressionRefWire>,
  detailTargetTableIds: ReadonlySet<string>
): AutoRowMatchKeyPreview | null {
  if (node.kind !== 'Comparison' || node.operator !== 'EQUAL' || !node.left || !node.right) return null;
  if (node.left.kind !== 'FieldRef' || node.right.kind !== 'FieldRef') return null;
  const sides = [node.left, node.right].map(side => ({ node: side, ref: side.stableId ? refsById.get(side.stableId) : undefined }));
  if (!sides[0].ref || !sides[1].ref) return null;
  const isTargetField = (r: V4ExpressionRefWire): boolean =>
    r.kind === 'report_field' && !!r.table_id && detailTargetTableIds.has(r.table_id) && !!r.field_id;
  const isPlainSource = (r: V4ExpressionRefWire): boolean => r.kind === 'source_field' && !!r.field_id;
  let reportSide = sides[0];
  let sourceSide = sides[1];
  if (!(isTargetField(reportSide.ref!) && isPlainSource(sourceSide.ref!))) {
    reportSide = sides[1];
    sourceSide = sides[0];
    if (!(isTargetField(reportSide.ref!) && isPlainSource(sourceSide.ref!))) return null;
  }
  const sourceText = sliceBySpan(filterText, sourceSide.node);
  const equalityText = sliceBySpan(filterText, node);
  if (!sourceText || !equalityText) return null;
  return {
    targetTableId: reportSide.ref!.table_id!,
    targetFieldId: reportSide.ref!.field_id!,
    sourceText: sourceText.trim(),
    sourceRef: sourceSide.ref!,
    reportRef: reportSide.ref!,
    equalityText: equalityText.trim()
  };
}
