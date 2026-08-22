[README_EN.md.md](https://github.com/user-attachments/files/31334093/README_EN.md.md)
# 🚀 PE-SmartFixer: AI-Powered Multimodal Vision & Offline Registry Surgery Tool for WinPE

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
