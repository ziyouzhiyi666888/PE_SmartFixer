package main

import (
	"errors"
	"testing"
)

func TestExplainBackupError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"权限不足", errors.New("open C:\\Windows\\x.reg: Access is denied."), "权限不足"},
		{"空间不足", errors.New("write C:\\Windows\\x.reg: There is not enough space on the disk."), "空间不足"},
		{"磁盘只读", errors.New("The media is write protected."), "磁盘只读"},
		{"路径不存在", errors.New("The system cannot find the path specified."), "目标盘符不存在"},
		{"I/O错误", errors.New("An I/O error occurred while reading from or writing to a device."), "硬盘 I/O 错误"},
		{"CRC错误", errors.New("Data error (cyclic redundancy check)."), "硬盘 I/O 错误"},
		{"设备未就绪", errors.New("The device is not ready."), "设备未就绪"},
		{"未知错误", errors.New("Something weird happened."), "未知错误"},
		{"nil错误", nil, "未知错误"},
	}
	for _, c := range cases {
		got := explainBackupError(c.err)
		if !containsStr(got, c.want) {
			t.Errorf("%s: explainBackupError(%v) = %q, want contains %q", c.name, c.err, got, c.want)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
