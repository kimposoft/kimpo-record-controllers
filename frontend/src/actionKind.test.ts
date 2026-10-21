/**
 * actionKind 单测：surface 入参 → 数据动作种类的三种来源，
 * 以及种类 → 写操作映射；与 manifest.json 的 data_actions / operation_kind surfaces 逐条对齐。
 */
import { describe, expect, it } from 'vitest';
import { resolveDataActionKind, writeOperationForKind } from './actionKind';
// 直接读插件根 manifest.json（唯一权威），不内联副本——有人改了 manifest 而没改本模块时这里要转红。
import manifest from '../../manifest.json';

describe('resolveDataActionKind', () => {
  it.each([
    ['data.extract_record', 'fetch'],
    ['data.create_record', 'create'],
    ['data.update_record', 'update'],
    ['data.delete_record', 'delete'],
    ['operation_kind:fetch', 'fetch'],
    ['operation_kind:delete', 'delete']
  ] as const)('bind %s → %s', (bind, kind) => {
    expect(resolveDataActionKind({ bind })).toBe(kind);
  });

  it('第三方动作名按宿主下发的 operationKind / operation_kind 判定', () => {
    expect(resolveDataActionKind({ bind: 'org.example.x.fetch_order', hostContext: { operationKind: 'fetch' } })).toBe('fetch');
    expect(resolveDataActionKind({ bind: 'org.example.x.purge', hostContext: { operation_kind: 'delete' } })).toBe('delete');
  });

  it('无法判定或种类不在闭集时返回空串', () => {
    expect(resolveDataActionKind({ bind: 'org.example.x.unknown' })).toBe('');
    expect(resolveDataActionKind({ bind: 'operation_kind:upsert' })).toBe('');
    expect(resolveDataActionKind({ bind: 'org.example.x.y', hostContext: { operationKind: 'merge' } })).toBe('');
    expect(resolveDataActionKind({})).toBe('');
  });

  it('官方名优先于 hostContext（官方兼容名种类永远以兼容表为准）', () => {
    expect(resolveDataActionKind({ bind: 'data.create_record', hostContext: { operationKind: 'delete' } })).toBe('create');
  });
});

describe('writeOperationForKind', () => {
  it('create/update/delete → insert/update/delete，fetch 与空串 → 空串（取数模式）', () => {
    expect(writeOperationForKind('create')).toBe('insert');
    expect(writeOperationForKind('update')).toBe('update');
    expect(writeOperationForKind('delete')).toBe('delete');
    expect(writeOperationForKind('fetch')).toBe('');
    expect(writeOperationForKind('')).toBe('');
  });
});

describe('manifest 对齐', () => {
  // manifest 每个 action-config surface 的 bind 都必须能折算出种类，否则宿主挂载时会被 index.ts 拒绝。
  it('每个 action-config surface bind 都能折算出种类，且 data_actions 与兼容名一致', () => {
    const surfaces = manifest.capabilities.frontend.surfaces as Array<{ type: string; bind: string }>;
    for (const s of surfaces.filter(x => x.type === 'action-config')) {
      expect(resolveDataActionKind({ bind: s.bind }), s.bind).not.toBe('');
    }
    for (const d of manifest.capabilities.data_actions as Array<{ action: string; operation_kind: string }>) {
      expect(resolveDataActionKind({ bind: d.action })).toBe(d.operation_kind);
    }
  });
});
