# Windows 维护协议 (WMP) 技术规格书
**版本:** 0.1.0-draft (草案)  
**作者:** ziyouzhiyi666888  
**状态:** 开放讨论与生态接入实施中  

---

## 1. 概述 (Abstract)
Windows 维护协议 (Windows Maintenance Protocol, 简称 WMP) 确立了一种在 Windows 预安装环境 (WinPE) 恶劣离线场景下，AI 驱动的智能诊断核心（简称“大脑”，如 AI 视觉 Agent）与底层系统修复工具（简称“手术刀”）之间的标准化、零网络、轻量化通信规范。

WMP 核心通过将“前沿的多模态 AI 视觉分析”与“硬编码的物理系统执行”进行完美解耦，旨在构建一个全自动、零配置的操作系统自愈生态系统（OS Self-Healing Ecosystem）。

---

## 2. 通信层设计 (Zero-Footprint Design / 零内存残留设计)
为了确保在无网卡驱动、磁盘损坏等极度恶劣的 WinPE 环境下达成 100% 的高可用性，WMP 严格禁止使用任何笨重的网络套接字（Network Sockets）。所有数据交换必须且仅能通过以下两种本地管道之一流转：
- **标准输入/输出流 (Stdin / Stdout Pipes)**  
  接入工具必须能够通过 Stdin（标准输入）接收标准的 JSON 指令负载，并通过 Stdout（标准输出）实时返回标准的 JSON 执行结果。
- **本地回环 JSON-RPC (`127.0.0.1`)**  
  对于需要常驻后台服务模式的工具，允许且仅允许在本地回环地址 `127.0.0.1` 上，通过基于 HTTP 的 JSON-RPC 2.0 协议进行原子化异步通信。

---

## 3. 协议数据单元 (JSON-RPC 2.0 标准格式)
所有 WMP 交互负载必须严格遵守 JSON-RPC 2.0 规范，其核心修机指令流必须封装在 `params`（参数）字段中。

### 3.1 AI 诊断手术指令帧 (AI 大脑 → 物理维护工具)
当 AI 大脑通过视觉大模型识别出蓝屏（BSOD）图片、死机报错屏幕的故障模式后，向底层指定的执行工具下发统一结构化的物理手术指令包。

#### 指令格式示例 (Payload Schema)
```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0001",
  "action_type": "REGISTRY_SURGERY | FILE_SURGERY | EXEC_COMMAND",
  "target_scope": {
    "target_os_drive": "D:",
    "target_hive": "SYSTEM",
    "control_set": "DYNAMIC"
  },
  "operations": [
    {
      "op": "REPLACE | DELETE | ADD",
      "key": "Start",
      "value_type": "REG_DWORD",
      "value_data": "0x00000000",
      "file_source": "",
      "file_dest": ""
    }
  ],
  "safety_policy": {
    "risk_level": "SAFE | WARNING | CRITICAL",
    "require_backup": true,
    "skip_if_exists": false
  },
  "fallback": {
    "description": "人工或自动回滚指导说明",
    "fallback_script": "reg unload HKLM\\SysTemp"
  }
}
```

