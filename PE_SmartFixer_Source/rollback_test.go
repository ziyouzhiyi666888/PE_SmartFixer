package main

import (
	"strings"
	"testing"
)

// 验证"路径改写"这个关键修复：把在线路径 SYSTEM 改写为离线挂载点 SysTemp
// 这是文档原方案（直接 reg import）失效的根本原因所在。
func TestRewriteRegForOffline(t *testing.T) {
	// 模拟工具真实生成的备份文件内容（与 registry.go 写出的格式一致）
	backup := `Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SYSTEM\ControlSet001\Services\iaStorV]
"Start"=dword:00000003
`
	got := rewriteRegForOffline(backup)

	if !strings.Contains(got, `[HKEY_LOCAL_MACHINE\SysTemp\ControlSet001\Services\iaStorV]`) {
		t.Errorf("未正确改写为离线挂载路径:\n%s", got)
	}
	if strings.Contains(got, `[HKEY_LOCAL_MACHINE\SYSTEM\`) {
		t.Errorf("仍残留在线路径（会写到 PE 自己的注册表）:\n%s", got)
	}
	// 值内容必须原样保留
	if !strings.Contains(got, `"Start"=dword:00000003`) {
		t.Errorf("键值内容被破坏:\n%s", got)
	}
	// 头部声明保留
	if !strings.HasPrefix(got, "Windows Registry Editor Version 5.00") {
		t.Errorf("reg 文件头被破坏:\n%s", got)
	}
}

func TestRewriteRegForOfflineMultipleServices(t *testing.T) {
	// 多个服务的备份各自独立改写
	a := rewriteRegForOffline(`[HKEY_LOCAL_MACHINE\SYSTEM\ControlSet001\Services\stornvme]`)
	if !strings.Contains(a, `SysTemp\ControlSet001\Services\stornvme`) {
		t.Errorf("多服务改写失败: %s", a)
	}
	// 非 SYSTEM 路径不应被误改
	b := rewriteRegForOffline(`[HKEY_LOCAL_MACHINE\SOFTWARE\Test]`)
	if strings.Contains(b, "SysTemp") {
		t.Errorf("非 SYSTEM 路径被误改: %s", b)
	}
}
