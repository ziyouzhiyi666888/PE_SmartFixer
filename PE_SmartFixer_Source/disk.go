package main

import "os"

// ScanSystemDrive 遍历 A-Z 盘符（跳过 X），查找包含 \Windows\System32\config\SYSTEM 的系统盘
func ScanSystemDrive() string {
	for i := 'A'; i <= 'Z'; i++ {
		d := string(i)
		if d == "X" {
			continue
		}
		path := d + ":\\Windows\\System32\\config\\SYSTEM"
		if _, err := os.Stat(path); err == nil {
			return d
		}
	}
	return ""
}