#### 核心字段定义 (Field Definitions)
*   **`wmp_version`**: 协议版本号。当前阶段必须固定为 `0.1.0-draft`。
*   **`session_id`**: 用于跟踪的唯一会话标识符。
*   **`action_type`**: 手术动作分类。必须为 `REGISTRY_SURGERY`（离线注册表手术）、`FILE_SURGERY`（系统驱动文件修补手术）、`EXEC_COMMAND`（执行系统急救命令）之一。
*   **`target_scope.target_os_drive`**: 离线操作系统的盘符（例如 D:）。
*   **`target_scope.target_hive`**: 注册表配置单元名称（例如 SYSTEM、SOFTWARE）。
*   **`target_scope.control_set`**: 注册表控制组定位。填入 **`DYNAMIC`** 代表开启**动态检测机制**，通知执行工具自动去瘫痪系统中排查真正生效的控制组编号（如 `ControlSet001`），严禁盲猜。
*   **`operations[].op`**: 具体的物理修改动作。必须为 `REPLACE`（替换修改）、`DELETE`（无情删除）、`ADD`（新增项）之一。
*   **`operations[].key`**: 注册表项路径或文件名称（例如：`Services\\iaStorAVC\\Start`）。
*   **`operations[].value_type`**: 针对注册表操作：`REG_DWORD`, `REG_SZ`, `REG_EXPAND_SZ` 等。针对文件操作则为 `N/A`。
*   **`operations[].value_data`**: 新的键值数据。
*   **`file_source`**: 专用于文件补全手术（`FILE_SURGERY`），代表源文件路径（如 U 盘备份驱动）。
*   **`file_dest`**: 专用于文件补全手术（`FILE_SURGERY`），代表目标文件路径（如瘫痪系统的 `System32` 关键路径）。
*   **`safety_policy.risk_level`**: 风险等级，必须是 `SAFE`, `WARNING`, `CRITICAL` 之一。
*   **`safety_policy.require_backup`**: 如果为 true，工具在执行写入前必须创建对应的物理备份。
*   **`safety_policy.skip_if_exists`**: 如果为 true，若目标已存在则直接跳过该操作。
*   **`fallback.description`**: 便于人类阅读的故障回滚操作指南。
*   **`fallback.fallback_script`**: 发生故障时用于执行自动化回滚的底层原生命令。

---

### 3.2 工具执行反馈帧 (物理维护工具 → AI 大脑)
第三方工具在接收并完成 WMP 手术指令后，必须立刻向 AI 大脑投递一个原子化的状态回报包，以完成整个系统智能化自愈的闭环验证。

#### 反馈格式示例 (Payload Schema)
```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0001",
  "status": "SUCCESS | PARTIAL_SUCCESS | FAILED",
  "execution_log": {
    "backup_created": "D:\\Windows\\System32\\config\\SYSTEM.bak",
    "affected_rows": 1,
    "error_code": 0,
    "error_message": ""
  },
  "system_state": {
    "registry_flushed": true,
    "requires_reboot": true
  }
}
```

#### 核心字段定义 (Field Definitions)
*   **`status`**: 执行状态。必须为 `SUCCESS`（完全成功）、`PARTIAL_SUCCESS`（部分成功）、`FAILED`（彻底失败）之一。
*   **`execution_log.backup_created`**: 已创建的物理备份文件的绝对路径（若无备份则为空）。
*   **`execution_log.affected_rows`**: 本次手术修改或覆盖的注册表键值/文件总数量。
*   **`execution_log.error_code`**: 系统层面的错误代码（为 0 代表无错误完美通过）。
*   **`execution_log.error_message`**: 便于人类阅读的详细错误原因描述。
*   **`system_state.registry_flushed`**: 如果为 true，代表修改已物理持久化写入磁盘，未留存在临时缓存中。
*   **`system_state.requires_reboot`**: 如果为 true，代表更改需要用户重启电脑才能正式生效。

---

## 4. 安全防护与灾备回滚架构 (Safety & Rollback Architecture)
任何宣称兼容或接入 WMP v0.1.0-draft 协议标准的物理维护工具，在对瘫痪的目标操作系统进行任何易失性（破坏性）物理写入前，**必须且强制执行“术前快照（Pre-Surgery Snapshot）”防御逻辑**：
1. **注册表手术防线**：任何注册表键值的修改，底层必须先导出并生成对应的 `.reg` 备份文件。
2. **驱动文件手术防线**：任何核心驱动、系统文件的替换或覆盖，必须先将原有损坏文件重命名改写为 `.bak` 原生备份。
3. **自动熔断回滚机制**：如果执行过程中发生意外导致 `status == FAILED`，工具必须立刻自动调用指令中下发的 `fallback.fallback_script` 脚本（如强行卸载离线挂载的 SYSTEM 单元），**实施物理级熔断与安全倒回，确保用户磁盘数据资产的绝对零污染**。

