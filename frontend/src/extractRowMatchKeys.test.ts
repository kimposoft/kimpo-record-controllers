/**
 * 「自动按行列匹配」只读预览纯函数用例（抽键权威在服务端展开期，前端只识别不改写）。
 * AST 摘要按服务端 bind.astSummary 的真实形状手工构造：Logical(AND) / Comparison(EQUAL) / FieldRef(stableId)
 * + rune span（本用例文本全在 BMP 内，rune 偏移即 UTF-16 下标，直接按 indexOf 计算）。
 */
import { describe, expect, it } from 'vitest';
import type { V4ExpressionRefWire } from '@kimposoft/expression-editor';
import { previewAutoRowMatchKeys, type FilterAstNode } from './extractRowMatchKeys';

const TBL_DETAIL = 'tbl_detail';
const FLD_CODE = 'fld_code';

/** 来源 t1.物料编号（r1）、本报表明细.物料编号（r2）、来源 t1.数量（r3）、本报表明细.规格（r4）。 */
const refs: V4ExpressionRefWire[] = [
  { ref_id: 'r1', kind: 'source_field', source_instance_id: 't1', table_id: 'tbl_src', field_id: 'fld_src_code', last_label: '[采购入库的扩展表].[物料编号]' },
  { ref_id: 'r2', kind: 'report_field', source_instance_id: 'rpt-tbl_detail-abc', table_id: TBL_DETAIL, field_id: FLD_CODE, last_label: '本报表.[采购入库的扩展表].[物料编号]' },
  { ref_id: 'r3', kind: 'source_field', source_instance_id: 't1', table_id: 'tbl_src', field_id: 'fld_qty', last_label: '[采购入库的扩展表].[数量]' },
  { ref_id: 'r4', kind: 'report_field', source_instance_id: 'rpt-tbl_detail-abc', table_id: TBL_DETAIL, field_id: 'fld_spec', last_label: '本报表.[采购入库的扩展表].[规格]' }
];

/** 在 text 里定位子串并生成 span（子串须唯一出现）。 */
function spanOf(text: string, sub: string): { start: number; end: number } {
  const start = text.indexOf(sub);
  if (start < 0 || text.indexOf(sub, start + 1) >= 0) throw new Error(`fixture: ${sub} must occur exactly once in ${text}`);
  return { start, end: start + sub.length };
}
function fieldRef(text: string, token: string, stableId: string): FilterAstNode {
  return { kind: 'FieldRef', stableId, span: spanOf(text, token) };
}
function equal(text: string, leftToken: string, leftId: string, rightToken: string, rightId: string): FilterAstNode {
  const whole = `${leftToken} = ${rightToken}`;
  return { kind: 'Comparison', operator: 'EQUAL', span: spanOf(text, whole), left: fieldRef(text, leftToken, leftId), right: fieldRef(text, rightToken, rightId) };
}
function and(left: FilterAstNode, right: FilterAstNode, text: string): FilterAstNode {
  return { kind: 'Logical', operator: 'AND', span: { start: 0, end: text.length }, left, right };
}

