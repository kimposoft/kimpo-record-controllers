package extract

import (
	"context"
	"testing"

	"github.com/kimposoft/kimpo-plugin-sdk/plugin"
)

// capturingLog 记录上报的结构化日志条目，用于断言走了 SDK 分级日志器通道。
type capturingLog struct{ entries []plugin.LogEntry }

func (c *capturingLog) Write(_ context.Context, e plugin.LogEntry) error {
	c.entries = append(c.entries, e)
	return nil
}
func (c *capturingLog) WriteBatch(_ context.Context, es []plugin.LogEntry) error {
	c.entries = append(c.entries, es...)
	return nil
}

// logOnlyHost 仅提供 Log/Logger 子客户端（其余出站为 nil），用于验证 logf 的结构化上报路径。
// Logger 用 plugin.NewLogger 包 capturingLog 构造（minSeverity=trace 保证各级别放行），
// 实例须缓存复用——logf 经它 Emit，断言前 Flush 排空攒批队列。
// 嵌入 plugin.Host 接口零值兜底其余方法（与 service_test.go fakeHost 同一取舍）。
type logOnlyHost struct {
	plugin.Host
	lg     plugin.LogClient
	logger plugin.Logger
}

func (h *logOnlyHost) Template() plugin.TemplateClient { return nil }
func (h *logOnlyHost) Cache() plugin.CacheClient       { return nil }
func (h *logOnlyHost) Asset() plugin.AssetClient       { return nil }
func (h *logOnlyHost) Event() plugin.EventClient       { return nil }
func (h *logOnlyHost) Log() plugin.LogClient           { return h.lg }
func (h *logOnlyHost) Logger() plugin.Logger           { return h.logger }

func (h *logOnlyHost) Query() plugin.QueryClient           { return nil }
func (h *logOnlyHost) Mirror() plugin.MirrorClient         { return nil }
func (h *logOnlyHost) Capability() plugin.CapabilityClient { return nil }

// host 注入且有 Logger 通道时，logf 应把级别/模块/消息经 SDK 分级器结构化上报。
func TestLogfReportsToHost(t *testing.T) {
	cl := &capturingLog{}
	logger := plugin.NewLogger(cl, "trace")
	ctx := plugin.WithHost(context.Background(), &logOnlyHost{lg: cl, logger: logger})
	logf(ctx, "ERROR", "取数失败 code=%d", 42)
	logger.Flush() // 排空攒批队列，保证条目已送达 capturingLog
	if len(cl.entries) != 1 {
		t.Fatalf("expected 1 host log entry, got %d", len(cl.entries))
	}
	e := cl.entries[0]
	if e.Severity != "error" || e.Module != "extraction" || e.Message != "取数失败 code=42" {
		t.Fatalf("unexpected log entry: %+v", e)
	}
	if e.Category != "plugin" {
		t.Fatalf("logf 应以 category=plugin 上报, got %q", e.Category)
	}
	if e.OccurredAt.IsZero() {
		t.Fatalf("SDK 分级器应在 Emit 时打点 OccurredAt")
	}
}

// host 未注入（裸进程 / 无回连令牌）时 logf 仅写 stderr，不 panic、不阻断。
func TestLogfNoHostNoPanic(t *testing.T) {
	logf(context.Background(), "INFO", "无 host 兜底 stderr")
}
