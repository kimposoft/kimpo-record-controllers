package writecommon

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// wrapLikeSDK 模拟 SDK plugin.ApplyMutation 对宿主 ApplyChangeSet 错误的包装形态
// （fmt.Errorf("alice: apply change set: %w", err)，见 kimpo-plugin-sdk plugin/alicemutation.go），
// 供本文件测试 ClassifyApplyError 在真实调用形态下仍能透过 %w 包装取到宿主 gRPC 状态。
func wrapLikeSDK(err error) error {
	return fmt.Errorf("alice: apply change set: %w", err)
}

// TestClassifyApplyError_MirrorFenceVariants 宿主写围栏的三种拒绝文案都应判定为围栏拒绝。
func TestClassifyApplyError_MirrorFenceVariants(t *testing.T) {
	for _, hostMsg := range []string{
		"MIRROR_FENCE_BUSY: target record is covered by an open Mirror session the caller does not own",
		"MIRROR_FENCE_UNAVAILABLE: Mirror write fence session ownership authority is unavailable",
		"ALICE_APPLY_FENCE_UNAVAILABLE: Mirror write fence rejected the request: some underlying reason",
	} {
		t.Run(hostMsg, func(t *testing.T) {
			err := wrapLikeSDK(status.Error(codes.FailedPrecondition, hostMsg))
			rejected, reason := ClassifyApplyError(err)
			if !rejected {
				t.Fatalf("want rejected=true, got false (reason=%q)", reason)
			}
			if reason != hostMsg {
				t.Fatalf("reason=%q, want 宿主原始状态文案 %q（不带 SDK/gRPC 包装噪音）", reason, hostMsg)
			}
		})
	}
}

// TestClassifyApplyError_OtherFailedPreconditionSurfacesReason 非围栏含义的
// FailedPrecondition（如变更集过期）应判 rejected=false，且 reason 是宿主原始状态文案。
func TestClassifyApplyError_OtherFailedPreconditionSurfacesReason(t *testing.T) {
	hostMsg := "ALICE_APPLY_CHANGESET_EXPIRED: change-set has expired; re-derive"
	err := wrapLikeSDK(status.Error(codes.FailedPrecondition, hostMsg))
	rejected, reason := ClassifyApplyError(err)
	if rejected {
		t.Fatalf("want rejected=false, got true")
	}
	if reason != hostMsg {
		t.Fatalf("reason=%q, want %q", reason, hostMsg)
	}
}

// TestClassifyApplyError_NonStatusErrorNeverLeaksRawText 🔴 不带 gRPC 状态的原始错误
// （本地/传输层错误、测试桩直接构造的 errors.New）必须 reason=""，绝不能把 err.Error()
// 原样透出——这类不可信文本可能夹带令牌/敏感上下文
// （同 create/update/delete 三包 service_test.go 的 TestExecute_ApplyFailureDoesNotFallBack
// 泄漏防护用例）。
func TestClassifyApplyError_NonStatusErrorNeverLeaksRawText(t *testing.T) {
	err := wrapLikeSDK(errors.New("credential ticket-should-not-leak"))
	rejected, reason := ClassifyApplyError(err)
	if rejected {
		t.Fatalf("want rejected=false, got true")
	}
	if reason != "" {
		t.Fatalf("reason=%q, want empty（非 gRPC 状态错误不得透出原文）", reason)
	}
}

// TestClassifyApplyError_Nil err==nil 时防御性返回零值，调用方不应在此路径调用本函数。
func TestClassifyApplyError_Nil(t *testing.T) {
	rejected, reason := ClassifyApplyError(nil)
	if rejected || reason != "" {
		t.Fatalf("rejected=%v reason=%q, want false/\"\"", rejected, reason)
	}
}
