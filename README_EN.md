[README_EN.md.md](https://github.com/user-attachments/files/31334093/README_EN.md.md)
# 🚀 PE-SmartFixer: AI-Powered Multimodal Vision & Offline Registry Surgery Tool for WinPE

`Version: v1.1` │ `Target: WinPE` │ `Core: Pure Go (zero deps)` │ `Brain: Qwen-VL` │ `License: GPL-3.0`

PE-SmartFixer completely revolutionizes the traditionally fractured experience of WinPE maintenance, which heavily relied on manual documentation lookups and script hacking. By leveraging Large Multimodal Models (LMM/VLM) as the intelligent diagnostic gateway, paired with Go's low-level hardware orchestration, it achieves a "Vision-to-Diagnosis, One-Click Direct Fix" paradigm.

> 💡 **Maintainer's Note**: The core architecture of Win32 asynchronous message loops, message dispatching, and offline registry hive mounting has been fully closed-loop. Due to high cloud token consumption, this project is now fully open-sourced. We invite global Go developers and Windows systems experts to collaborate on our upcoming v2.0 evolution!

---

## ✨ v1.0 Core Capabilities (Production Ready)

- ⚡ **Zero-Intermediary Execution**: No script engine wrapper required! The AI vision module directly analyzes BSOD/driver screenshots, and the Go binary immediately performs precision surgery on the offline target system's registry hives.
- 📦 **Zero External Dependencies**: Ditch the bloated Python runtime and heavy frameworks. Built natively on the Win32 API window procedure (`wndProc`) and message loop scheduling. The compiled binary is only a few MBs, guaranteeing bulletproof stability under memory-constrained PE ramdisks.
- 🛡️ **Idempotent Safety Gates**:
  - **Network Pre-flight Inspection**: Asynchronous network status probing ensures the main UI never blocks, gracefully providing fallback options under offline environments.
  - **State Isolation**: Automatically creates `.reg` configuration backups before any mutation, allowing instant manual rollbacks.
  - **Thread-Safe Dispatching**: Implements the native `PostMessage` model with custom `WM_AI_DONE` signals, completely eliminating cross-thread pointer corruption and deadlocks.

### Capable Boundaries (v1.0 Proof of Concept)
- ✅ Fixes `0x0000007B` BSOD caused by storage mode switching (AHCI/RAID/IDE) via offline hive manipulation.
- ✅ Restores critical boot-essential system services accidentally disabled by malware or optimization tools.
- ✅ Automatic patching of missing or corrupted core storage drivers (e.g., `iaStorV.sys`).

---

## 🧠 v1.1 — Long-Term Memory & Multi-Stage Adaptive Risk Fusing

> v1.1 turns the tool from a stateless one-shot repair utility into a **self-adapting repair system with persistent memory**. All new code relies exclusively on the Go standard library, so the binary size is essentially unchanged (amd64: 4.93 MB) and the project remains **100% dependency-free**.

### 1. On-Disk Black Box (Long-Term Memory)

- After every applied repair round, the tool writes `X:\Windows\pe_fix_memory.json` on the **target** system drive (never on the PE itself):
  `{"last_repair_time":"YYYY-MM-DD HH:MM:SS","diagnose_count":X,"last_action":"REG|a;REG|b"}`
- **Atomic write**: content is written to a `.tmp` file and then renamed, so a power loss can never leave a half-written record.
- **Per-machine isolation**: the record travels with the target disk and never pollutes other machines.
- **Forward compatible**: tolerates a UTF-8 BOM and ignores unknown fields.
- **Exact de-duplication**: actions are compared token-by-token (a substring match can no longer swallow `REG|iaStor` into `REG|iaStorV`).

### 2. Multi-Stage Adaptive Risk Fusing

- **24-hour active defence**: on startup, if the last repair is older than 24 hours the system assumes the previous fix succeeded, **deletes the record** and returns to a clean first diagnosis.
- **Hard fuse after 3 consecutive failures**: within 24 hours, `diagnose_count >= 3` makes the tool intercept **locally and issue no network request at all** (saving tokens). The main UI shows a blood-red high-risk warning and locks the "Start Diagnosis" button.
- **Clock-skew safe**: if a dead CMOS battery pushed the clock into the future, the record is treated as *not expired* — conservative fusing, never a free pass.

### 3. Asymmetric Pre-Flight Transaction Rollback

- On startup the tool parses previously failed `REG|<service>` tickets and, **before the AI is consulted again**, restores the previous `Start` value: `reg load` the target SYSTEM hive → **rewrite the path** → `reg import` → `reg unload`.
- ⚠️ **Implementation detail that matters**: paths inside the backup `.reg` are *online* paths (`HKLM\SYSTEM\...`). Importing that file directly only modifies **the PE's own registry**. v1.1 rewrites the mount point (`SYSTEM` → `SysTemp`) so the restore truly lands on the offline target hive — and aborts the rollback if the rewrite fails, rather than silently writing to the wrong place.
- **Non-blocking**: the rollback outcome is merged into the main window text instead of a modal dialog that would stall startup.

### 4. In-Memory Negative Constraint Prompt

- The static `config.json` is **never rewritten**; the historical failure ticket is evaluated at runtime.
- A "historical intensive-care record" is appended to the `systemPrompt`, forcing the model to **seal off every previously failed path**, discard its earlier hypotheses and change its reasoning dimension.
- Special characters (`%`, quotes, backslashes) in the ticket cannot corrupt the prompt.

### 5. Four-Layer Safety Whitelist (defends against wrong AI instructions)

| Layer | Purpose | Blocked example |
|-------|---------|-----------------|
| Instruction format | `REG\|service` (max 2 segments) / `FILE\|name\|relpath` (exactly 3) | `REG\|a\|b` |
| **Service-name whitelist** | letters, digits, `_`, `.`, `-` only | `REG\|svc&calc` (command injection) |
| **File-name whitelist** | rejects `\ / : * ? " < > \|` | `FILE\|a/b.sys\|drivers` |
| **Path-traversal guard** | target must stay inside `X:\Windows\` | `FILE\|x\|..\..\..\Users` |

> Validation runs twice — at parse time and again at execution time — so a bypassed first layer is still stopped before any directory is created or file written.

### 6. Bug Fixes since v1.0

- **🔴 32-bit build was completely unusable on 64-bit Windows.** Root cause: **WOW64 file-system redirection** maps `C:\Windows\System32` to `C:\Windows\SysWOW64` for 32-bit processes, and `SysWOW64\config\SYSTEM` does not exist — so the system drive could not be found (all repairs silently disabled) and repairs targeting `System32` would have written to `SysWOW64`. Fixed by calling `Wow64DisableWow64FsRedirection()` at startup.
- **AI JSON containing Windows paths failed to parse** (`invalid character 'D' in string escape code` because `\D` is not a legal JSON escape). Illegal escapes are now repaired before parsing — idempotently, leaving `\n` / `\uXXXX` untouched.
- **Placeholder hint text did not disappear on click.** Now uses focus-gained/focus-lost handling (a stock Edit control has no `EM_SETCUEBANNER` cue banner without a comctl32 v6 manifest).
- **AI received an empty description** ("no input provided") because the status message was inserted *before* the user input was read and the cleanup pass then removed the trailing text. Reading now happens first, text length uses `GetWindowTextLengthW`, and unclosed marker lines are filtered.
- **Registry modification continued even when its `.reg` backup failed** — a real data-safety hole. The mutation is now aborted when the backup cannot be written.
- **Backup failure reasons are now localised in Chinese**: `Access is denied` → insufficient privileges, `write protected` → read-only media, `I/O error` / `CRC` → suspected bad sectors, etc. (9 categories).
- **Robustness & quality**: 5-second timeouts on every registry command, DNS pre-flight before cloud downloads, `atomic.Pointer` for the cross-thread AI result (removing a theoretical data race), whole-project `gofmt`, zero `go vet` warnings, and **71 automated tests** (100% coverage of the pure logic functions).

### 7. Known Limitations

- A record timestamped *exactly* 24 hours ago is treated as slightly older than 24 hours (process start latency), i.e. it returns to a clean first diagnosis — the threshold semantics are `> 24h`.
- The [WMP specification](WMP_SPEC_EN.md) has **no reference implementation yet**: no stdin/stdout JSON-RPC interface and no `wmp_manifest.json` in this project.
- `CRITICAL` is now a **human-confirmed** action (high-risk warning plus the AI's suggested operations) rather than a hard block.

---

## 🗺️ v2.0 Architecture Roadmap (The Next Frontier)

We are building a highly decoupled, fully offline Edge-AI diagnostics and automation engine. We welcome the global community to co-author this paradigm shift.

```text
               +-----------------------------------------+

               |  User UI Triggers Diagnostics (Win32)   |
               +-----------------------------------------+
                                    |
                                    v
                     [ Network Pre-flight Inspection ]
                                    |
          +-------------------------+-------------------------+

          | Online Channel                                    | Offline Channel (OOM Proof)
          v                                                   v
+-----------------------------+                     +-----------------------------+

| Dashscope Qwen-VL Cloud API |                     | Local U-Disk Hardware Probe |
+-----------------------------+                     +-----------------------------+

          |                                                   | (Mounts physical partition)
          |                                                   v
          |                                         +-----------------------------+

          |                                         | Spawn Local Ollama/llama.cpp|
          |                                         +-----------------------------+

          |                                                   | (Loads 1.5B-3B GGUF Model)
          |                                                   v
          |                                         +-----------------------------+

          |                                         | BaseURL Route to 127.0.0.1  |
          |                                         +-----------------------------+

          |                                                   |
          +-------------------------+-------------------------+
                                    |
                                    v
                     +-------------------------------+

                     | Unified JSON Payload Output   |
                     +-------------------------------+
                                    |
                                    v
                     +-------------------------------+

                     | Native DISM & Registry Surgery|
                     +-------------------------------+
                                    |
                                    v
                     +-------------------------------+

                     |  Deconstruct & Defer Kill     |
                     |  (100% Ramdisk Zero Footprint)|
                     +-------------------------------+
```

### 1. Edge-AI Router via Physical Partition Mounting
- **The Core Anti-Pattern we are breaking**: Bloating the WinPE core image (`Boot.wim`) with gigabytes of local models is a dead end—it instantly triggers Out-Of-Memory (OOM) crashes on older hardware.
- **Our Architecture**: The 2G-4G light-quantized VLM model files (`.gguf`) reside strictly on the **physical USB flash drive partition**. Upon user execution, the Go runtime performs a full-disk hardware scan, identifies the model database path, and asynchronously fires up a single-file inference engine (`llama.cpp` / `ollama`).
- **Memory Ephemerality (`defer kill`)**: The local engine opens a loopback address (`http://127.0.0.1:11434`) compliant with OpenAI payload standard specs. The core payload code stays 100% unchanged. The moment the user exits the app, a low-level OS `kill` signal is issued via `defer`. **The model is completely purged from RAM instantly, leaving a absolute zero-footprint memory footprint on the host machine.**

### 2. Dual-Track Alignment: `.dmp` Binary Crash Snapshot Meeting Textual VLM
- **Native Probe Injection**: Automatically traverses and extracts minidump snapshots from the target host's `Minidump/` directory.
- **Context Synchronization**: Leverages native, lightweight debugging utilities to convert binary dumps to plain text on the fly. The text log and the visual BSOD screenshot are fed synchronously into the LMM pipeline, allowing the model's vast contextual memory of OS internal behaviors to pin-point the root cause, superseding brittle hardcoded binary parsers.

### 3. Native DISM Offline Driver Injection Closed-Loop
- Fully automates driver provisioning for storage controllers (e.g., Intel VMD controller mismatch) without forcing system boot.
- The Go runtime captures the AI's diagnostic JSON payload bill of materials, invokes the native host `DISM` engine offline across volumes, injects matching storage `.inf` assets, and records cold `.bak` snapshots for instant rollback safely.

---

## 🤝 How to Contribute

We welcome upstream Pull Requests and issue discussions on the following tracks:
- **Local Model Routing**: Hardware probing scripts and automated `Ollama` subprocess lifecycle management.
- **Minidump Extraction**: Lightweight text parsing wrappers for `.dmp` analysis.
- **UI Internationalization (i18n)**: Decoupling native Win32 window handles into multi-lingual string matrices.

---

## 📬 Contact & Copyright

- **Author**: ziyouzhiyi666888
- **Email**: 865796217@qq.com
- **License**: Strictly governed under **GNU General Public License v3.0 (GPL-3.0)**. Commercial distributors and PE deployment vendors MUST acquire independent commercial licensing permissions from the author before integration. Unauthorized closed-source distribution is legally prohibited.
