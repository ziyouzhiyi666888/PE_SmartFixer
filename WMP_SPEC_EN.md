# Windows Maintenance Protocol (WMP) Specification
**Version:** 0.1.0-draft  
**Author:** ziyouzhiyi666888  
**Status:** Open for Discussion & Implementation  

---

## 1. Abstract

The Windows Maintenance Protocol (WMP) defines a standardized, zero-network, ultra-lightweight communication specification between AI-driven Diagnostic Agents (the "Brain") and lower-level OS recovery tools (the "Scalpel") within the Windows Preinstallation Environment (WinPE).

WMP decouples multimodal visual analysis from hardcoded system execution, enabling an automated OS self-healing ecosystem.

---

## 2. Communication Layer (Zero-Footprint Design)

To guarantee 100% reliability in drive-critical and driver-less WinPE environments, WMP explicitly bans heavy network sockets. All data exchange MUST flow through either:

- **Standard I/O Streams (Stdin / Stdout Pipes)**  
  Tools MUST be able to receive JSON payloads via Stdin and return JSON results via Stdout.
- **Local Loopback JSON-RPC (`127.0.0.1`)**  
  For tools that require persistent service mode, JSON-RPC 2.0 over HTTP is permitted exclusively on `127.0.0.1`.

---

## 3. Protocol Data Units (JSON-RPC 2.0 Standard)

All WMP payloads MUST follow the JSON-RPC 2.0 specification. The `params` field MUST contain the following structure.

### 3.1 AI Diagnostic Frame (AI → Maintenance Tool)

When the AI Brain detects a system crash pattern (e.g., BSOD image analysis), it issues a standardized structured surgery command packet to the target execution tool.

#### Payload Schema

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
    "description": "Rollback instructions for the tool",
    "fallback_script": "reg unload HKLM\\SysTemp"
  }
}
```

Field Definitions

Field Type Description
wmp_version string Protocol version. MUST be 0.1.0-draft
session_id string Unique session identifier for tracking
action_type string Type of surgery. MUST be one of REGISTRY_SURGERY, FILE_SURGERY, EXEC_COMMAND
target_scope.target_os_drive string Drive letter of the offline OS (e.g., D:)
target_scope.target_hive string Registry hive name (e.g., SYSTEM, SOFTWARE)
target_scope.control_set string DYNAMIC if tool should auto-detect; otherwise specific ControlSet
operations[].op string Operation type. MUST be one of REPLACE, DELETE, ADD
operations[].key string Registry key name or file name
operations[].value_type string For REG operations: REG_DWORD, REG_SZ, REG_EXPAND_SZ, etc. For FILE operations: N/A
operations[].value_data string New value data
operations[].file_source string For FILE_SURGERY: source file path
operations[].file_dest string For FILE_SURGERY: destination file path
safety_policy.risk_level string MUST be one of SAFE, WARNING, CRITICAL
safety_policy.require_backup boolean If true, tool MUST create backup before execution
safety_policy.skip_if_exists boolean If true, skip operation if target already exists
fallback.description string Human-readable rollback instructions
fallback.fallback_script string Command to execute for automatic rollback

---

3.2 Tool Execution Feedback (Maintenance Tool → AI)

Upon receiving and executing the WMP frame, the target tool MUST return an atomic state packet back to the AI Brain to complete the self-healing verification cycle.

Payload Schema

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

Field Definitions

Field Type Description
status string Execution status. MUST be one of SUCCESS, PARTIAL_SUCCESS, FAILED
execution_log.backup_created string Path to created backup file (empty if none)
execution_log.affected_rows integer Number of registry keys/files modified
execution_log.error_code integer System error code (0 if no error)
execution_log.error_message string Human-readable error description
system_state.registry_flushed boolean true if changes are persisted to disk
system_state.requires_reboot boolean true if user needs to reboot for changes to take effect

---

4. Safety & Rollback Architecture

Every tool compliant with WMP v0.1.0-draft MUST execute a "Pre-Surgery Snapshot" before making volatile writes to the target offline OS. This ensures that:

1. All registry modifications MUST be preceded by a .reg backup export.
2. All file replacements MUST rename the original file to .bak.
3. If status == FAILED, the tool MUST trigger automatic atomic recovery using the defined fallback.fallback_script logic to ensure zero data corruption.

---

5. Error Handling

5.1 Standard Error Response

If a tool cannot process the request, it MUST return an error response:

```json
{
  "wmp_version": "0.1.0-draft",
  "session_id": "wmp_session_2026_0001",
  "status": "FAILED",
  "execution_log": {
    "error_code": 5,
    "error_message": "Access denied: insufficient privileges"
  }
}
```

5.2 Error Codes (Recommended)

Code Description
0 Success
1 Unknown action_type
2 Malformed JSON payload
3 Target file/registry key not found
4 Permission denied
5 Backup creation failed
6 Rollback failed
99 Unknown error

---

6. Example Workflow

6.1 AI Sends Diagnostic Frame

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
    "description": "Restore original Start value from backup",
    "fallback_script": "reg restore HKLM\\SysTemp D:\\backup.reg"
  }
}
```

6.2 Tool Returns Feedback

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

7. Implementer Notes

7.1 Tool Requirements

· Self-Description: Each compliant tool SHOULD provide a wmp_manifest.json in its root directory:

```json
{
  "name": "PE-SmartFixer",
  "wmp_version": "0.1.0-draft",
  "capabilities": ["REGISTRY_SURGERY", "FILE_SURGERY"],
  "input": "stdin",
  "output": "stdout"
}
```

· Graceful Failure: Tools MUST handle malformed JSON gracefully and return error responses instead of crashing.
· Timeout Handling: Tools SHOULD implement operation timeouts of 60 seconds unless explicitly overridden.

---

End of Specification

