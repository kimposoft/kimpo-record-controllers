package router

import (
	"context"
	"testing"

	"github.com/kimposoft/kimpo-plugin-sdk/gen/bizpb"
)

func TestGetActionsDoesNotContributeWorkflowStart(t *testing.T) {
	resp, err := New().GetActions(context.Background(), &bizpb.GetActionsRequest{})
	if err != nil {
		t.Fatalf("GetActions() error = %v", err)
	}

	want := map[string]int32{
		"data.extract_record": 10,
		"data.create_record":  20,
		"data.update_record":  30,
		"data.delete_record":  40,
		"data.sort_detail":    50,
	}
	if len(resp.GetActions()) != len(want) {
		t.Fatalf("GetActions() count = %d, want %d", len(resp.GetActions()), len(want))
	}
	for _, action := range resp.GetActions() {
		name := action.GetName()
		if name == "workflow.start_instance" {
			t.Fatal("record controllers must not contribute workflow.start_instance")
		}
		weight, ok := want[name]
		if !ok {
			t.Fatalf("unexpected action %q", name)
		}
		if action.GetSortWeight() != weight {
			t.Errorf("action %q sort weight = %d, want %d", name, action.GetSortWeight(), weight)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("missing action %q", name)
	}
}
