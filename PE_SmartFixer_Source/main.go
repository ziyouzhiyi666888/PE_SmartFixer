package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var appConfig Config
var targetDrive string

// ---------- v1.1 长期记忆与熔断状态 ----------
var globalFailedAction string // 历史失败工单（注入负向提示词）
var gFuseAlert bool           // 熔断标记（文字变红）
var gFuseBlocked bool         // 熔断封锁（禁止再次修复）

type Config struct {
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	BaseURL        string `json:"base_url"`
	PatchServerURL string `json:"patch_server_url"`
	AISystemPrompt string `json:"ai_system_prompt"`
}

// ---------- Win32 API ----------
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procRegisterClass    = user32.NewProc("RegisterClassW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procGetWindowText    = user32.NewProc("GetWindowTextW")
	procSetWindowText    = user32.NewProc("SetWindowTextW")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procGetOpenFileName  = comdlg32.NewProc("GetOpenFileNameW")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_BORDER           = 0x00800000
	WS_VSCROLL          = 0x00200000
	ES_MULTILINE        = 0x0004
	ES_AUTOVSCROLL      = 0x0040
	ES_WANTRETURN       = 0x1000
	EM_SETSEL           = 0x00B1
	EM_REPLACESEL       = 0x00C2
	WM_COMMAND          = 0x0111
	WM_DESTROY          = 0x0002
	WM_CTLCOLOREDIT     = 0x0133
	WM_USER             = 0x0400
	WM_AI_DONE          = WM_USER + 1
	COLOR_RED           = 0x000000FF // COLORREF 为 BGR 顺序：红 = 0x000000FF
	IDC_EDIT            = 100
	IDC_BTN_IMG         = 101
	IDC_BTN_START       = 102
)

type OPENFILENAME struct {
	StructSize      uint32
	HwndOwner       uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustomData      uintptr
	Hook            uintptr
	TemplateName    *uint16
}

// ---------- 结果结构体 ----------
type AIDiagnoseResult struct {
	Reply string
	Err   error
}

// ★★★ 全局指针作为"内存避风港"，锁定 goroutine 结果生命周期 ★★★
// 用原子指针消除 goroutine 写入与 UI 线程读取之间的数据竞争
var globalAIResult atomic.Pointer[AIDiagnoseResult]

// ---------- 全局窗口句柄 ----------
var hWndMain, hEdit, hBtnImg, hBtnStart uintptr

// 输入框提示文字（点击获得焦点时消失，失焦且为空时恢复）
var editHintText = "请输入故障现象描述（如蓝屏代码），或点击下方按钮选择截图（可多选）。"

// 编辑框通知码
const (
	EN_SETFOCUS  = 0x0100
	EN_KILLFOCUS = 0x0200
)

