package main

import "testing"

func TestValidServiceName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"正常服务名", "iaStorV", true},
		{"带下划线", "my_service", true},
		{"带点和连字符", "svc.drv-x", true},
		{"空字符串", "", false},
		{"含&注入", "svc&calc", false},
		{"含竖线", "svc|calc", false},
		{"含空格", "my svc", false},
		{"含分号", "svc;rmdir", false},
	}
	for _, c := range cases {
		if got := validServiceName(c.in); got != c.want {
			t.Errorf("%s: validServiceName(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestValidFileName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"正常文件名", "netkvm.inf", true},
		{"正常sys", "driver.sys", true},
		{"空字符串", "", false},
		{"含反斜杠", "a\\b.exe", false},
		{"含斜杠", "a/b.exe", false},
		{"含冒号", "a:b.exe", false},
		{"含星号", "*.exe", false},
		{"含问号", "a?.exe", false},
		{"含双引号", `a"b.exe`, false},
		{"含尖括号", "a<b>.exe", false},
		{"含竖线", "a|b.exe", false},
	}
	for _, c := range cases {
		if got := validFileName(c.in); got != c.want {
			t.Errorf("%s: validFileName(%q) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestParseDocActionSecurity(t *testing.T) {
	cases := []struct {
		name      string
		docAction string
		wantErr   string
	}{
		{"REG正常", "REG|iaStorV", ""},
		{"REG注入&", "REG|svc&calc", "服务名含非法字符"},
		{"REG注入|", "REG|svc|evil", "REG 指令格式错误"},
		{"REG空服务名", "REG|", "服务名含非法字符"},
		{"FILE正常", "FILE|netkvm.inf|INF", ""},
		{"FILE路径穿越", "FILE|x.sys|..\\..\\evil", ""}, // 格式合法，路径校验在 FixSystemFileOffline
		{"FILE文件名带斜杠", "FILE|a/b.sys|drivers", "文件名含非法字符"},
		{"FILE文件名穿越", "FILE|..\\evil.sys|drivers", "文件名含非法字符"},
	}
	for _, c := range cases {
		_, err := parseDocAction(c.docAction)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: parseDocAction(%q) unexpected error: %v", c.name, c.docAction, err)
			}
		} else {
			if err == nil {
				t.Errorf("%s: parseDocAction(%q) expected error containing %q, got nil", c.name, c.docAction, c.wantErr)
			} else if !containsStr(err.Error(), c.wantErr) {
				t.Errorf("%s: parseDocAction(%q) error = %q, want contains %q", c.name, c.docAction, err.Error(), c.wantErr)
			}
		}
	}
}

func TestFixSystemFileOfflinePathGuard(t *testing.T) {
	// 路径穿越必须被拒绝（在创建目录/写文件之前）
	err := FixSystemFileOffline("C", "evil.exe", "..\\..\\..\\Users\\Public")
	if err == nil {
		t.Fatal("路径穿越未被拒绝，存在安全漏洞!")
	}
	if !containsStr(err.Error(), "路径越界") {
		t.Errorf("错误信息应提示路径越界，实际: %v", err)
	}
	// 正常路径不应被误杀（只校验，不真正写文件——用不存在的盘符避免副作用）
	err2 := FixSystemFileOffline("Z", "netkvm.inf", "INF")
	if err2 == nil {
		t.Log("Z盘路径校验通过（后续会因盘符不存在而失败，属于正常）")
	} else if containsStr(err2.Error(), "路径越界") {
		t.Errorf("正常路径被误判越界: %v", err2)
	}
}
