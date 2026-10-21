package writecommon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// localesDir 与 //go:embed locales/*.json 指向同一份磁盘文件；测试直接读磁盘而非走
// embed.FS，纯粹是为了少一层间接，不影响生产 embed 声明的正确性（两者读的是同一批源文件）。
const localesDir = "locales"

// supportedLocales 是插件支持的四语言（zh-CN 为默认 locale）。
var supportedLocales = []string{"zh-CN", "en-US", "zh-TW", "ja-JP"}

func loadLocale(t *testing.T, locale string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(localesDir, locale+".json"))
	if err != nil {
		t.Fatalf("read %s.json: %v", locale, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s.json: %v", locale, err)
	}
	return m
}

// placeholderPattern 匹配 {name} 占位符，与 SDK i18n.interpolate 的占位语法一致。
var placeholderPattern = regexp.MustCompile(`\{[^{}]+\}`)

func placeholders(s string) []string {
	matches := placeholderPattern.FindAllString(s, -1)
	sort.Strings(matches)
	return matches
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFourLocalesShareExactKeySet 四语言包 key 集合必须完全一致（无缺失、无多余）：任何一语言
// 漏译一个 key，该 locale 下用户就会看到两级查找链回落到默认语言 zh-CN 的文案（或双 miss 时
// 直接看到裸 message_key）——必须在测试期发现，而不是留到运行时才暴露。
func TestFourLocalesShareExactKeySet(t *testing.T) {
	tables := map[string]map[string]string{}
	for _, locale := range supportedLocales {
		tables[locale] = loadLocale(t, locale)
	}
	base := tables["zh-CN"]
	if len(base) == 0 {
		t.Fatal("zh-CN 语言包为空")
	}
	for _, locale := range supportedLocales {
		if locale == "zh-CN" {
			continue
		}
		table := tables[locale]
		for key := range base {
			if _, ok := table[key]; !ok {
				t.Errorf("%s 缺少 key %q（zh-CN 已有）", locale, key)
			}
		}
		for key := range table {
			if _, ok := base[key]; !ok {
				t.Errorf("%s 存在多余 key %q（zh-CN 没有，四语言包须一一对应）", locale, key)
			}
		}
	}
}

// TestFourLocalesSharePlaceholders 同一 key 在四语言下的 {name} 占位符集合必须一致——防止某
// 语言翻译时漏填/改名占位符：SDK interpolate 对未提供的占位符原样保留（不报错），会导致用户
// 看到裸 "{xxx}" 而不报错，必须在测试期用占位符集合比对钉住。
func TestFourLocalesSharePlaceholders(t *testing.T) {
	tables := map[string]map[string]string{}
	for _, locale := range supportedLocales {
		tables[locale] = loadLocale(t, locale)
	}
	base := tables["zh-CN"]
	for key, zhText := range base {
		want := placeholders(zhText)
		for _, locale := range supportedLocales {
			if locale == "zh-CN" {
				continue
			}
			text, ok := tables[locale][key]
			if !ok {
				continue // 缺失已由 TestFourLocalesShareExactKeySet 报告，此处不重复报错。
			}
			got := placeholders(text)
			if !equalStrings(got, want) {
				t.Errorf("key %q 占位符不一致: zh-CN=%v %s=%v", key, want, locale, got)
			}
		}
	}
}

// TestTReturnsEnglishForEnUS 冒烟验证两级查找链「① 命中请求 locale」分支：en-US 请求应返回
// 英文文案，而不是静默回落到默认 locale（zh-CN）——防止语言表加载错位或键漂移。
func TestTReturnsEnglishForEnUS(t *testing.T) {
	const key = "write.common.no_host"
	got := T("en-US", key, nil)
	if got == "" {
		t.Fatalf("en-US %s 不应为空", key)
	}
	if got == T(DefaultLocale(), key, nil) {
		t.Fatalf("en-US 文案与默认(zh-CN)文案相同，怀疑未命中 en-US 语言表: %q", got)
	}
}

// TestTInterpolatesParams 验证带占位符的 key 在非默认 locale 下也能正确插值（而不仅仅是
// 默认 locale 路径）。
func TestTInterpolatesParams(t *testing.T) {
	got := T("en-US", "write.common.failed", map[string]any{"status": "failed"})
	want := "Write failed; the host returned status failed"
	if got != want {
		t.Fatalf("T interpolate = %q, want %q", got, want)
	}
}

// TestDefaultLocaleIsZhCN 锁定默认 locale（T(loc,...) 传入空/未知 loc 时的回落目标——宿主
// 未透传 TriggerContext.locale / ValidateParamsRequest.locale 时的行为）。
func TestDefaultLocaleIsZhCN(t *testing.T) {
	if DefaultLocale() != "zh-CN" {
		t.Fatalf("DefaultLocale() = %q, want zh-CN", DefaultLocale())
	}
}

// TestUnknownLocaleFallsBackToDefault 未知 locale（如宿主透传的怪异值）应回落默认语言，
// 而不是返回空串（两级查找链的第②级）。
func TestUnknownLocaleFallsBackToDefault(t *testing.T) {
	const key = "write.common.no_host"
	if got := T("xx-XX", key, nil); got != T(DefaultLocale(), key, nil) {
		t.Fatalf("未知 locale 应回落默认语言: got=%q want=%q", got, T(DefaultLocale(), key, nil))
	}
}
