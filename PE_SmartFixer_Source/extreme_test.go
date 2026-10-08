package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// 一、黑匣子真实磁盘 I/O（原子写入 / BOM / 损坏 / 不存在）
// ============================================================

func TestWriteFixMemoryRealDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pe_fix_memory.json")
	mem := FixMemory{LastRepairTime: "2026-08-24 10:30:00", DiagnoseCount: 2, LastAction: "REG|iaStorV"}

	if err := writeFixMemory(path, mem); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 落盘内容必须与规范格式逐字符一致，且不含 BOM
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"last_repair_time":"2026-08-24 10:30:00","diagnose_count":2,"last_action":"REG|iaStorV"}`
	if string(raw) != want {
		t.Errorf("落盘内容不符规范\n got: %s\nwant: %s", raw, want)
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		t.Error("落盘文件不应带 UTF-8 BOM")
	}
	// 临时文件必须已被清理（原子化保证）
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("原子写入后不应残留 .tmp 文件")
	}
	// 回读一致
	back, err := loadFixMemory(path)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if back != mem {
		t.Errorf("回读不一致: %+v != %+v", back, mem)
	}
}

func TestWriteFixMemoryOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	_ = writeFixMemory(path, FixMemory{LastRepairTime: "2026-08-24 10:00:00", DiagnoseCount: 1, LastAction: "A"})
	_ = writeFixMemory(path, FixMemory{LastRepairTime: "2026-08-24 11:00:00", DiagnoseCount: 9, LastAction: "B"})
	back, err := loadFixMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.DiagnoseCount != 9 || back.LastAction != "B" {
		t.Errorf("覆盖写入失败: %+v", back)
	}
}

func TestWriteFixMemoryBadPath(t *testing.T) {
	// 非法路径必须返回错误，且绝不 panic
	err := writeFixMemory(filepath.Join("Z:", "不存在的盘", "m.json"), FixMemory{})
	if err == nil {
		t.Error("非法路径应返回错误")
	}
}

func TestLoadFixMemoryMissing(t *testing.T) {
	if _, err := loadFixMemory(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("文件不存在应返回错误")
	}
}

func TestLoadFixMemoryCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte("{oops"), 0644)
	if _, err := loadFixMemory(path); err == nil {
		t.Error("损坏内容应返回错误")
	}
}

func TestLoadFixMemoryWithBOM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.json")
	body := `{"last_repair_time":"2026-08-24 10:30:00","diagnose_count":3,"last_action":"X"}`
	_ = os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...), 0644)
	mem, err := loadFixMemory(path)
	if err != nil {
		t.Fatalf("带 BOM 应能解析: %v", err)
	}
	if mem.DiagnoseCount != 3 {
		t.Errorf("解析错误: %+v", mem)
	}
}

func TestLoadFixMemoryUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "extra.json")
	// 未知字段 + 乱序 + 空白，都应容忍（向前兼容）
	body := "{\n  \"future_field\": 123,\n  \"last_action\": \"REG|a\",\n  \"diagnose_count\": 2,\n  \"last_repair_time\": \"2026-08-24 10:30:00\"\n}"
	_ = os.WriteFile(path, []byte(body), 0644)
	mem, err := loadFixMemory(path)
	if err != nil {
		t.Fatalf("未知字段应被忽略: %v", err)
	}
	if mem.DiagnoseCount != 2 || mem.LastAction != "REG|a" {
		t.Errorf("解析错误: %+v", mem)
	}
}

func TestLoadFixMemoryWrongType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrong.json")
	_ = os.WriteFile(path, []byte(`{"diagnose_count":"三","last_action":"x","last_repair_time":"2026-08-24 10:30:00"}`), 0644)
	if _, err := loadFixMemory(path); err == nil {
		t.Error("类型不符应返回错误")
	}
}

// ============================================================
// 二、病历本合并：子串误判 / 特殊字符 / 超长 / 幂等
// ============================================================

func TestContainsActionExactMatch(t *testing.T) {
	// 子串绝不能算"已存在"，否则会漏记新工单
	if containsAction("REG|iaStorV", "REG|iaStor") {
		t.Error("子串不应被判为已存在（会漏记工单）")
	}
	if containsAction("REG|iaStor", "REG|iaStorV") {
		t.Error("超串不应被判为已存在")
	}
	if !containsAction("REG|iaStorV;REG|stornvme", "REG|stornvme") {
		t.Error("精确匹配应成功")
	}
	if !containsAction(" REG|iaStorV ; REG|stornvme ", "REG|stornvme") {
		t.Error("带空白应能匹配")
	}
	if containsAction("", "REG|a") {
		t.Error("空病历本不应匹配")
	}
}

func TestMergeFixMemorySubstringSafety(t *testing.T) {
	// 回归：旧实现用 strings.Contains，会把 REG|iaStor 误判为已存在于 REG|iaStorV
	mem := FixMemory{DiagnoseCount: 1, LastAction: "REG|iaStorV"}
	mem = mergeFixMemory(mem, "REG|iaStor", time.Now())
	if mem.LastAction != "REG|iaStorV;REG|iaStor" {
		t.Errorf("相似服务名必须作为新工单追加，实际 %q", mem.LastAction)
	}
}

func TestMergeFixMemoryEmptyAction(t *testing.T) {
	now := time.Now()
	mem := mergeFixMemory(FixMemory{}, "", now)
	if mem.DiagnoseCount != 1 {
		t.Errorf("空动作仍应计数，实际 %d", mem.DiagnoseCount)
	}
	if mem.LastAction != "" {
		t.Errorf("空动作不应写入病历，实际 %q", mem.LastAction)
	}
}

func TestMergeFixMemoryThreeRounds(t *testing.T) {
	now := time.Now()
	mem := FixMemory{}
	for i, act := range []string{"REG|a", "REG|b", "REG|c"} {
		mem = mergeFixMemory(mem, act, now)
		if mem.DiagnoseCount != i+1 {
			t.Errorf("第%d轮计数错误: %d", i+1, mem.DiagnoseCount)
		}
	}
	if mem.DiagnoseCount != 3 {
		t.Fatalf("三轮后应为3，实际 %d", mem.DiagnoseCount)
	}
	if d := decideHook(mem, now); d != hookFuse {
		t.Error("三轮失败后应触发熔断")
	}
}

func TestFixMemorySpecialCharsRoundTrip(t *testing.T) {
	// 工单里可能出现引号/反斜杠（如 FILE|a.sys|drivers\etc），必须能安全往返
	now := time.Now()
	nasty := `FILE|we"ird.sys|drivers\sub\dir`
	mem := mergeFixMemory(FixMemory{}, nasty, now)

	out, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Fatalf("产出的 JSON 非法: %s", out)
	}
	back, err := parseFixMemory(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.LastAction != nasty {
		t.Errorf("特殊字符往返失败\n got: %q\nwant: %q", back.LastAction, nasty)
	}
}

func TestFixMemoryVeryLongAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.json")
	long := strings.Repeat("REG|svc;", 2000) + "REG|last"
	mem := mergeFixMemory(FixMemory{}, long, time.Now())
	if err := writeFixMemory(path, mem); err != nil {
		t.Fatalf("超长病历经写入失败: %v", err)
	}
	back, err := loadFixMemory(path)
	if err != nil {
		t.Fatalf("超长病历经读取失败: %v", err)
	}
	if back.LastAction != long {
		t.Error("超长病历往返不一致")
	}
}

func TestMergeFixMemoryIdempotentTimestampFormat(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	mem := mergeFixMemory(FixMemory{}, "REG|a", now)
	if mem.LastRepairTime != "2026-01-02 03:04:05" {
		t.Errorf("时间格式必须为 YYYY-MM-DD HH:MM:SS，实际 %q", mem.LastRepairTime)
	}
	// 需要能被 decideHook 反解回来
	if d := decideHook(mem, now.Add(time.Minute)); d != hookRollback {
		t.Errorf("刚写入的记录应判定为回滚分支，实际 %v", d)
	}
}

// ============================================================
// 三、钩子判定：时钟偏移 / 非法时间戳 / 极端计数
// ============================================================

func TestDecideHookFutureTimestamp(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	future := FixMemory{
		LastRepairTime: now.Add(48 * time.Hour).Format("2006-01-02 15:04:05"),
		DiagnoseCount:  5,
	}
	// 主板电池没电导致时钟跳到未来：时间差为负，不满足 >24h，走熔断分支（保守安全）
	if d := decideHook(future, now); d != hookFuse {
		t.Errorf("未来时间戳应按未过期处理（保守熔断），实际 %v", d)
	}
}

func TestDecideHookBadTimestampFormats(t *testing.T) {
	now := time.Now()
	for _, bad := range []string{"2026/08/24 10:30:00", "2026-08-24", "not a time", "2026-08-24 10:30", ""} {
		mem := FixMemory{LastRepairTime: bad, DiagnoseCount: 9}
		if d := decideHook(mem, now); d != hookClean {
			t.Errorf("非法时间戳 %q 应判定初诊，实际 %v", bad, d)
		}
	}
}

func TestDecideHookZeroAndNegativeCount(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	for _, cnt := range []int{0, -1, -100} {
		mem := FixMemory{LastRepairTime: now.Add(-time.Hour).Format("2006-01-02 15:04:05"), DiagnoseCount: cnt}
		if d := decideHook(mem, now); d != hookRollback {
			t.Errorf("计数 %d 应走回滚分支，实际 %v", cnt, d)
		}
	}
}

