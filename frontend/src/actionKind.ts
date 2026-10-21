/**
 * actionKind — action-config surface 的「数据动作种类」判定。
 *
 * 本插件的标准取数 / 写配置面板既服务四个官方动作名（data.extract_record 等），也通过 manifest 中四个
 * `bind: "operation_kind:<kind>"` surface 服务同种类的其他数据动作（含第三方）：宿主对「带 operation_kind、
 * 但没有按名绑定 surface」的动作回落到这四个 surface。此时 `bind` 可能是：
 *   - 官方动作名（名字 → 种类走兼容表，与宿主的兼容映射同口径）；
 *   - `operation_kind:<kind>`（宿主把命中的 surface bind 原样传入）；
 *   - 第三方动作名（宿主传动作名，种类另经 hostContext.operationKind / operation_kind 下发）。
 * 本模块把三种入参统一折算为种类，取数 / 写模式只由种类决定，不比较动作名。
 */

/** 标准操作种类闭集（与宿主 pipeline.DataActionKind、manifest capabilities.data_actions 同口径）。 */
export type DataActionKind = 'fetch' | 'create' | 'update' | 'delete';

/** 写模式下 ExpressionEditor 认识的写操作字面量（种类 → 写操作，fetch 无写操作）。 */
export type DataActionWriteOperation = 'insert' | 'update' | 'delete';

const KINDS: readonly DataActionKind[] = ['fetch', 'create', 'update', 'delete'];

/** 官方兼容名 → 种类（与宿主内置兼容表、本插件 manifest data_actions 逐条一致）。 */
const OFFICIAL_ACTION_KINDS: Readonly<Record<string, DataActionKind>> = {
  'data.extract_record': 'fetch',
  'data.create_record': 'create',
  'data.update_record': 'update',
  'data.delete_record': 'delete'
};

/** 按种类绑定的 surface bind 前缀。 */
export const OPERATION_KIND_BIND_PREFIX = 'operation_kind:';

function asKind(raw: unknown): DataActionKind | '' {
  if (typeof raw !== 'string') return '';
  const v = raw.trim();
  return (KINDS as readonly string[]).includes(v) ? (v as DataActionKind) : '';
}

/**
 * resolveDataActionKind 把 surface 入参折算为种类；无法判定时返回空串（调用方据此具名拒绝挂载）。
 * 判定顺序：官方名 → `operation_kind:<kind>` bind → 宿主下发的 operationKind / operation_kind。
 */
export function resolveDataActionKind(input: {
  bind?: string;
  hostContext?: Record<string, unknown>;
  operationKind?: unknown;
}): DataActionKind | '' {
  const bind = (input.bind ?? '').trim();
  const official = OFFICIAL_ACTION_KINDS[bind];
  if (official) return official;
  if (bind.startsWith(OPERATION_KIND_BIND_PREFIX)) {
    return asKind(bind.slice(OPERATION_KIND_BIND_PREFIX.length));
  }
  const ctx = input.hostContext ?? {};
  return asKind(input.operationKind) || asKind(ctx.operationKind) || asKind(ctx.operation_kind);
}

/** 种类 → 写操作；fetch 与无法判定一律返回空串（= 取数模式，沿用组件既有容错口径）。 */
export function writeOperationForKind(kind: DataActionKind | ''): DataActionWriteOperation | '' {
  switch (kind) {
    case 'create':
      return 'insert';
    case 'update':
      return 'update';
    case 'delete':
      return 'delete';
    default:
      return '';
  }
}
