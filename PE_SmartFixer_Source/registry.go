package main

import (
	"context"
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
	targetDir := driveLetter + ":\\Windows\\" + filepath.FromSlash(relativePath)
	targetDir = filepath.Clean(targetDir)
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