func TestDecideHookJustUnder24h(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	mem := FixMemory{
		LastRepairTime: now.Add(-23*time.Hour - 59*time.Minute).Format("2006-01-02 15:04:05"),
		DiagnoseCount:  3,
	}
	if d := decideHook(mem, now); d != hookFuse {
		t.Errorf("略小于24小时应熔断，实际 %v", d)
	}
}

// ============================================================
// 四、回滚路径改写：大小写 / 多服务 / 空内容 / 防御校验
// ============================================================

func TestRewriteSafeNormal(t *testing.T) {
	in := "Windows Registry Editor Version 5.00\n\n[HKEY_LOCAL_MACHINE\\SYSTEM\\ControlSet001\\Services\\iaStorV]\n\"Start\"=dword:00000003\n"
	out, err := rewriteRegForOfflineSafe(in)
	if err != nil {
		t.Fatalf("正常备份不应报错: %v", err)
	}
	if !strings.Contains(out, `SysTemp\ControlSet001\Services\iaStorV`) {
		t.Errorf("改写失败: %s", out)
	}
}

func TestRewriteSafeRejectsLowercaseLeftover(t *testing.T) {
	// 小写在线路径无法被 ReplaceAll 命中 → 防御校验必须报错中止（而非静默写错注册表）
	in := "[hkey_local_machine\\system\\ControlSet001\\Services\\x]\n"
	if _, err := rewriteRegForOfflineSafe(in); err == nil {
		t.Error("残留在线路径时必须报错中止")
	} else if !strings.Contains(err.Error(), "路径改写失败") {
		t.Errorf("错误信息不明确: %v", err)
	}
}

func TestRewriteSafeEmptyContent(t *testing.T) {
	out, err := rewriteRegForOfflineSafe("")
	if err != nil {
		t.Errorf("空内容不应报错: %v", err)
	}
	if out != "" {
		t.Errorf("空内容应原样返回，实际 %q", out)
	}
}

func TestRewriteSafeMultiService(t *testing.T) {
	in := "[HKEY_LOCAL_MACHINE\\SYSTEM\\ControlSet001\\Services\\a]\n" +
		"[HKEY_LOCAL_MACHINE\\SYSTEM\\ControlSet001\\Services\\b]\n"
	out, err := rewriteRegForOfflineSafe(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "SysTemp") != 2 {
		t.Errorf("多服务应全部改写，实际: %s", out)
	}
	if strings.Contains(out, `SYSTEM\ControlSet`) {
		t.Errorf("仍有残留: %s", out)
	}
}

// ============================================================
// 五、指令解析极限：空白 / 大小写 / 空段 / 超长 / 注入
// ============================================================

func TestParseDocActionWhitespaceAndCase(t *testing.T) {
	cases := []struct{ in, wantPrefix string }{
		{"  REG | iaStorV  ", "REG"},
		{"reg|iaStorV", "REG"},
		{"ReG|iaStorV", "REG"},
		{"file|a.sys|drivers", "FILE"},
		{"  FILE | a.sys | drivers ", "FILE"},
	}
	for _, c := range cases {
		p, err := parseDocAction(c.in)
		if err != nil {
			t.Errorf("%q 应通过，实际错误: %v", c.in, err)
			continue
		}
		if p != c.wantPrefix {
			t.Errorf("%q 前缀应为 %s，实际 %s", c.in, c.wantPrefix, p)
		}
	}
}

func TestParseDocActionInjectionVariants(t *testing.T) {
	// 各种命令注入尝试都必须被拦下
	bad := []string{
		"REG|svc&calc",
		"REG|svc|calc",
		"REG|svc;calc",
		"REG|svc>out.txt",
		"REG|svc<in.txt",
		"REG|svc^&calc",
		"REG|svc%TEMP%",
		"REG|svc\"quote",
		"REG|svc'quote",
		"REG|svc`tick",
		"REG|svc$(whoami)",
		"REG|中文服务",
		"REG|svc name",
		"REG|",
	}
	for _, s := range bad {
		if _, err := parseDocAction(s); err == nil {
			t.Errorf("危险/非法指令未被拦截: %q", s)
		}
	}
}

func TestParseDocActionVeryLongServiceName(t *testing.T) {
	long := "REG|" + strings.Repeat("a", 5000)
	// 超长但字符合法 → 允许（后续 reg add 会自然失败）
	if _, err := parseDocAction(long); err != nil {
		t.Errorf("合法字符的超长服务名不应在解析层被拒: %v", err)
	}
}