// ---------- 窗口过程 ----------
//
//export wndProc
func wndProc(hWnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_CTLCOLOREDIT:
		// 输入框绘制前，父窗口可设置文字颜色（wParam = HDC）
		if gFuseAlert {
			procSetTextColor.Call(wParam, COLOR_RED)
		}
		brush, _, _ := procGetStockObject.Call(0) // WHITE_BRUSH
		return brush

	case WM_COMMAND:
		low := wParam & 0xFFFF
		notifyCode := (wParam >> 16) & 0xFFFF

		// 处理输入框焦点通知：实现"点击消失、失焦恢复"的提示文字
		if low == IDC_EDIT {
			switch notifyCode {
			case EN_SETFOCUS: // 获得焦点：若当前是提示文字则清空
				if gFuseBlocked {
					return 0 // 熔断状态禁止编辑
				}
				buf := make([]uint16, 256)
				n, _, _ := procGetWindowText.Call(hEdit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				if n > 0 {
					txt := syscall.UTF16ToString(buf)
					if txt == editHintText {
						procSetWindowText.Call(hEdit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(""))))
					}
				}
			case EN_KILLFOCUS: // 失去焦点：若为空则恢复提示文字
				if gFuseBlocked {
					return 0
				}
				buf := make([]uint16, 256)
				n, _, _ := procGetWindowText.Call(hEdit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				if n == 0 {
					procSetWindowText.Call(hEdit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(editHintText))))
				}
			}
			return 0
		}

		if low == IDC_BTN_IMG {
			openFileDialog()
			return 0
		}
		if low == IDC_BTN_START {
			// 【硬熔断封锁】已达 3 次连续失败，禁止再次修复，保护数据
			if gFuseBlocked {
				MessageBox("🚨 安全熔断已生效",
					"本机 24 小时内已连续 3 次修复失败，系统已锁定自动化写入。\n\n"+
						"请立即停止反复重启，优先备份重要数据，并呼叫人工排查硬件！", 0x30)
				return 0
			}
			procSendMessage.Call(hBtnStart, 0x0001, 0, 0)
			statusMsg := "\n\n[系统提示] 正在上传截图并请求 AI 专家诊断中，请稍候..."

			// ===== 第一步：先读取用户输入（必须在插入状态消息之前） =====
			buf := make([]uint16, 4096)
			procGetWindowText.Call(hEdit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			fullText := syscall.UTF16ToString(buf)
			// 提示文字不是用户输入，视为空
			if fullText == editHintText {
				fullText = ""
			}
			imagePaths := extractImagePaths(fullText)
			userText := cleanText(fullText)

			// ===== 第二步：再插入状态消息（用 GetWindowTextLengthW 获取正确长度） =====
			textLen, _, _ := user32.NewProc("GetWindowTextLengthW").Call(hEdit)
			procSendMessage.Call(hEdit, EM_SETSEL, textLen, textLen)
			procSendMessage.Call(hEdit, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(statusMsg))))

			if userText == "" && len(imagePaths) == 0 {
				globalAIResult.Store(&AIDiagnoseResult{Reply: "", Err: fmt.Errorf("没有输入任何内容")})
				procPostMessage.Call(hWnd, WM_AI_DONE, 0, 0)
				return 0
			}

			go func() {
				reply, err := CallQwenVLAPI(userText, imagePaths)
				// ★★★ 存入全局变量，生命周期被锁定 ★★★
				globalAIResult.Store(&AIDiagnoseResult{Reply: reply, Err: err})
				// ★★★ 回归 PostMessage，异步投递，绝不阻塞 ★★★
				procPostMessage.Call(hWnd, WM_AI_DONE, 0, 0)
			}()
			return 0
		}
	case WM_AI_DONE:
		// ★★★ UI 线程直接从全局变量读取，不依赖 lParam ★★★
		if res := globalAIResult.Load(); res != nil {
			if res.Err != nil {
				MessageBox("AI 请求失败", fmt.Sprintf("调用 Qwen 出错: %v", res.Err), 0x10)
			} else {
				finishMsg := "\n[诊断完成] 请查看弹窗结果。"
				textLen2, _, _ := procGetWindowText.Call(hEdit, 0, 0)
				procSendMessage.Call(hEdit, EM_SETSEL, uintptr(textLen2), uintptr(textLen2))
				procSendMessage.Call(hEdit, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(finishMsg))))
				RenderWindowForUser(res.Reply)
			}
			// ★★★ 用完立即清空，允许 GC 回收 ★★★
			globalAIResult.Store(nil)
		}
		procSendMessage.Call(hBtnStart, 0x0001, 1, 0)
		return 0
	case WM_DESTROY:
		procDestroyWindow.Call(hWnd)
		postQuitMessage(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hWnd, uintptr(msg), wParam, lParam)
	return ret
}

func postQuitMessage(exitCode int) {
	user32.NewProc("PostQuitMessage").Call(uintptr(exitCode))
}

// ---------- 文件对话框 ----------
// utf16PtrAllowNul 将字符串转为 UTF-16 指针，允许字符串中包含 \x00（NUL）字符。
// syscall.StringToUTF16Ptr 遇到 NUL 会 panic，而 Windows 的 Filter 字符串
// 需要用 \x00 分隔多个过滤项，因此必须使用本函数。
func utf16PtrAllowNul(s string) *uint16 {
	encoded := utf16.Encode([]rune(s))
	buf := make([]uint16, len(encoded)+1) // +1 结尾 NUL
	copy(buf, encoded)
	return &buf[0]
}

