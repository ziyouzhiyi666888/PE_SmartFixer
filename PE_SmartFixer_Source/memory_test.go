package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------- 黑匣子：计数与病历本合并 ----------

func TestMergeFixMemoryFirstRound(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 30, 0, 0, time.Local)
	mem := mergeFixMemory(FixMemory{}, "REG|iaStorV", now)

	if mem.DiagnoseCount != 1 {
		t.Errorf("首次记录 diagnose_count 应为 1，实际 %d", mem.DiagnoseCount)
	}
	if mem.LastAction != "REG|iaStorV" {
		t.Errorf("首次记录 last_action 应为 REG|iaStorV，实际 %q", mem.LastAction)
	}
	if mem.LastRepairTime != "2026-08-24 10:30:00" {
		t.Errorf("时间格式应为 YYYY-MM-DD HH:MM:SS，实际 %q", mem.LastRepairTime)
	}
}

func TestMergeFixMemoryAccumulate(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 30, 0, 0, time.Local)
	mem := FixMemory{LastRepairTime: "2026-08-24 09:00:00", DiagnoseCount: 1, LastAction: "REG|iaStorV"}
	mem = mergeFixMemory(mem, "REG|stornvme", now)

	if mem.DiagnoseCount != 2 {
		t.Errorf("累加后 diagnose_count 应为 2，实际 %d", mem.DiagnoseCount)
	}
	if mem.LastAction != "REG|iaStorV;REG|stornvme" {
		t.Errorf("新工单应以分号追加，实际 %q", mem.LastAction)
	}
}

func TestMergeFixMemoryDedupe(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 30, 0, 0, time.Local)
	mem := FixMemory{DiagnoseCount: 1, LastAction: "REG|iaStorV;REG|stornvme"}
	mem = mergeFixMemory(mem, "REG|iaStorV", now)

	if mem.LastAction != "REG|iaStorV;REG|stornvme" {
		t.Errorf("重复动作不应重复追加，实际 %q", mem.LastAction)
	}
	if mem.DiagnoseCount != 2 {
		t.Errorf("计数仍应+1，实际 %d", mem.DiagnoseCount)
	}
}

func TestMemoryJSONFormat(t *testing.T) {
	mem := FixMemory{
		LastRepairTime: "2026-08-24 10:30:00",
		DiagnoseCount:  2,
		LastAction:     "REG|iaStorV;REG|stornvme",
	}
	out, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	want := `{"last_repair_time":"2026-08-24 10:30:00","diagnose_count":2,"last_action":"REG|iaStorV;REG|stornvme"}`
	if got != want {
		t.Errorf("JSON 格式不符规范\n got: %s\nwant: %s", got, want)
	}
	// 反向解析
	var back FixMemory
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.DiagnoseCount != 2 || back.LastAction != mem.LastAction {
		t.Errorf("往返解析不一致: %+v", back)
	}
}

func TestFixMemoryPath(t *testing.T) {
	old := targetDrive
	defer func() { targetDrive = old }()
	targetDrive = "C"
	if got := fixMemoryPath(); got != "C:\\Windows\\pe_fix_memory.json" {
		t.Errorf("黑匣子路径错误: %q", got)
	}
}

func TestParseFixMemoryWithBOM(t *testing.T) {
	body := `{"last_repair_time":"2026-08-24 10:30:00","diagnose_count":3,"last_action":"REG|iaStorV"}`
	// 无 BOM
	mem, err := parseFixMemory([]byte(body))
	if err != nil {
		t.Fatalf("无BOM解析失败: %v", err)
	}
	if mem.DiagnoseCount != 3 || mem.LastAction != "REG|iaStorV" {
		t.Errorf("解析结果错误: %+v", mem)
	}
	// 带 UTF-8 BOM（记事本常见）——不应被判为损坏
	memB, errB := parseFixMemory(append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...))
	if errB != nil {
		t.Fatalf("带BOM解析失败（会误删病历）: %v", errB)
	}
	if memB.DiagnoseCount != 3 {
		t.Errorf("带BOM解析结果错误: %+v", memB)
	}
	// 真正损坏的内容必须报错
	if _, bad := parseFixMemory([]byte("{not json")); bad == nil {
		t.Error("损坏内容应报错")
	}
}

