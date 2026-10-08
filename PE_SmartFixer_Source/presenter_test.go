package main

import (
	"encoding/json"
	"testing"
)

func TestFixJSONEscapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"非法转义\\D", `"SYSTEM32\DRIVERS"`, `"SYSTEM32\\DRIVERS"`},
		{"合法转义保留", `"a\nb\tc"`, `"a\nb\tc"`},
		{"合法\\u保留", `"\u4e2d\u6587"`, `"\u4e2d\u6587"`},
		{"普通文本不变", `"hello"`, `"hello"`},
		{"反斜杠在末尾", `"abc\`, `"abc\\`},
		{"双反斜杠保留", `"a\\b"`, `"a\\b"`},
	}
	for _, c := range cases {
		got := fixJSONEscapes(c.in)
		if got != c.want {
			t.Errorf("%s: fixJSONEscapes(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRenderJSONWithBadEscape(t *testing.T) {
	// 模拟 AI 返回含非法转义的 JSON
	raw := `{
"risk_level": "CRITICAL",
"user_desc": "蓝屏代码0x00000074通常表示内存管理错误。",
"doc_action": "REG|MEMORY_FILE|memory.sys|SYSTEM32\DRIVERS"
}`
	// 修复后应能正常解析
	fixed := fixJSONEscapes(raw)
	var pr PresenterResponse
	if err := json.Unmarshal([]byte(fixed), &pr); err != nil {
		t.Fatalf("解析修复后的 JSON 失败: %v\nfixed=%s", err, fixed)
	}
	if pr.RiskLevel != "CRITICAL" {
		t.Errorf("RiskLevel = %q, want CRITICAL", pr.RiskLevel)
	}
	if pr.DocAction != "REG|MEMORY_FILE|memory.sys|SYSTEM32\\DRIVERS" {
		t.Errorf("DocAction = %q", pr.DocAction)
	}
}