describe('previewAutoRowMatchKeys（只读识别，不改写任何输入）', () => {
  it('单条「来源字段 = 本报表明细字段」等式：识别为将派生的匹配键，带两侧绑定与原文', () => {
    const text = '[物料编号]#{r1} = [物料编号]#{r2}';
    const ast = equal(text, '[物料编号]#{r1}', 'r1', '[物料编号]#{r2}', 'r2');
    const out = previewAutoRowMatchKeys(text, refs, ast, new Set([TBL_DETAIL]));
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ targetTableId: TBL_DETAIL, targetFieldId: FLD_CODE, sourceText: '[物料编号]#{r1}', equalityText: text });
    expect(out[0].sourceRef.ref_id).toBe('r1');
    expect(out[0].reportRef.ref_id).toBe('r2');
  });

  it('等式两侧反序（本报表在左）同样识别，来源侧仍是来源字段', () => {
    const text = '[物料编号]#{r2} = [物料编号]#{r1}';
    const ast = equal(text, '[物料编号]#{r2}', 'r2', '[物料编号]#{r1}', 'r1');
    const out = previewAutoRowMatchKeys(text, refs, ast, new Set([TBL_DETAIL]));
    expect(out[0]).toMatchObject({ targetFieldId: FLD_CODE, sourceText: '[物料编号]#{r1}' });
  });

  it('顶层 AND 里混有其它子式：只识别等式，普通过滤子式不受影响（本函数无任何改写）', () => {
    const text = '[物料编号]#{r1} = [物料编号]#{r2} AND [数量]#{r3} > 0';
    const eq = equal(text, '[物料编号]#{r1}', 'r1', '[物料编号]#{r2}', 'r2');
    const gt: FilterAstNode = { kind: 'Comparison', operator: 'GREATER', span: spanOf(text, '[数量]#{r3} > 0'), left: fieldRef(text, '[数量]#{r3}', 'r3'), right: { kind: 'Literal', span: spanOf(text, ' 0') } };
    const out = previewAutoRowMatchKeys(text, refs, and(eq, gt, text), new Set([TBL_DETAIL]));
    expect(out).toHaveLength(1);
    expect(out[0].equalityText).toBe('[物料编号]#{r1} = [物料编号]#{r2}');
  });

  it('同一目标字段出现两条等式：只报首条（与服务端「首个等式胜出」一致）；不同字段各报一条', () => {
    const text = '[物料编号]#{r1} = [物料编号]#{r2} AND [数量]#{r3} = [规格]#{r4}';
    const eq1 = equal(text, '[物料编号]#{r1}', 'r1', '[物料编号]#{r2}', 'r2');
    const eq2 = equal(text, '[数量]#{r3}', 'r3', '[规格]#{r4}', 'r4');
    const out = previewAutoRowMatchKeys(text, refs, and(eq1, eq2, text), new Set([TBL_DETAIL]));
    expect(out.map(m => m.targetFieldId)).toEqual([FLD_CODE, 'fld_spec']);
  });

  it('等式在 OR 之下不识别（不是顶层 AND 子式）', () => {
    const text = '[物料编号]#{r1} = [物料编号]#{r2} OR [数量]#{r3} > 0';
    const eq = equal(text, '[物料编号]#{r1}', 'r1', '[物料编号]#{r2}', 'r2');
    const gt: FilterAstNode = { kind: 'Comparison', operator: 'GREATER', span: spanOf(text, '[数量]#{r3} > 0') };
    const or: FilterAstNode = { kind: 'Logical', operator: 'OR', span: { start: 0, end: text.length }, left: eq, right: gt };
    expect(previewAutoRowMatchKeys(text, refs, or, new Set([TBL_DETAIL]))).toEqual([]);
  });

  it('本报表侧不是本节点开启自动匹配的明细目标表（或没有明细目标）：不识别', () => {
    const text = '[物料编号]#{r1} = [物料编号]#{r2}';
    const ast = equal(text, '[物料编号]#{r1}', 'r1', '[物料编号]#{r2}', 'r2');
    expect(previewAutoRowMatchKeys(text, refs, ast, new Set(['tbl_other']))).toEqual([]);
    expect(previewAutoRowMatchKeys(text, refs, ast, new Set())).toEqual([]);
  });

  it('两侧都是来源字段：不识别', () => {
    const text = '[数量]#{r3} = [物料编号]#{r1}';
    const ast = equal(text, '[数量]#{r3}', 'r3', '[物料编号]#{r1}', 'r1');
    expect(previewAutoRowMatchKeys(text, refs, ast, new Set([TBL_DETAIL]))).toEqual([]);
  });

  it('AST 缺失或不是等式：不识别', () => {
    const text = '[物料编号]#{r1} <> [物料编号]#{r2}';
    const ne: FilterAstNode = { kind: 'Comparison', operator: 'NOT_EQUAL', span: { start: 0, end: text.length }, left: fieldRef(text, '[物料编号]#{r1}', 'r1'), right: fieldRef(text, '[物料编号]#{r2}', 'r2') };
    expect(previewAutoRowMatchKeys(text, refs, ne, new Set([TBL_DETAIL]))).toEqual([]);
    expect(previewAutoRowMatchKeys(text, refs, null, new Set([TBL_DETAIL]))).toEqual([]);
  });
});
