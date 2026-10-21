package recordcontrollers

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// TestManifestJSONMatchesTrackedRootAndHasNoPackagedCopy 守护「根 manifest.json 单文件权威」：
// 内嵌字节与已提交的根 manifest.json 逐字节一致、返回值是隔离副本，并且 cmd/plugin 下不得再出现
// manifest.json 副本（没人同步它，留着就是静默漂移的第二事实源）。
func TestManifestJSONMatchesTrackedRootAndHasNoPackagedCopy(t *testing.T) {
	want, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	got := ManifestJSON()
	if !bytes.Equal(got, want) {
		t.Fatal("embedded manifest differs from tracked root manifest.json")
	}
	if len(got) == 0 {
		t.Fatal("embedded manifest must not be empty")
	}
	got[0] ^= 0xff
	if fresh := ManifestJSON(); !bytes.Equal(fresh, want) {
		t.Fatal("ManifestJSON must return an isolated copy")
	}
	if _, err := os.Stat("cmd/plugin/manifest.json"); err == nil {
		t.Fatal("cmd/plugin/manifest.json 是多余的副本，构建不消费它，请删除")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// TestManifestDeclaresDataActionsAndLeastPrivilege 守护官方数据动作插件与第三方走同一份公开数据动作契约：
//   - capabilities.data_actions 逐条声明四个官方名 → fetch / create / update / delete（与宿主内置兼容表一致）；
//   - capabilities.features 不声明 alice-prepared-dispatch（该特性只留给关联表单 ui.linked_form 的提供方
//     kimpo-form-controllers；本插件数据动作由 data.action:<kind> 授予准入）；
//   - api_scope 收为最小集：写类种类必需的 AliceDataService/ApplyChangeSet、明细排序实际调用的
//     MirrorService/ReadTable + ReorderDetailRows（detailsort/service.go），以及日志两项；不申请多余的 Mirror RPC；
//   - data_access 写类种类申请 record 模型且字段为 "*"（ApplyChangeSet 是整对象接口）；
//   - 追加四个 bind "operation_kind:<kind>" 的 action-config surface，官方标准面板为所有同种类动作服务。
func TestManifestDeclaresDataActionsAndLeastPrivilege(t *testing.T) {
	var m struct {
		APIScope struct {
			AllowedRPCs []string `json:"allowed_rpcs"`
		} `json:"api_scope"`
		DataAccess struct {
			AllowedModels []string            `json:"allowed_models"`
			Fields        map[string][]string `json:"fields"`
		} `json:"data_access"`
		Capabilities struct {
			Features    []string `json:"features"`
			DataActions []struct {
				Action        string `json:"action"`
				OperationKind string `json:"operation_kind"`
			} `json:"data_actions"`
			Frontend struct {
				Surfaces []struct {
					Type string `json:"type"`
					Bind string `json:"bind"`
				} `json:"surfaces"`
			} `json:"frontend"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(ManifestJSON(), &m); err != nil {
		t.Fatal(err)
	}

	wantKinds := map[string]string{
		"data.extract_record": "fetch",
		"data.create_record":  "create",
		"data.update_record":  "update",
		"data.delete_record":  "delete",
	}
	gotKinds := map[string]string{}
	for _, d := range m.Capabilities.DataActions {
		if _, dup := gotKinds[d.Action]; dup {
			t.Fatalf("data_actions 重复声明 %q", d.Action)
		}
		gotKinds[d.Action] = d.OperationKind
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Fatalf("data_actions = %v, want %v", gotKinds, wantKinds)
	}

	for _, f := range m.Capabilities.Features {
		if f == "alice-prepared-dispatch" {
			t.Fatal("数据动作经 data_actions 声明，manifest 不得声明 alice-prepared-dispatch")
		}
	}

	wantRPCs := []string{
		"kimpo.plugins.host.v1.AliceDataService/ApplyChangeSet",
		"kimpo.plugins.host.v1.LogService/Write",
		"kimpo.plugins.host.v1.LogService/WriteBatch",
		"kimpo.plugins.host.v1.MirrorService/ReadTable",
		"kimpo.plugins.host.v1.MirrorService/ReorderDetailRows",
	}
	gotRPCs := append([]string(nil), m.APIScope.AllowedRPCs...)
	sort.Strings(gotRPCs)
	if !reflect.DeepEqual(gotRPCs, wantRPCs) {
		t.Fatalf("allowed_rpcs = %v, want 最小集 %v", gotRPCs, wantRPCs)
	}

	if !reflect.DeepEqual(m.DataAccess.AllowedModels, []string{"record"}) ||
		!reflect.DeepEqual(m.DataAccess.Fields["record"], []string{"*"}) {
		t.Fatalf("data_access = %+v, want record 模型且字段为 *", m.DataAccess)
	}

	binds := map[string]bool{}
	for _, s := range m.Capabilities.Frontend.Surfaces {
		if s.Type == "action-config" {
			binds[s.Bind] = true
		}
	}
	for _, kind := range []string{"fetch", "create", "update", "delete"} {
		if !binds["operation_kind:"+kind] {
			t.Fatalf("缺少按种类回落的 action-config surface operation_kind:%s", kind)
		}
	}
	for action := range wantKinds {
		if !binds[action] {
			t.Fatalf("缺少官方动作名绑定的 action-config surface %s", action)
		}
	}
}
