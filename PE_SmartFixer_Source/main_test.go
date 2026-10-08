package main

import (
	"strings"
	"testing"
)

func TestExtractImagePaths(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "单个截图路径",
			text: "蓝屏代码 0x0000007B\n[截图路径: C:\\Users\\test\\Desktop\\screen1.png]\n请帮我分析",
			want: []string{"C:\\Users\\test\\Desktop\\screen1.png"},
		},
		{
			name: "多个截图路径",
			text: "[截图路径: C:\\a.png]\n[截图路径: D:\\b.jpg]",
			want: []string{"C:\\a.png", "D:\\b.jpg"},
		},
		{
			name: "无截图路径",
			text: "只有文字描述，没有截图",
			want: nil,
		},
		{
			name: "空文本",
			text: "",
			want: nil,
		},
		{
			name: "截图路径在开头",
			text: "[截图路径: E:\\img1.bmp]\n故障描述",
			want: []string{"E:\\img1.bmp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractImagePaths(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("extractImagePaths() len = %d, want %d; got = %v", len(got), len(tt.want), got)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractImagePaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "清除截图路径标记",
			text: "蓝屏\n[截图路径: C:\\a.png]\n重启失败",
			want: "蓝屏\n重启失败",
		},
		{
			name: "清除系统提示",
			text: "故障描述\n[系统提示] 正在上传截图并请求 AI 专家诊断中，请稍候...\n更多描述",
			want: "故障描述\n更多描述",
		},
		{
			name: "清除诊断完成标记",
			text: "分析中\n[诊断完成] 请查看弹窗结果。\n结束",
			want: "分析中\n结束",
		},
		{
			name: "混合标记清除",
			text: "原始描述\n[截图路径: C:\\a.png]\n[系统提示] 正在请求AI...\n[诊断完成] 完成。",
			want: "原始描述",
		},
		{
			name: "纯文本无标记",
			text: "蓝屏代码 0x0000007B\nINACCESSIBLE_BOOT_DEVICE",
			want: "蓝屏代码 0x0000007B\nINACCESSIBLE_BOOT_DEVICE",
		},
		{
			name: "空文本",
			text: "",
			want: "",
		},
		{
			name: "只有标记没有其他内容",
			text: "[截图路径: X:\\x.png]\n[系统提示] 提示",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanText(tt.text)
			if got != tt.want {
				t.Errorf("cleanText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDocAction(t *testing.T) {
	tests := []struct {
		name       string
		docAction  string
		wantPrefix string
		wantErr    string
	}{
		{
			name:       "REG 指令",
			docAction:  "REG|iaStorV",
			wantPrefix: "REG",
			wantErr:    "",
		},
		{
			name:       "FILE 指令",
			docAction:  "FILE|netkvm.inf|INF",
			wantPrefix: "FILE",
			wantErr:    "",
		},
		{
			name:      "缺少分隔符",
			docAction: "REGiaStorV",
			wantErr:   "缺少 '|' 分隔符",
		},
		{
			name:      "FILE 缺少路径参数",
			docAction: "FILE|netkvm.inf",
			wantErr:   "缺少相对路径参数",
		},
		{
			name:      "未知前缀",
			docAction: "UNKNOWN|test",
			wantErr:   "未知工单前缀",
		},
		{
			name:      "REG 多段拒绝",
			docAction: "REG|srv|key",
			wantErr:   "REG 指令格式错误",
		},
		{
			name:       "小写 reg",
			docAction:  "reg|ServiceName",
			wantPrefix: "REG",
			wantErr:    "",
		},
		{
			name:       "小写 file",
			docAction:  "file|driver.sys|drivers",
			wantPrefix: "FILE",
			wantErr:    "",
		},
		{
			name:      "空指令",
			docAction: "",
			wantErr:   "缺少 '|' 分隔符",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, err := parseDocAction(tt.docAction)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("parseDocAction() expected error containing %q, got nil", tt.wantErr)
					return
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("parseDocAction() error = %q, want contain %q", err.Error(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("parseDocAction() unexpected error = %v", err)
					return
				}
				if prefix != tt.wantPrefix {
					t.Errorf("parseDocAction() prefix = %q, want %q", prefix, tt.wantPrefix)
				}
			}
		})
	}
}
