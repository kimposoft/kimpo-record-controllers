package extract

import (
	"os"
	"strings"
	"testing"
)

func TestDetectRetiredPublicShapeRejectsAtAnyDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		code    string
		path    string
	}{
		{name: "raw nested in array", payload: `{"targets":[{"outputs":[{"expression":{"args":[{"kind":"raw","text":"SELECT secret"}]}}]}]}`, code: publicSchemaCodeRawNodeRetired, path: "$.targets[0].outputs[0].expression.args[0].kind"},
		{name: "post sort presence", payload: `{"target":{"post_sort":null}}`, code: publicSchemaCodeInlinePostSortRetired, path: "$.target.post_sort"},
		{name: "take hyphen", payload: `{"target":{"take-v1":false}}`, code: publicSchemaCodeTakeV1Retired, path: "$.target.take-v1"},
		{name: "take underscore", payload: `{"target":{"take_v1":{}}}`, code: publicSchemaCodeTakeV1Retired, path: "$.target.take_v1"},
		{name: "take selector", payload: `{"meta":{"pushdown_face":"RECORD_EXTRACT_TAKE_V1"}}`, code: publicSchemaCodeTakeV1Retired, path: "$.*.pushdown_face"},
		{name: "kimpo sql", payload: `{"nested":{"kimpo_sql":"SELECT secret"}}`, code: publicSchemaCodeKimpoSQLRetired, path: "$.*.kimpo_sql"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			violation, err := detectRetiredPublicShape([]byte(test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if violation == nil || violation.Code != test.code || violation.Path != test.path {
				t.Fatalf("violation=%#v, want code=%q path=%q", violation, test.code, test.path)
			}
			if strings.Contains(violation.Error(), "secret") || strings.Contains(violation.Error(), "SELECT") {
				t.Fatalf("violation leaked business value: %q", violation.Error())
			}
		})
	}
}

func TestDetectRetiredPublicShapeAllowsCurrentAndUnknownShapes(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		`{"limit":10,"order_by":[{"order":"asc","value":{"kind":"source_field"}}]}`,
		`{"future_extension":{"enabled":true}}`,
		`{"pushdown_face":"RECORD_EXTRACT_TAKE_V2"}`,
		`{"capsule_ref":"capsule-1","bound_profile":"mysql8","null_barrier":true,"result_type":"text"}`,
	} {
		violation, err := detectRetiredPublicShape([]byte(payload))
		if err != nil || violation != nil {
			t.Fatalf("current/unknown shape changed: payload=%s violation=%v err=%v", payload, violation, err)
		}
	}
}

func TestRetiredPublicSchemaCodesHaveBothPluginLocales(t *testing.T) {
	t.Parallel()

	for _, code := range []string{
		publicSchemaCodeRawNodeRetired,
		publicSchemaCodeInlinePostSortRetired,
		publicSchemaCodeTakeV1Retired,
		publicSchemaCodeKimpoSQLRetired,
		// 3.4 旧格式取数节点 Execute 入口具名拒绝码。
		publicSchemaCodeExpressionLegacyRetired,
	} {
		key := publicSchemaMessageKey(code)
		if key == "" {
			t.Fatalf("missing message key for %s", code)
		}
		for _, locale := range []string{"zh-CN", "en-US"} {
			if message := tr.T(locale, key, nil); message == "" || message == key {
				t.Fatalf("missing %s translation for %s (%s): %q", locale, code, key, message)
			}
		}
	}
}

func TestDetectRetiredPublicShapeIsStableAndScrubsDynamicKeys(t *testing.T) {
	t.Parallel()

	const payload = `{"z":{"kind":"raw"},"a":{"post_sort":[]}}`
	for i := 0; i < 50; i++ {
		violation, err := detectRetiredPublicShape([]byte(payload))
		if err != nil || violation == nil || violation.Code != publicSchemaCodeInlinePostSortRetired || violation.Path != "$.*.post_sort" {
			t.Fatalf("iteration %d: violation=%#v err=%v", i, violation, err)
		}
	}

	const secretKey = "Cookie-auth_session-lease-route-attach-business-secret"
	violation, err := detectRetiredPublicShape([]byte(`{"` + secretKey + `":{"kind":"raw"}}`))
	if err != nil || violation == nil || violation.Path != "$.*.kind" {
		t.Fatalf("dynamic key violation=%#v err=%v", violation, err)
	}
	if strings.Contains(violation.Path, secretKey) || strings.Contains(violation.Error(), secretKey) {
		t.Fatalf("dynamic key leaked: %#v", violation)
	}
}

func TestDetectRetiredPublicShapeLeavesMalformedJSONToTypedDecoder(t *testing.T) {
	t.Parallel()

	violation, err := detectRetiredPublicShape([]byte(`{"kind":`))
	if err == nil || violation != nil {
		t.Fatalf("malformed JSON must remain a decoder error: violation=%#v err=%v", violation, err)
	}
}

func TestRetiredPublicSchemaGatePrecedesTypedDecodeAndHostIO(t *testing.T) {
	t.Parallel()

	serviceRaw, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	serviceText := string(serviceRaw)
	validateStart := strings.Index(serviceText, "func (s *Service) ValidateParams")
	if validateStart < 0 {
		t.Fatal("ValidateParams entrypoint missing")
	}
	gate := strings.Index(serviceText[validateStart:], "detectRetiredExpressionValue(expression)")
	schema := strings.Index(serviceText[validateStart:], "plugin.ValidateParamsAgainstSchema")
	if gate < 0 || schema < 0 || gate >= schema {
		t.Fatalf("ValidateParams public-schema gate must precede SDK schema decode: start=%d gate=%d schema=%d", validateStart, gate, schema)
	}

	// Execute 对任何 params.expression 在入口处整体具名拒绝。这里守次序与卫生：
	// kimpo_sql 闸 → expression 闸 → 宿主回执解析，且拒绝分支不得插值/记录被拒绝的业务载荷
	// （只允许记录常量错误码）。
	execStart := strings.Index(serviceText, "func (s *Service) Execute(")
	if execStart < 0 {
		t.Fatal("Execute entrypoint missing")
	}
	execText := serviceText[execStart:]
	kimpoSQLGate := strings.Index(execText, `params["kimpo_sql"]`)
	exprGate := strings.Index(execText, `params["expression"]`)
	legacyReject := strings.Index(execText, "publicSchemaCodeExpressionLegacyRetired), nil), nil")
	receipt := strings.Index(execText, "plugin.ParseRecordExtractExecutionTimeHandled(")
	if kimpoSQLGate < 0 || exprGate < 0 || legacyReject < 0 || receipt < 0 ||
		kimpoSQLGate >= exprGate || exprGate >= legacyReject || legacyReject >= receipt {
		t.Fatalf("Execute gate order drifted: kimpo_sql=%d expression=%d reject=%d receipt=%d", kimpoSQLGate, exprGate, legacyReject, receipt)
	}
	gateBlock := execText[exprGate:legacyReject]
	if strings.Contains(gateBlock, `params["expression"])`) || strings.Contains(gateBlock, "hasExpr)") ||
		strings.Contains(gateBlock, "ctxMap") || strings.Contains(gateBlock, "%v") {
		t.Fatalf("Execute legacy expression rejection must not log or interpolate rejected payload: %s", gateBlock)
	}
}
