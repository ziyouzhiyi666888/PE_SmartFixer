package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ---------- OpenAI 标准格式结构体 ----------

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []ContentPart
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL string `json:"url"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func CallQwenVLAPI(userText string, imagePaths []string) (string, error) {
	if appConfig.APIKey == "" {
		return "", fmt.Errorf("配置不完整：缺少 api_key")
	}
	if appConfig.BaseURL == "" {
		return "", fmt.Errorf("配置不完整：缺少 base_url")
	}
	model := appConfig.Model
	if model == "" {
		model = "qwen-vl-plus"
	}

	systemPrompt := appConfig.AISystemPrompt
	if systemPrompt == "" {
		systemPrompt = "你是一名Windows系统维修专家。根据故障描述和截图返回严格JSON，格式：{\"risk_level\":\"SAFE|WARNING|CRITICAL\",\"user_desc\":\"大白话原因\",\"doc_action\":\"具体动作\"}，doc_action格式：REG|服务名 或 FILE|文件名|相对路径，多指令用;分隔。只返回JSON。"
	}

	var content []ContentPart
	if userText != "" {
		content = append(content, ContentPart{Type: "text", Text: userText})
	}
	for _, path := range imagePaths {
		if path == "" {
			continue
		}
		imgData, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("读取图片失败 %s: %w", path, err)
		}
		base64Str := base64.StdEncoding.EncodeToString(imgData)
		mimeType := "image/jpeg"
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".png") {
			mimeType = "image/png"
		} else if strings.HasSuffix(lower, ".bmp") {
			mimeType = "image/bmp"
		}
		content = append(content, ContentPart{
			Type: "image_url",
			ImageURL: &ImageURL{
				URL: fmt.Sprintf("data:%s;base64,%s", mimeType, base64Str),
			},
		})
	}
	if len(content) == 0 {
		return "", fmt.Errorf("没有提供任何文本或图片")
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: content},
	}

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	url := appConfig.BaseURL
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+appConfig.APIKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 返回非 200: %d\nbody: %s", resp.StatusCode, string(body))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("解析响应 JSON 失败: %w\nbody: %s", err, string(body))
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("API 错误: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("API 响应中没有 choices")
	}

	reply := chatResp.Choices[0].Message.Content
	reply = strings.TrimSpace(reply)
	if strings.HasPrefix(reply, "```json") {
		reply = strings.TrimPrefix(reply, "```json")
		reply = strings.TrimSuffix(reply, "```")
		reply = strings.TrimSpace(reply)
	}
	if strings.HasPrefix(reply, "```") {
		reply = strings.TrimPrefix(reply, "```")
		reply = strings.TrimSuffix(reply, "```")
		reply = strings.TrimSpace(reply)
	}
	return reply, nil
}