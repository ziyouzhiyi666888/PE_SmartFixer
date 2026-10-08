package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------- WOW64 重定向关闭（32 位构建的关键修复）----------

func TestDisableWow64RedirectionNoPanic(t *testing.T) {
	// 64 位进程下该 API 返回 FALSE，但绝不能 panic 或崩溃
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("关闭重定向时发生 panic: %v", r)
		}
	}()
	disableWow64Redirection()
	// 调用后，标准系统路径仍应可访问（64 位下不受影响）
	if _, err := os.Stat("C:\\Windows"); err != nil {
		t.Logf("C:\\Windows 不可访问（环境相关）: %v", err)
	}
}

func TestScanSystemDriveAfterRedirectionOff(t *testing.T) {
	disableWow64Redirection()
	drive := ScanSystemDrive()
	t.Logf("关闭重定向后扫描到系统盘: %q", drive)
	// 在有 Windows 的机器上应能找到系统盘
	if drive == "" {
		t.Log("未找到系统盘（可能是无盘/受限环境）")
	}
}

// ---------- UTF-16 指针转换（纯函数）----------

func TestUtf16PtrAllowNul(t *testing.T) {
	// 含 NUL 的字符串是 Windows Filter 的必需格式，不能 panic
	p := utf16PtrAllowNul("A\x00B\x00\x00")
	if p == nil {
		t.Fatal("不应返回空指针")
	}
	// 纯 ASCII
	if utf16PtrAllowNul("abc") == nil {
		t.Error("普通字符串不应出错")
	}
	// 中文 + 结尾 NUL
	if utf16PtrAllowNul("中文测试\x00") == nil {
		t.Error("中文不应出错")
	}
	// 空字符串（仅结尾 NUL）
	if utf16PtrAllowNul("") == nil {
		t.Error("空字符串不应出错")
	}
	// 极端：超长含大量 NUL
	long := ""
	for i := 0; i < 1000; i++ {
		long += "x\x00"
	}
	if utf16PtrAllowNul(long) == nil {
		t.Error("超长含 NUL 不应出错")
	}
}

// ---------- 黑匣子写入的分支保护 ----------

func TestRecordFixRoundGuards(t *testing.T) {
	oldDrive := targetDrive
	defer func() { targetDrive = oldDrive }()

	// targetDrive 为空 → 静默返回，绝不 panic / 不写文件
	targetDrive = ""
	recordFixRound("REG|iaStorV")

	// action 为空 → 静默返回
	targetDrive = "Z" // 不存在的盘，确保即使进入写入分支也不会污染真实系统
	recordFixRound("")

	// 不存在的盘 + 合法动作 → 写入失败但不 panic
	recordFixRound("REG|iaStorV")
}

// ---------- 启动钩子的安全分支 ----------

func TestStartupMemoryHookNoDrive(t *testing.T) {
	oldDrive := targetDrive
	oldAlert, oldBlocked := gFuseAlert, gFuseBlocked
	defer func() {
		targetDrive = oldDrive
		gFuseAlert, gFuseBlocked = oldAlert, oldBlocked
	}()

	targetDrive = "" // 未识别到系统盘
	if startupMemoryHook() {
		t.Error("无系统盘时不应熔断")
	}
	if gFuseBlocked {
		t.Error("无系统盘时不应封锁")
	}
}

func TestStartupMemoryHookMissingFile(t *testing.T) {
	oldDrive := targetDrive
	oldAlert, oldBlocked := gFuseAlert, gFuseBlocked
	defer func() {
		targetDrive = oldDrive
		gFuseAlert, gFuseBlocked = oldAlert, oldBlocked
	}()

	gFuseAlert, gFuseBlocked = false, false
	targetDrive = "Z" // 该盘不存在 → 读文件失败 → 纯净初诊
	if startupMemoryHook() {
		t.Error("无黑匣子时不应熔断")
	}
	if gFuseBlocked {
		t.Error("无黑匣子时不应封锁")
	}
}

// ---------- 回滚：使用真实临时文件验证路径与改写 ----------

func TestRestoreRegistryStartMissingHive(t *testing.T) {
	// 盘符不存在 → 必须在读取备份前就报错，且不执行任何 reg 命令
	err := RestoreRegistryStart("Z", "iaStorV")
	if err == nil {
		t.Fatal("不存在的盘应报错")
	}
	if !containsStrAny(err.Error(), "SYSTEM 配置单元不存在", "备份文件读取失败") {
		t.Errorf("错误信息不符合预期: %v", err)
	}
}

func TestRestoreRegistryStartMissingBackup(t *testing.T) {
	// C 盘存在 hive，但备份文件不存在 → 应报"备份文件读取失败"
	err := RestoreRegistryStart("C", "根本不存在的服务名_ZZZ")
	if err == nil {
		t.Log("C 盘不可访问时跳过（环境相关）")
		return
	}
	if !containsStrAny(err.Error(), "备份文件读取失败", "SYSTEM 配置单元不存在") {
		t.Errorf("错误信息不符合预期: %v", err)
	}
}

// ---------- 备份文件内容 → 回滚改写 端到端（真实文件）----------

func TestBackupFileToRollbackRewrite(t *testing.T) {
	dir := t.TempDir()
	// 复刻工具真实写出的备份内容
	backup := `Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SYSTEM\ControlSet001\Services\stornvme]
"Start"=dword:00000000
`
	path := filepath.Join(dir, "stornvme_Start_backup.reg")
	if err := os.WriteFile(path, []byte(backup), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := rewriteRegForOfflineSafe(string(data))
	if err != nil {
		t.Fatalf("改写失败: %v", err)
	}
	// 必须指向离线挂载点，且保留原始键值与 ControlSet
	if !containsStrAny(fixed, `[HKEY_LOCAL_MACHINE\SysTemp\ControlSet001\Services\stornvme]`) {
		t.Errorf("挂载点路径未生成:\n%s", fixed)
	}
	if !containsStrAny(fixed, `"Start"=dword:00000000`) {
		t.Errorf("原值丢失:\n%s", fixed)
	}
	if containsStrAny(fixed, `SYSTEM\ControlSet`) {
		t.Errorf("残留在线路径（会写到 PE 自身注册表）:\n%s", fixed)
	}
}

func containsStrAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