func TestParseDocActionFilePathForms(t *testing.T) {
	ok := []string{
		"FILE|netkvm.inf|INF",
		"FILE|a.sys|drivers",
		"FILE|a.sys|drivers\\sub\\deep",
		"FILE|a.sys|.",
	}
	for _, s := range ok {
		if _, err := parseDocAction(s); err != nil {
			t.Errorf("%q 应通过: %v", s, err)
		}
	}
	bad := []string{
		"FILE|..\\evil.exe|drivers",
		"FILE|a/b.sys|drivers",
		"FILE|a:b.sys|drivers",
		"FILE|*.sys|drivers",
		"FILE||drivers",
		"FILE|a.sys",
	}
	for _, s := range bad {
		if _, err := parseDocAction(s); err == nil {
			t.Errorf("%q 应被拒绝", s)
		}
	}
}

// ============================================================
// 六、JSON 转义修复：幂等性 / 多重非法 / 边界
// ============================================================

func TestFixJSONEscapesIdempotent(t *testing.T) {
	inputs := []string{
		`"SYSTEM32\DRIVERS"`,
		`"a\nb\tc"`,
		`"x\Dy\Ez"`,
		`"trailing\`,
		`"\\\\"`,
		`""`,
	}
	for _, in := range inputs {
		once := fixJSONEscapes(in)
		twice := fixJSONEscapes(once)
		if once != twice {
			t.Errorf("非幂等: %q → %q → %q", in, once, twice)
		}
	}
}

func TestFixJSONEscapesMultipleIllegal(t *testing.T) {
	got := fixJSONEscapes(`"C:\Windows\System32\drivers\a.sys"`)
	want := `"C:\\Windows\\System32\\drivers\\a.sys"`
	if got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
}

func TestRenderJSONWithWindowsPaths(t *testing.T) {
	// 端到端：AI 真实可能返回的 Windows 路径 JSON 必须能解析
	raw := `{"risk_level":"SAFE","user_desc":"驱动路径 C:\Windows\System32\drivers 损坏","doc_action":"FILE|netkvm.inf|drivers"}`
	fixed := fixJSONEscapes(raw)
	var pr PresenterResponse
	if err := json.Unmarshal([]byte(fixed), &pr); err != nil {
		t.Fatalf("解析失败: %v\nfixed=%s", err, fixed)
	}
	if !strings.Contains(pr.UserDesc, `C:\Windows\System32\drivers`) {
		t.Errorf("路径被破坏: %q", pr.UserDesc)
	}
	if pr.DocAction != "FILE|netkvm.inf|drivers" {
		t.Errorf("工单被破坏: %q", pr.DocAction)
	}
}

// ============================================================
// 七、提示词注入：特殊字符 / 超长 / 空
// ============================================================

func TestBuildSystemPromptPercentSafe(t *testing.T) {
	// 历史工单里含 % 时，Sprintf 不得产生 %!x 之类的错误输出
	got := buildSystemPrompt("BASE", "REG|svc%TEMP%")
	if strings.Contains(got, "%!") {
		t.Errorf("百分号被误当格式符: %s", got)
	}
	if !strings.Contains(got, "REG|svc%TEMP%") {
		t.Errorf("历史工单未被原样注入: %s", got)
	}
}

func TestBuildSystemPromptEmptyBase(t *testing.T) {
	got := buildSystemPrompt("", "REG|a")
	if !strings.Contains(got, "【历史重症监护记录】") {
		t.Error("空基础提示词也应注入历史约束")
	}
}

func TestBuildSystemPromptVeryLong(t *testing.T) {
	long := strings.Repeat("REG|service_name;", 500)
	got := buildSystemPrompt("BASE", long)
	if !strings.HasSuffix(got, "给出全新的备用工单动作！") {
		t.Error("超长历史下提示词结尾应保持完整")
	}
	if len(got) < len(long) {
		t.Error("历史工单丢失")
	}
}

// ============================================================
// 八、文本清洗极限
// ============================================================

func TestCleanTextWeirdInputs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"\n\n\n", ""},
		{"[截图路径: C:\\a.png", ""},     // 未闭合标签
		{"[截图路径: ]", ""},             // 空标签
		{"   \t  \n  ", ""},          // 纯空白
		{"0x0000007B", "0x0000007B"}, // 正常保留
	}
	for _, c := range cases {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("cleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractImagePathsWeirdInputs(t *testing.T) {
	if got := extractImagePaths(""); len(got) != 0 {
		t.Errorf("空文本应无路径，实际 %v", got)
	}
	if got := extractImagePaths("[截图路径: C:\\a.png]"); len(got) != 1 || got[0] != "C:\\a.png" {
		t.Errorf("单路径解析失败: %v", got)
	}
	multi := "[截图路径: C:\\a.png\n[截图路径: D:\\b.jpg]"
	got := extractImagePaths(multi)
	if len(got) != 2 {
		t.Errorf("多路径解析失败: %v", got)
	}
}