func openFileDialog() {
	var file [4096]uint16
	ofn := OPENFILENAME{
		StructSize:   uint32(unsafe.Sizeof(OPENFILENAME{})),
		HwndOwner:    hWndMain,
		Filter:       utf16PtrAllowNul("图片文件 (*.png;*.jpg;*.bmp)\x00*.png;*.jpg;*.bmp\x00所有文件 (*.*)\x00*.*\x00\x00"),
		File:         &file[0],
		MaxFile:      uint32(len(file)),
		FileTitle:    nil,
		MaxFileTitle: 0,
		InitialDir:   nil,
		Title:        syscall.StringToUTF16Ptr("选择故障截图（可多选）"),
		Flags:        0x00000200,
	}
	ret, _, _ := procGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(ofn)

	if ret != 0 {
		var paths []string
		dirLen := 0
		for i := 0; i < len(file); i++ {
			if file[i] == 0 {
				dirLen = i
				break
			}
		}
		dirPath := syscall.UTF16ToString(file[:dirLen])
		if dirPath == "" {
			return
		}
		idx := dirLen + 1
		fileNameStart := idx
		for i := idx; i < len(file); i++ {
			if file[i] == 0 {
				if i == fileNameStart {
					break
				}
				fileName := syscall.UTF16ToString(file[fileNameStart:i])
				if fileName != "" {
					paths = append(paths, dirPath+"\\"+fileName)
				}
				fileNameStart = i + 1
			}
		}
		if len(paths) == 0 && dirPath != "" && strings.Contains(dirPath, ".") {
			paths = append(paths, dirPath)
		}
		if len(paths) > 0 {
			pathList := strings.Join(paths, "\n[截图路径: ")
			pathList = "[截图路径: " + pathList + "]"
			textLen, _, _ := procGetWindowText.Call(hEdit, 0, 0)
			procSendMessage.Call(hEdit, EM_SETSEL, uintptr(textLen), uintptr(textLen))
			procSendMessage.Call(hEdit, EM_REPLACESEL, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("\n"+pathList+"\n"))))
		}
	}
}

func extractImagePaths(text string) []string {
	var paths []string
	startTag := "[截图路径: "
	endTag := "]"
	for {
		start := strings.Index(text, startTag)
		if start == -1 {
			break
		}
		start += len(startTag)
		end := strings.Index(text[start:], endTag)
		if end == -1 {
			break
		}
		path := text[start : start+end]
		if strings.Contains(path, "\n[截图路径: ") {
			for _, p := range strings.Split(path, "\n[截图路径: ") {
				p = strings.TrimSpace(p)
				if p != "" {
					paths = append(paths, p)
				}
			}
		} else {
			paths = append(paths, strings.TrimSpace(path))
		}
		text = text[start+end+1:]
	}
	return paths
}

func cleanText(text string) string {
	for {
		start := strings.Index(text, "[截图路径: ")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "]")
		if end == -1 {
			break
		}
		text = text[:start] + text[start+end+1:]
	}
	for {
		start := strings.Index(text, "[系统提示]")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "\n")
		if end == -1 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+1:]
	}
	for {
		start := strings.Index(text, "[诊断完成]")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "\n")
		if end == -1 {
			text = text[:start]
			break
		}
		text = text[:start] + text[start+end+1:]
	}
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 过滤程序生成的标记行；含"未闭合的截图标记"（缺少 ]）也一并过滤，
		// 避免标记文字被当作故障描述发给 AI
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "[系统提示]") ||
			strings.HasPrefix(trimmed, "[诊断完成]") ||
			strings.HasPrefix(trimmed, "[截图路径: ") {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	return strings.Join(cleaned, "\n")
}

