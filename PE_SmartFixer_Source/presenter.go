package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type PresenterResponse struct {
	RiskLevel string `json:"risk_level"`
	UserDesc  string `json:"user_desc"`
	DocAction string `json:"doc_action"`
}

// fixJSONEscapes 修复 AI 返回 JSON 中的非法反斜杠转义。
// 例如 "SYSTEM32\DRIVERS" 中的 \D 不是合法 JSON 转义，会导致 json.Unmarshal 失败，
// 需将非法 \ 转义为字面量 \\。
func fixJSONEscapes(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			sb.WriteByte(c)
			continue
		}
		if i+1 < len(s) {
			n := s[i+1]
			if strings.ContainsRune("\"\\/bfnrtu", rune(n)) {
				// 合法转义序列，原样保留
				sb.WriteByte(c)
				sb.WriteByte(n)
				i++
			} else {
				// 非法转义：\ 视为字面量反斜杠
				sb.WriteByte('\\')
				sb.WriteByte('\\')
				sb.WriteByte(n)
				i++
			}
		} else {
			// \ 在末尾，转义为字面量
			sb.WriteByte('\\')
			sb.WriteByte('\\')
		}
	}
	return sb.String()
}

func RenderWindowForUser(jsonFromAI string) {
	// 先修复 AI 返回 JSON 中的非法转义（如 SYSTEM32\DRIVERS）
	jsonFromAI = fixJSONEscapes(jsonFromAI)

	var pr PresenterResponse
	if err := json.Unmarshal([]byte(jsonFromAI), &pr); err != nil {
		msg := fmt.Sprintf("解析 AI 药方失败: %v\n原始内容: %s", err, jsonFromAI)
		messageBox("解析错误", msg, 0x10)
		return
	}
	switch pr.RiskLevel {
	case "SAFE":
		title := "🔧 安全修复建议 (SAFE)"
		msg := "故障原因: " + pr.UserDesc + "\n\n是否执行修复操作？"
		ret := messageBox(title, msg, 0x24) // MB_YESNO|MB_ICONQUESTION
		if ret == 6 {                      // IDYES
			ExecuteSurgery(pr.DocAction)
		} else {
			messageBox("操作取消", "已取消修复", 0x40)
		}
	case "WARNING":
		title := "⚠️ 警告修复建议 (WARNING)"
		msg := "故障原因: " + pr.UserDesc + "\n\n是否执行修复操作？"
		ret := messageBox(title, msg, 0x34) // MB_YESNO|MB_ICONWARNING
		if ret == 6 {                      // IDYES
			ExecuteSurgery(pr.DocAction)
		} else {
			messageBox("操作取消", "已取消修复", 0x40)
		}
	case "CRITICAL":
		// 高危：显示诊断结果 + AI 建议，人工确认后决定是否修复
		title := "🚨 物理熔断触发 - 高危警告"
		msg := "硬盘可能存在物理损坏或严重病毒！\n\n故障原因: " + pr.UserDesc
		if pr.DocAction != "" {
			msg += "\n\nAI 建议修复动作: " + pr.DocAction
		}
		msg += "\n\n⚠️ 高危问题可能涉及硬件损坏，强烈建议先备份数据！\n是否仍然执行修复？"
		ret := messageBox(title, msg, 0x34) // MB_YESNO|MB_ICONWARNING
		if ret == 6 {                      // IDYES
			ExecuteSurgery(pr.DocAction)
		} else {
			messageBox("操作取消", "已取消修复，请尽快备份数据！", 0x40)
		}
	default:
		messageBox("未知风险等级", "AI 返回了无法识别的风险等级: "+pr.RiskLevel, 0x10)
	}
}

// ExecuteSurgery 支持多指令（; 分隔）
func ExecuteSurgery(docAction string) {
	if docAction == "" {
		messageBox("执行错误", "工单动作为空，AI 未返回修复动作", 0x10)
		return
	}

	// 如果包含分号，拆分为多个指令
	if strings.Contains(docAction, ";") {
		actions := strings.Split(docAction, ";")
		var failed []string
		successCount := 0
		for _, action := range actions {
			action = strings.TrimSpace(action)
			if action == "" {
				continue
			}
			if err := executeSingleSurgery(action); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", action, err))
			} else {
				successCount++
			}
		}
		if len(failed) == 0 {
			messageBox("全部操作成功", fmt.Sprintf("共 %d 个操作全部完成", successCount), 0x40)
		} else {
			msg := fmt.Sprintf("成功 %d 个，失败 %d 个:\n%s", successCount, len(failed), strings.Join(failed, "\n"))
			messageBox("部分操作失败", msg, 0x10)
		}
		return
	}

	// 单个指令走原逻辑
	if err := executeSingleSurgery(docAction); err != nil {
		messageBox("操作失败", err.Error(), 0x10)
	} else {
		messageBox("操作成功", "修复完成", 0x40)
	}
}

// parseDocAction 解析工单指令，返回前缀及校验错误（不执行实际手术）
func parseDocAction(docAction string) (string, error) {
	parts := strings.SplitN(docAction, "|", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("格式错误，缺少 '|' 分隔符: %s", docAction)
	}
	prefix := strings.ToUpper(strings.TrimSpace(parts[0]))
	switch prefix {
	case "REG":
		if len(parts) > 2 {
			return prefix, fmt.Errorf("REG 指令格式错误: %s\n正确格式: REG|服务名\n如需修复文件请用: FILE|文件名|相对路径", docAction)
		}
		return prefix, nil
	case "FILE":
		if len(parts) < 3 {
			return prefix, fmt.Errorf("FILE 工单缺少相对路径参数: %s\n正确格式: FILE|文件名|相对路径", docAction)
		}
		return prefix, nil
	default:
		return prefix, fmt.Errorf("未知工单前缀: %s\n支持的前缀: REG (注册表服务) 或 FILE (系统文件)", prefix)
	}
}

// executeSingleSurgery 执行单个指令，增加参数校验，拒绝 REG 的多键值指令
func executeSingleSurgery(docAction string) error {
	prefix, err := parseDocAction(docAction)
	if err != nil {
		return err
	}
	parts := strings.SplitN(docAction, "|", 3)
	switch prefix {
	case "REG":
		service := strings.TrimSpace(parts[1])
		return FixRegistryOffline(targetDrive, service)
	case "FILE":
		fileName := strings.TrimSpace(parts[1])
		relPath := strings.TrimSpace(parts[2])
		return FixSystemFileOffline(targetDrive, fileName, relPath)
	default:
		return fmt.Errorf("未知工单前缀: %s", prefix)
	}
}