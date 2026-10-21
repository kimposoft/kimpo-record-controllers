package update

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

// TestParamsSchemaMatchesGolden 守护本动作参数 schema：由 SDK biz.DataActionParamsSchema（种类 update）
// 产出，json.Marshal 结果必须与金样逐字节一致。金样 testdata/params_schema_golden.json 与 SDK
// direction/biz/testdata/dataaction/update.json 同源；任何一侧改 schema 都必须同时改另一侧与金样。
// 另验经 structpb 往返（GetActions 的实际形态）后仍一致。
func TestParamsSchemaMatchesGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/params_schema_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(paramsSchemaMap())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("参数 schema 与金样不一致\n got: %s\nwant: %s", got, want)
	}
	st, err := structpb.NewStruct(paramsSchemaMap())
	if err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(st.AsMap())
	if !bytes.Equal(again, want) {
		t.Fatalf("structpb 往返后不一致：%s", again)
	}
}