func createMainWindow() uintptr {
	className := syscall.StringToUTF16Ptr("PESmartFixerClass")
	wc := struct {
		Style         uint32
		LpfnWndProc   uintptr
		CbClsExtra    int32
		CbWndExtra    int32
		HInstance     uintptr
		HIcon         uintptr
		HCursor       uintptr
		HbrBackground uintptr
		LpszMenuName  *uint16
		LpszClassName *uint16
	}{
		Style:         0,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		CbClsExtra:    0,
		CbWndExtra:    0,
		HInstance:     0,
		HIcon:         0,
		HCursor:       0,
		HbrBackground: 0x00000005,
		LpszMenuName:  nil,
		LpszClassName: className,
	}
	procRegisterClass.Call(uintptr(unsafe.Pointer(&wc)))

	title := syscall.StringToUTF16Ptr("PE_SmartFixer - AI 智能修复 (Qwen-VL)")
	hWnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		100, 100, 600, 400,
		0, 0, 0, 0,
	)
	return hWnd
}

func createControls(hWnd uintptr) {
	hEdit, _, _ = procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("EDIT"))),
		0,
		WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN,
		10, 10, 560, 250,
		hWnd,
		uintptr(IDC_EDIT),
		0, 0,
	)
	// 设置输入框提示文字（EN_SETFOCUS 时消失，EN_KILLFOCUS 时恢复）
	procSetWindowText.Call(hEdit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(editHintText))))

	hBtnImg, _, _ = procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("📷 选择截图"))),
		WS_CHILD|WS_VISIBLE|WS_BORDER,
		10, 280, 120, 30,
		hWnd,
		uintptr(IDC_BTN_IMG),
		0, 0,
	)

	hBtnStart, _, _ = procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("🚀 开始诊断"))),
		WS_CHILD|WS_VISIBLE|WS_BORDER,
		150, 280, 120, 30,
		hWnd,
		uintptr(IDC_BTN_START),
		0, 0,
	)
}

func runMessageLoop() {
	var msg struct {
		Hwnd    uintptr
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func MessageBox(title, text string, style uintptr) int {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := proc.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), style)
	return int(ret)
}

func messageBox(title, text string, style uintptr) int {
	return MessageBox(title, text, style)
}

// setEditText 改写输入框文字并强制重绘（配合 WM_CTLCOLOREDIT 实现红字）
func setEditText(s string) {
	procSetWindowText.Call(hEdit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(s))))
	procInvalidateRect.Call(hEdit, 0, 1)
}

// ---------- v1.1 前置钩子判定（纯函数，便于测试） ----------

type hookDecision int

const (
	hookClean    hookDecision = iota // 初诊 / 超24小时 → 销毁记忆
	hookFuse                         // 3次连续失败 → 本地硬熔断
	hookRollback                     // 未满3次 → 回滚上次改动 + 唤醒二阶段
)

// decideHook 判定钩子动作
func decideHook(mem FixMemory, now time.Time) hookDecision {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", mem.LastRepairTime, time.Local)
	if err != nil {
		return hookClean // 时间戳损坏 → 视为初诊
	}
	if now.Sub(t) > 24*time.Hour {
		return hookClean // 【24小时主动防御】判定上轮已修复成功
	}
	if mem.DiagnoseCount >= 3 {
		return hookFuse // 【3次连续失败本地硬熔断】
	}
	return hookRollback
}