// ---------- 前置钩子判定 ----------

func TestDecideHookCleanWhenNoTime(t *testing.T) {
	mem := FixMemory{LastRepairTime: "", DiagnoseCount: 5}
	if d := decideHook(mem, time.Now()); d != hookClean {
		t.Errorf("时间戳为空应判定初诊，实际 %v", d)
	}
}

func TestDecideHookCleanWhenExpired(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	mem := FixMemory{
		LastRepairTime: now.Add(-25 * time.Hour).Format("2006-01-02 15:04:05"),
		DiagnoseCount:  3,
	}
	if d := decideHook(mem, now); d != hookClean {
		t.Errorf("超过24小时应判定上轮已修复成功(初诊)，实际 %v", d)
	}
}

func TestDecideHookFuseAtThree(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	for _, cnt := range []int{3, 4, 10} {
		mem := FixMemory{
			LastRepairTime: now.Add(-1 * time.Hour).Format("2006-01-02 15:04:05"),
			DiagnoseCount:  cnt,
		}
		if d := decideHook(mem, now); d != hookFuse {
			t.Errorf("24小时内 %d 次失败应触发硬熔断，实际 %v", cnt, d)
		}
	}
}

func TestDecideHookRollbackUnderThree(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	for _, cnt := range []int{1, 2} {
		mem := FixMemory{
			LastRepairTime: now.Add(-2 * time.Hour).Format("2006-01-02 15:04:05"),
			DiagnoseCount:  cnt,
		}
		if d := decideHook(mem, now); d != hookRollback {
			t.Errorf("24小时内 %d 次失败应回滚+唤醒，实际 %v", cnt, d)
		}
	}
}

func TestDecideHookBoundaryExactly24h(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local)
	// 恰好 24 小时 → 不算过期（>24h 才过期）
	mem := FixMemory{
		LastRepairTime: now.Add(-24 * time.Hour).Format("2006-01-02 15:04:05"),
		DiagnoseCount:  3,
	}
	if d := decideHook(mem, now); d != hookFuse {
		t.Errorf("恰好24小时仍应熔断，实际 %v", d)
	}
}

// ---------- 负向约束提示词 ----------

func TestBuildSystemPromptNoHistory(t *testing.T) {
	base := "基础提示词"
	if got := buildSystemPrompt(base, ""); got != base {
		t.Errorf("无历史失败时不应改动提示词，实际 %q", got)
	}
}

func TestBuildSystemPromptWithHistory(t *testing.T) {
	base := "你是一名Windows系统维修专家。"
	got := buildSystemPrompt(base, "REG|iaStorV;REG|stornvme")

	if !strings.HasPrefix(got, base) {
		t.Error("应保留原始系统提示词")
	}
	if !strings.Contains(got, "【历史重症监护记录】") {
		t.Error("缺失历史记录段")
	}
	if !strings.Contains(got, "REG|iaStorV;REG|stornvme") {
		t.Error("应注入全部历史失败路径")
	}
	if !strings.Contains(got, "严格避开") {
		t.Error("缺失负向强约束语义")
	}
	// 原静态配置不得被污染（函数式拼接，base 不变）
	if strings.Contains(base, "历史重症监护") {
		t.Error("原始提示词被就地修改了")
	}
}

func TestNegativePromptContainsForbiddenPaths(t *testing.T) {
	old := globalFailedAction
	defer func() { globalFailedAction = old }()

	globalFailedAction = "REG|iaStorV;FILE|netkvm.inf"
	if globalFailedAction == "" {
		t.Fatal("全局失败工单不应为空")
	}
	got := buildSystemPrompt("BASE", globalFailedAction)
	if !strings.Contains(got, "REG|iaStorV") || !strings.Contains(got, "FILE|netkvm.inf") {
		t.Errorf("负向提示词应包含全部历史路径: %s", got)
	}
}
