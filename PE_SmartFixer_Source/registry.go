package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------- v1.1 硬盘黑匣子（长期记忆） ----------

type FixMemory struct {
	LastRepairTime string `json:"last_repair_time"`
	DiagnoseCount  int    `json:"diagnose_count"`
	LastAction     string `json:"last_action"`
}

// fixMemoryPath 黑匣子绝对路径：目标盘 Windows\pe_fix_memory.json
func fixMemoryPath() string {
	return targetDrive + ":\\Windows\\pe_fix_memory.json"
}

// parseFixMemory 解析黑匣子内容（容忍 UTF-8 BOM，避免误判损坏丢失病历）
func parseFixMemory(data []byte) (FixMemory, error) {
	var mem FixMemory
	err := json.Unmarshal([]byte(strings.TrimPrefix(string(data), "\ufeff")), &mem)
	return mem, err
}

// loadFixMemory 从指定路径读取黑匣子（容忍 BOM）
func loadFixMemory(path string) (FixMemory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FixMemory{}, err
	}
	return parseFixMemory(data)
}

// writeFixMemory 原子化写入黑匣子（先写 .tmp 再改名，避免半截文件）
func writeFixMemory(path string, mem FixMemory) error {
	out, err := json.Marshal(mem)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// containsAction 判断病历本是否已精确包含某条工单（按分号逐条比对，避免子串误判）
func containsAction(history, action string) bool {
	for _, a := range strings.Split(history, ";") {
		if strings.TrimSpace(a) == action {
			return true
		}
	}
	return false
}

// mergeFixMemory 合并一轮修复记录：计数+1，动作去重追加（纯函数，便于测试）
func mergeFixMemory(mem FixMemory, action string, now time.Time) FixMemory {
	mem.DiagnoseCount++ // 文件不存在时为 0→1，已存在则累加
	if action != "" && !containsAction(mem.LastAction, action) {
		if mem.LastAction != "" {
			mem.LastAction += ";" + action
		} else {
			mem.LastAction = action
		}
	}
	mem.LastRepairTime = now.Format("2006-01-02 15:04:05")
	return mem
}

// recordFixRound 记录一轮修复：计数+1，动作并入病历本，原子写入目标盘
// （一轮 = 一次诊断，多指令工单只计 1 次，避免计数虚高误触发熔断）
func recordFixRound(action string) {
	if targetDrive == "" || action == "" {
		return
	}
	path := fixMemoryPath()
	mem := FixMemory{}
	if loaded, err := loadFixMemory(path); err == nil {
		mem = loaded
	}
	_ = writeFixMemory(path, mergeFixMemory(mem, action, time.Now()))
}

// rewriteRegForOffline 把备份 .reg 的在线路径改写为离线挂载路径（纯函数，便于测试）
// 备份文件里是 HKEY_LOCAL_MACHINE\SYSTEM\...（在线路径），直接 reg import 只会改到
// PE 自身的注册表；改写为 HKLM\SysTemp\... 后，配合 reg load 才能真正写回目标系统。
func rewriteRegForOffline(content string) string {
	return strings.ReplaceAll(content,
		`HKEY_LOCAL_MACHINE\SYSTEM\`,
		`HKEY_LOCAL_MACHINE\SysTemp\`)
}

// rewriteRegForOfflineSafe 改写并做防御校验：若仍残留在线路径（大小写异常/格式异常）
// 则报错中止 —— 宁可放弃自动回滚，也绝不能把键值写进 PE 自己的注册表。
func rewriteRegForOfflineSafe(content string) (string, error) {
	fixed := rewriteRegForOffline(content)
	if strings.Contains(strings.ToUpper(fixed), `HKEY_LOCAL_MACHINE\SYSTEM\`) {
		return "", fmt.Errorf("备份文件路径改写失败（含未识别的在线路径），已中止回滚以防写错注册表")
	}
	return fixed, nil
}

// RestoreRegistryStart 非对称事务回滚：把备份 .reg 里的启动键值原子化复原到目标盘
func RestoreRegistryStart(driveLetter string, service string) error {
	hive := driveLetter + ":\\Windows\\System32\\config\\SYSTEM"
	backup := driveLetter + ":\\Windows\\" + service + "_Start_backup.reg"

	if _, err := os.Stat(hive); err != nil {
		return fmt.Errorf("SYSTEM 配置单元不存在: %w", err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("备份文件读取失败: %w", err)
	}

	// 关键一步：路径改写 + 防御校验（改写失败则中止，避免写错注册表）
	fixed, err := rewriteRegForOfflineSafe(string(data))
	if err != nil {
		return err
	}

	tmp := filepath.Join(os.TempDir(), "pe_rollback_tmp.reg")
	if err := os.WriteFile(tmp, []byte(fixed), 0644); err != nil {
		return fmt.Errorf("临时回滚文件写入失败: %w", err)
	}
	defer os.Remove(tmp)

	ctxLoad, cancelLoad := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLoad()
	if out, err := exec.CommandContext(ctxLoad, "cmd.exe", "/c", "reg", "load",
		`HKLM\SysTemp`, hive).CombinedOutput(); err != nil {
		return fmt.Errorf("回滚挂载失败: %s, out: %s", err, string(out))
	}
	defer func() {
		ctxUnload, cancelUnload := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnload()
		_ = exec.CommandContext(ctxUnload, "cmd.exe", "/c", "reg", "unload", `HKLM\SysTemp`).Run()
	}()

	ctxImp, cancelImp := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelImp()
	if out, err := exec.CommandContext(ctxImp, "cmd.exe", "/c", "reg", "import",
		tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("回滚导入失败: %s, out: %s", err, string(out))
	}
	return nil
}

// explainBackupError 将备份失败的系统错误转换为中文说明，便于用户理解
func explainBackupError(err error) string {
	if err == nil {
		return "未知错误"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "access is denied"):
		return "权限不足：请确认以管理员身份运行程序，且目标盘允许写入文件"
	case strings.Contains(msg, "no space left on device") || strings.Contains(msg, "not enough space"):
		return "目标盘空间不足：请清理磁盘空间后重试"
	case strings.Contains(msg, "write-protected") || strings.Contains(msg, "write protected"):
		return "磁盘只读：可能是写保护开关或坏道导致，请检查硬盘状态"
	case strings.Contains(msg, "cannot find the path") || strings.Contains(msg, "path not found"):
		return "目标盘符不存在或未正确挂载：请检查 PE 中硬盘的盘符分配"
	case strings.Contains(msg, "i/o error") || strings.Contains(msg, "crc error") || strings.Contains(msg, "input/output") || strings.Contains(msg, "cyclic redundancy"):
		return "硬盘 I/O 错误：可能存在物理坏道，建议先检测硬盘健康状态"
	case strings.Contains(msg, "device is not ready") || strings.Contains(msg, "not ready"):
		return "设备未就绪：请检查硬盘连接线、供电和盘符是否正常"
	case strings.Contains(msg, "permission denied"):
		return "没有写入权限：请检查文件系统权限或改用管理员运行"
	case strings.Contains(msg, "read-only"):
		return "文件系统只读：请检查磁盘是否为只读状态"
	default:
		return "未知错误，请检查磁盘状态、空间和权限后重试"
	}
}

func FixRegistryOffline(driveLetter string, targetService string) error {
	systemHive := driveLetter + ":\\Windows\\System32\\config\\SYSTEM"
	if _, err := os.Stat(systemHive); err != nil {
		return fmt.Errorf("SYSTEM 文件不存在: %w", err)
	}

	ctxMount, cancelMount := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelMount()
	mountCmd := exec.CommandContext(ctxMount, "cmd.exe", "/c", "reg", "load", "HKLM\\SysTemp", systemHive)
	if out, err := mountCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("挂载注册表失败: %s, out: %s", err, string(out))
	}
	defer func() {
		ctxUnload, cancelUnload := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelUnload()
		unloadCmd := exec.CommandContext(ctxUnload, "cmd.exe", "/c", "reg", "unload", "HKLM\\SysTemp")
		if out, err := unloadCmd.CombinedOutput(); err != nil {
			fmt.Printf("警告：卸载注册表失败: %s, out: %s\n", err, string(out))
		}
	}()

	ctxCS, cancelCS := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCS()
	currentCmd := exec.CommandContext(ctxCS, "cmd.exe", "/c", "reg", "query", "HKLM\\SysTemp\\Select", "/v", "Current")
	out, err := currentCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("读取 Select\\Current 失败: %w, out: %s", err, string(out))
	}
	lines := strings.Split(string(out), "\n")
	var currentNum int
	for _, line := range lines {
		if strings.Contains(line, "Current") && strings.Contains(line, "REG_DWORD") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				val := fields[len(fields)-1]
				if strings.HasPrefix(val, "0x") {
					num, _ := strconv.ParseInt(val[2:], 16, 64)
					currentNum = int(num)
				} else {
					num, _ := strconv.Atoi(val)
					currentNum = num
				}
				break
			}
		}
	}
	if currentNum == 0 {
		return fmt.Errorf("无法解析 Current ControlSet 编号")
	}
	controlSet := fmt.Sprintf("ControlSet%03d", currentNum)

	serviceKey := fmt.Sprintf("HKLM\\SysTemp\\%s\\Services\\%s", controlSet, targetService)

	ctxQuery, cancelQuery := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelQuery()
	queryCmd := exec.CommandContext(ctxQuery, "cmd.exe", "/c", "reg", "query", serviceKey, "/v", "Start")
	queryOut, _ := queryCmd.CombinedOutput()
	var currentStart string
	queryLines := strings.Split(string(queryOut), "\n")
	for _, line := range queryLines {
		if strings.Contains(line, "Start") && strings.Contains(line, "REG_DWORD") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				currentStart = fields[len(fields)-1]
				break
			}
		}
	}
	if currentStart != "" {
		fmt.Printf("🔍 服务 %s 当前 Start 值为: %s\n", targetService, currentStart)
	} else {
		fmt.Printf("ℹ️ 服务 %s 的 Start 键不存在或无法读取，将创建新值\n", targetService)
	}

	backupFile := driveLetter + ":\\Windows\\" + targetService + "_Start_backup.reg"
	startVal := strings.TrimPrefix(currentStart, "0x")
	if startVal == "" || startVal == "0" {
		startVal = "00000003"
	} else {
		startVal = fmt.Sprintf("%08s", startVal)
	}
	regContent := fmt.Sprintf(`Windows Registry Editor Version 5.00

[HKEY_LOCAL_MACHINE\SYSTEM\%s\Services\%s]
"Start"=dword:%s
`, controlSet, targetService, startVal)

	if err := os.WriteFile(backupFile, []byte(regContent), 0644); err != nil {
		// 备份失败必须中止，否则修改后无回滚依据
		return fmt.Errorf(
			"备份 Start 值写入失败，已中止修改（未改动任何数据）\n\n"+
				"错误原因: %v\n\n"+
				"中文说明: %s\n\n"+
				"备份文件: %s",
			err, explainBackupError(err), backupFile)
	}
	fmt.Printf("✅ 备份 Start 值成功，备份文件：%s\n", backupFile)

	ctxAdd, cancelAdd := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAdd()
	addCmd := exec.CommandContext(ctxAdd, "cmd.exe", "/c", "reg", "add", serviceKey, "/v", "Start", "/t", "REG_DWORD", "/d", "0", "/f")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("修改注册表服务启动类型失败: %s, out: %s", err, string(out))
	}

	driveLetterOnly := driveLetter[:1]
	msg := fmt.Sprintf(
		"服务 %s 已修改为自动启动。\n\n备份文件已保存至：\n%s\n\n"+
			"注意：该备份文件不能在 PE 中直接双击导入（会修改 PE 自身注册表）。\n"+
			"如需回滚，请在 PE 中执行以下步骤：\n"+
			"1. 打开注册表编辑器（regedit）\n"+
			"2. 选中 HKEY_LOCAL_MACHINE，点击“文件” → “加载配置单元”\n"+
			"3. 选择 %s:\\Windows\\System32\\config\\SYSTEM，加载为 HKLM\\Offline\n"+
			"4. 将 HKLM\\Offline\\%s\\Services\\%s\\Start 修改为备份文件中的原值（%s）\n"+
			"5. 选中 HKLM\\Offline，点击“文件” → “卸载配置单元”",
		targetService, backupFile, driveLetterOnly, controlSet, targetService, currentStart)
	if currentStart == "" {
		msg = fmt.Sprintf(
			"服务 %s 已修改为自动启动。\n\n备份文件已保存至：\n%s\n\n"+
				"注意：该备份文件不能在 PE 中直接双击导入（会修改 PE 自身注册表）。\n"+
				"如需回滚，请在 PE 中执行以下步骤：\n"+
				"1. 打开注册表编辑器（regedit）\n"+
				"2. 选中 HKEY_LOCAL_MACHINE，点击“文件” → “加载配置单元”\n"+
				"3. 选择 %s:\\Windows\\System32\\config\\SYSTEM，加载为 HKLM\\Offline\n"+
				"4. 将 HKLM\\Offline\\%s\\Services\\%s\\Start 修改为备份文件中的原值（备份文件中为 dword:00000003）\n"+
				"5. 选中 HKLM\\Offline，点击“文件” → “卸载配置单元”",
			targetService, backupFile, driveLetterOnly, controlSet, targetService)
	}
	MessageBox("注册表修改成功", msg, 0x40)
	return nil
}

func FixSystemFileOffline(driveLetter string, fileName string, relativePath string) error {
	// ===== 安全校验：目标必须位于 Windows 目录内，禁止路径穿越 =====
	if !validFileName(fileName) {
		return fmt.Errorf("文件名含非法字符: %q\n文件名不允许包含 \\ / : * ? \" < > |", fileName)
	}
	windowsRoot := filepath.Clean(driveLetter + ":\\Windows")
	targetDir := driveLetter + ":\\Windows\\" + filepath.FromSlash(relativePath)
	targetDir = filepath.Clean(targetDir)
	if targetDir != windowsRoot && !strings.HasPrefix(strings.ToLower(targetDir), strings.ToLower(windowsRoot)+"\\") {
		return fmt.Errorf("路径越界，已拒绝执行: %s\n目标必须位于 %s 目录内", targetDir, windowsRoot)
	}
	fullPath := filepath.Join(targetDir, fileName)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	copyFile := func(srcPath string) error {
		if _, err := os.Stat(fullPath); err == nil {
			bakPath := fullPath + ".bak"
			if err := os.Rename(fullPath, bakPath); err != nil {
				return fmt.Errorf("备份原文件失败: %w", err)
			}
		}
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("打开源文件失败: %w", err)
		}
		defer srcFile.Close()
		dstFile, err := os.Create(fullPath)
		if err != nil {
			return fmt.Errorf("创建目标文件失败: %w", err)
		}
		defer dstFile.Close()
		if _, err := io.Copy(dstFile, srcFile); err != nil {
			return fmt.Errorf("复制文件内容失败: %w", err)
		}
		return nil
	}

	var success bool

	winsxsRoot := driveLetter + ":\\Windows\\WinSxS"
	if _, err := os.Stat(winsxsRoot); err == nil {
		var foundPath string
		stopErr := fmt.Errorf("found")
		_ = filepath.Walk(winsxsRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && info.Name() == fileName {
				foundPath = path
				return stopErr
			}
			return nil
		})
		if foundPath != "" {
			if err := copyFile(foundPath); err == nil {
				success = true
			}
		}
	}

	if !success && appConfig.PatchServerURL != "" {
		host := strings.TrimPrefix(appConfig.PatchServerURL, "https://")
		host = strings.TrimPrefix(host, "http://")
		if idx := strings.Index(host, "/"); idx != -1 {
			host = host[:idx]
		}
		ctxResolve, cancelResolve := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancelResolve()
		_, err := net.DefaultResolver.LookupHost(ctxResolve, host)
		if err != nil {
			fmt.Printf("⚠️ 网络预检失败：无法解析域名 %s，跳过云端下载\n", host)
			goto skipCloud
		}

		baseURL := strings.TrimRight(appConfig.PatchServerURL, "/")
		fileURL := baseURL + "/" + fileName
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(fileURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if _, err := os.Stat(fullPath); err == nil {
				bakPath := fullPath + ".bak"
				if err := os.Rename(fullPath, bakPath); err != nil {
					return fmt.Errorf("备份原文件失败: %w", err)
				}
			}
			dstFile, err := os.Create(fullPath)
			if err != nil {
				return fmt.Errorf("创建目标文件失败: %w", err)
			}
			defer dstFile.Close()
			if _, err := io.Copy(dstFile, resp.Body); err != nil {
				return fmt.Errorf("写入文件失败: %w", err)
			}
			success = true
		}
	}
skipCloud:

	if !success {
		errMsg := fmt.Sprintf("本地与云端均无此备件\n\n文件名称：%s\n目标路径：%s\n\n请手动将正确文件放置到上述路径。", fileName, fullPath)
		MessageBox("物理防线击穿提示", errMsg, 0x10)
		return fmt.Errorf("本地 WinSxS 和云端 Gitee 均未找到文件 %s，目标路径：%s", fileName, fullPath)
	}

	msg := "文件修复成功！原文件已备份为 .bak 文件。"
	lowerPath := strings.ToLower(relativePath)
	lowerName := strings.ToLower(fileName)

	if strings.HasSuffix(lowerName, ".inf") {
		msg += "\n\n注意：.inf 文件已复制，但需要手动安装驱动。\n请在 PE 或正常系统中运行以下命令：\npnputil /add-driver " + fullPath + " /install"
	} else if strings.Contains(lowerPath, "drivers") && strings.HasSuffix(lowerName, ".sys") {
		msg += "\n\n如果重启后问题依旧，可能是硬件兼容性问题。\n您可以在 PE 中将 " + fullPath + ".bak 改名为原文件名进行回滚。"
	}
	MessageBox("修复提示", msg, 0x40)
	return nil
}