// startupMemoryHook 前置生命周期钩子：24小时主动防御 + 3次本地硬熔断 + 非对称事务回滚
// 返回 true 表示已触发硬熔断（后续修复权限被锁死）
func startupMemoryHook() bool {
	if targetDrive == "" {
		return false
	}
	path := fixMemoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return false // 无历史记录 → 纯净初诊
	}

	mem, merr := parseFixMemory(data)
	if merr != nil {
		_ = os.Remove(path) // 记录损坏 → 销毁重来
		return false
	}

	switch decideHook(mem, time.Now()) {
	case hookClean:
		_ = os.Remove(path) // 已修复成功或记录过期 → 物理销毁
		return false

	case hookFuse:
		gFuseAlert = true
		gFuseBlocked = true
		setEditText("\n🚨【高危安全熔断提示】🚨\n" +
			"检测到当前这台电脑在 24 小时内已连续执行了 3 次 AI 修复工单，但系统仍无法成功引导。\n" +
			"系统判定：该机的操作底盘大概率已被【物理硬件损坏（如固态硬盘坏道/主控烧毁）】或【高危勒索木马病毒】深度劫持！\n" +
			"为了保护您的物理原始数据不被越修越烂，系统已自动限制自动化盲目写入。\n" +
			"👉 建议：请立即停止反复重启，优先使用本工具备份重要数据，并火速呼叫资深人工网管或去档口排查硬件！")
		return true

	default: // hookRollback
		// 【非对称事务前置回滚】抢在大模型重新号脉前，把上次改错的启动键值复原
		// 回滚失败不弹模态框（避免阻塞启动），改为并入唤醒提示一并展示
		var rollbackNotes []string
		if strings.Contains(mem.LastAction, "REG|") {
			for _, act := range strings.Split(mem.LastAction, ";") {
				act = strings.TrimSpace(act)
				if len(act) < 4 || !strings.EqualFold(act[:4], "REG|") {
					continue
				}
				svc := strings.TrimSpace(act[4:])
				if svc == "" || !validServiceName(svc) {
					continue
				}
				if err := RestoreRegistryStart(targetDrive, svc); err != nil {
					rollbackNotes = append(rollbackNotes, fmt.Sprintf("服务 %s 回滚未完成（%v）", svc, err))
				} else {
					rollbackNotes = append(rollbackNotes, fmt.Sprintf("服务 %s 已自动回滚复原", svc))
				}
			}
		}
		// 【UX 状态唤醒】把失败工单锁进全局变量，并提示用户重新诊断
		globalFailedAction = mem.LastAction
		wake := "\n⚠️【第二阶段备用方案已唤醒】\n" +
			"检测到上一次修复动作未能成功引导系统（历史失败工单：" + mem.LastAction + "）。\n"
		if len(rollbackNotes) > 0 {
			wake += "回滚情况：" + strings.Join(rollbackNotes, "；") + "。\n"
		}
		wake += "系统已把历史失败路径列为本次诊断的【禁区】。\n" +
			"👉 请重新描述故障现象（或选择截图），然后点击“开始诊断”，AI 将彻底更换思路给出全新方案。"
		setEditText(wake)
		return false
	}
}

// disableWow64Redirection 关闭 WOW64 文件系统重定向。
// 32 位程序在 64 位 Windows 上访问 C:\Windows\System32 会被透明重定向到
// C:\Windows\SysWOW64，导致：① 找不到系统盘（SysWOW64 下没有 config\SYSTEM）
// ② 修复 System32 下的文件会写错到 SysWOW64。故必须在扫描系统盘之前关闭。
// 在 32 位系统或 64 位进程中该调用返回 FALSE，无副作用。
func disableWow64Redirection() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("Wow64DisableWow64FsRedirection")
	var oldValue uintptr
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(&oldValue)))
}

func main() {
	runtime.LockOSThread()
	disableWow64Redirection() // 必须在 ScanSystemDrive 之前
	_ = exec.Command("cmd.exe", "/c", "reg", "unload", "HKLM\\SysTemp").Run()

	data, err := os.ReadFile("config.json")
	if err != nil {
		MessageBox("配置错误", "无法读取 config.json，请检查文件是否存在", 0x10)
		return
	}
	if err := json.Unmarshal(data, &appConfig); err != nil {
		MessageBox("配置错误", fmt.Sprintf("解析 config.json 失败: %v", err), 0x10)
		return
	}
	if appConfig.APIKey == "" {
		MessageBox("配置错误", "config.json 中缺少 api_key", 0x10)
		return
	}
	if appConfig.BaseURL == "" {
		MessageBox("配置错误", "config.json 中缺少 base_url", 0x10)
		return
	}
	if appConfig.Model == "" {
		appConfig.Model = "qwen-vl-plus"
	}

	targetDrive = ScanSystemDrive()
	if targetDrive == "" {
		MessageBox("提示", "未自动识别到系统盘，请确保 PE 正确加载硬盘。诊断时仍可继续。", 0x40)
	}

	hWndMain = createMainWindow()
	if hWndMain == 0 {
		MessageBox("错误", "创建窗口失败", 0x10)
		return
	}
	createControls(hWndMain)
	startupMemoryHook() // 前置生命周期钩子（需 hEdit 已创建）
	runMessageLoop()
}