---

## 5. 错误处理规范与通用错误码
如果底层工具在 WinPE 环境下由于环境恶劣（如权限不足或文件被死锁）无法处理 AI 大脑的药方，必须优雅降级并返回标准错误响应包。

### 5.1 标准错误包裹示例
```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0001",
  "status": "FAILED",
  "execution_log": {
    "error_code": 4,
    "error_message": "Access denied: insufficient privileges in WinPE environment"
  }
}
```

### 5.2 官方推荐错误代码表
*   **`0`**: 完美执行成功 (Success)
*   **`1`**: 无法识别的 `action_type` 指令动作 (Unknown action_type)
*   **`2`**: 格式错误的 JSON 协议数据包 (Malformed JSON payload)
*   **`3`**: 目标物理文件或离线注册表路径未找到 (Target not found)
*   **`4`**: 拒绝访问/WinPE 临时环境提权失败 (Permission denied)
*   **`5`**: 术前安全备份创建失败 (Backup creation failed)
*   **`6`**: 熔断自动回滚操作失败 (Rollback failed)
*   **`99`**: 未知的不可抗力系统级错误 (Unknown error)

---

## 6. 示例工作流 (Example Workflow)

### 6.1 AI 发送诊断帧示例
```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0007",
  "action_type": "REGISTRY_SURGERY",
  "target_scope": {
    "target_os_drive": "D:",
    "target_hive": "SYSTEM",
    "control_set": "DYNAMIC"
  },
  "operations": [
    {
      "op": "REPLACE",
      "key": "Services\\iaStorAVC\\Start",
      "value_type": "REG_DWORD",
      "value_data": "0x00000000"
    }
  ],
  "safety_policy": {
    "risk_level": "WARNING",
    "require_backup": true,
    "skip_if_exists": false
  },
  "fallback": {
    "description": "从导出的备份中还原原始的 Start 键值",
    "fallback_script": "reg restore HKLM\\SysTemp D:\\backup.reg"
  }
}
```

### 6.2 工具返回执行成功帧示例
```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0007",
  "status": "SUCCESS",
  "execution_log": {
    "backup_created": "D:\\Windows\\System32\\config\\SYSTEM.bak",
    "affected_rows": 1,
    "error_code": 0,
    "error_message": ""
  },
  "system_state": {
    "registry_flushed": true,
    "requires_reboot": true
  }
}
```

---

## 7. 生态接入规范声明 (Implementer Notes)

### 7.1 工具接入技术约束
*   **自描述清单文件 (`wmp_manifest.json`)**：任何第三方独立维护工具（如 DiskGenius、傲梅、或国外的 PE Tools）想要完美接入本 AI 智能自愈生态，只需在其程序根目录下放置一个标准的自描述声明。AI 修复工具主程序在扫描到该文件后，会**直接将其作为万能解耦插件接管**并进行联动。
```json
{
  "name": "PE-SmartFixer",
  "wmp_version": "0.1.0-draft",
  "capabilities": ["REGISTRY_SURGERY", "FILE_SURGERY"],
  "input": "stdin",
  "output": "stdout"
}
```
*   **优雅容错 (Graceful Failure)**：所有接入工具在面对格式错误、损坏的 JSON 协议数据时必须具备优雅降级能力，必须通过标准通道向主程序返回错误反馈帧，严禁发生主进程闪退或崩溃。
*   **死锁超时控制 (Timeout Handling)**：在恶劣的 WinPE 环境下，工具必须实现严格的 **60秒操作超时控制**。除非指令流中有特殊声明，否则一旦超出 60 秒未响应，主程序将自动判定为 `FAILED` 并强行切断操作，防止整个 PE 界面因为某个第三方工具异常而彻底陷入系统级死锁。

---
**Windows 维护协议 (WMP) 规格书草案终。**
