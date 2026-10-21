package extract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// extract public schema 的退役形状必须在强类型解码前从原始 JSON 中识别；否则
// encoding/json 会把 take-v1 等未知字段静默丢弃，调用方会误以为配置已经生效。
const (
	publicSchemaCodeRawNodeRetired        = "EXPR_RAW_NODE_RETIRED"
	publicSchemaCodeInlinePostSortRetired = "EXPR_INLINE_POST_SORT_RETIRED"
	publicSchemaCodeTakeV1Retired         = "EXPR_TAKE_V1_RETIRED"
	publicSchemaCodeKimpoSQLRetired       = "ACTION_NODE_KIMPOSQL_RETIRED"
	// publicSchemaCodeExpressionLegacyRetired 是 V3.4 旧格式取数节点（params.expression）
	// 误达插件 Execute 时的具名拒绝码——插件已无该格式的执行路径，与 kimpo_sql 同款在 Host IO 前拒绝。
	publicSchemaCodeExpressionLegacyRetired = "EXTRACT_EXPRESSION_LEGACY_RETIRED"
)

// publicSchemaViolation 只携带稳定错误码和结构路径，不保存或回显被拒绝的业务值。
type publicSchemaViolation struct {
	Code string
	Path string
}

func (e *publicSchemaViolation) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s at %s", e.Code, e.Path)
}

// detectRetiredPublicShape 在任意嵌套对象/数组中查找已从 extract public schema
// 删除的形状。非退役未知字段保持现有行为；非法 JSON 留给既有解码闸归类。
func detectRetiredPublicShape(data []byte) (*publicSchemaViolation, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode public schema payload: %w", err)
	}
	return detectRetiredPublicValue(root, "$"), nil
}

func detectRetiredPublicValue(value any, path string) *publicSchemaViolation {
	switch typed := value.(type) {
	case map[string]any:
		// map 遍历顺序必须稳定，同一手工载荷才能始终返回同一个首个错误码。
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := publicSchemaChildPath(path, key)
			switch key {
			case "kimpo_sql":
				return &publicSchemaViolation{Code: publicSchemaCodeKimpoSQLRetired, Path: childPath}
			case "post_sort":
				return &publicSchemaViolation{Code: publicSchemaCodeInlinePostSortRetired, Path: childPath}
			case "take-v1", "take_v1":
				return &publicSchemaViolation{Code: publicSchemaCodeTakeV1Retired, Path: childPath}
			case "kind":
				if kind, ok := typed[key].(string); ok && kind == "raw" {
					return &publicSchemaViolation{Code: publicSchemaCodeRawNodeRetired, Path: childPath}
				}
			case "pushdown_face":
				if face, ok := typed[key].(string); ok && face == "RECORD_EXTRACT_TAKE_V1" {
					return &publicSchemaViolation{Code: publicSchemaCodeTakeV1Retired, Path: childPath}
				}
			}
			if violation := detectRetiredPublicValue(typed[key], childPath); violation != nil {
				return violation
			}
		}
	case []any:
		for index := range typed {
			if violation := detectRetiredPublicValue(typed[index], fmt.Sprintf("%s[%d]", path, index)); violation != nil {
				return violation
			}
		}
	}
	return nil
}

func publicSchemaChildPath(parent, key string) string {
	// 只允许固定 public schema 键进入错误路径；来源字段 ID、未来扩展键或恶意
	// secret-as-key 一律收敛成通配段，避免借 JSON key 泄漏业务值。
	switch key {
	case "args", "auto_row_match", "cases", "chart_targets", "clear", "dedupe_field_ids",
		"dedupe_scope", "else", "expression", "field_id", "fill_strategy", "func", "group",
		"group_by", "items", "joins", "kind", "kimpo_sql", "left", "limit", "mapping",
		"match_keys", "name", "on", "op", "order", "order_by", "outputs", "parent",
		"partition_by", "post_sort", "predicate", "pushdown_face", "refs", "right", "series",
		"source", "source_expr", "source_field", "table_alias", "tables", "take-v1", "take_v1",
		"target", "target_data_table_id", "target_field_id", "targets", "then", "value", "values",
		"when", "where", "where_expr", "win_order_by", "window", "x", "y":
		return parent + "." + key
	default:
		return parent + ".*"
	}
}

func detectRetiredExpressionValue(expression any) (*publicSchemaViolation, error) {
	payload, err := json.Marshal(expression)
	if err != nil {
		return nil, err
	}
	return detectRetiredPublicShape(payload)
}

func publicSchemaMessageKey(code string) string {
	switch code {
	case publicSchemaCodeRawNodeRetired:
		return "extract.public_schema.raw_node_retired"
	case publicSchemaCodeInlinePostSortRetired:
		return "extract.public_schema.inline_post_sort_retired"
	case publicSchemaCodeTakeV1Retired:
		return "extract.public_schema.take_v1_retired"
	case publicSchemaCodeKimpoSQLRetired:
		return "extract.public_schema.kimpo_sql_retired"
	case publicSchemaCodeExpressionLegacyRetired:
		return "extract.public_schema.expression_legacy_retired"
	default:
		return ""
	}
}

func publicSchemaFieldPath(violation *publicSchemaViolation) string {
	if violation == nil {
		return "expression"
	}
	return "expression" + strings.TrimPrefix(violation.Path, "$")
}

func publicSchemaValidationMessage(violation *publicSchemaViolation) string {
	if violation == nil {
		return ""
	}
	return "[" + violation.Code + "] " + trDefault(publicSchemaMessageKey(violation.Code), nil)
}
