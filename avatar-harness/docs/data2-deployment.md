# data2 部署

> 历史方案归档：下文是空间不足时准备的整机迁移方案，资源快照和“尚未安装”状态已经过时。当前采用 **Mac harness + data2 GPU 模型服务**，部署、访问和验证结果见 [data2-models.md](data2-models.md)。不要按下文迁移或启动第二个 harness。

所有新增服务器文件限定在 `/data2/huangmr/code/avatar-harness/`：

| 目录 | 用途 |
|---|---|
| `source/` | 项目源码与原始模型、角色资产 |
| `bin/` | Linux amd64 静态 Go 可执行文件，内含 Web 与运行资源 |
| `runtime/state/` | 配置、密钥、会话和服务状态 |
| `runtime/logs/` | 服务日志 |
| `runtime/cache/` | 模型、下载及运行缓存 |
| `runtime/tmp/` | 临时文件和本项目独立 tmux socket |

部署脚本不会删除服务器上的其他项目，不安装系统包，不在服务器用户主目录中保存项目模型或缓存。SSH 只使用现有 `data2` 配置；服务器服务仅监听回环地址，通过隧道访问。

## 实际检查（2026-10-30）

- SSH 已连通。4 × RTX 3090，每卡 24576 MiB，总空闲约 24258 MiB/卡，检查时利用率 0%。这些是当次资源快照。
- Linux amd64、glibc 2.27、驱动 535.154.05、CUDA 编译工具 11.8、系统 Python 3.6.9，Node 22.23.3。Go 静态交叉编译包已在 Mac 生成，服务器运行不依赖 Go 或 Python 安装。
- 已有 `au_gen` 环境为 Python 3.10.21、PyTorch 2.2.2+cu118、FastAPI 0.116.1。还同时登记了 CPU ONNX Runtime 1.23.2 和 GPU 包 1.16.3，不能只看包名就认为 CUDA 推理可用；后续 GPU 服务应使用独立环境并做实际推理验证。
- `/data2` 普通用户可用字节为 **0**，小文件写入返回 `ENOSPC`。含系统预留的空闲约 200GB，不等于当前账号可用。当前尚未完成服务器安装或启动。

## 可迁移范围

第一阶段直接迁移已有 Go Harness、工具调度和 Web/模型资源服务，百炼 LLM、ASR、情感 TTS 仍使用原 API；原有七类情感、角色男声与取消机制保留。LAM 高斯渲染、麦克风/摄像头采集、WASM 口型仍在 Mac 浏览器，服务器不会代替浏览器运行相机或渲染。

GPU 语音表情推理、视觉 AU、语音情感与 rPPG 可作为后续外部模型服务。当前没有把未适配的 GPU 模型标记成已部署；仅复制 ONNX 文件也不会让现有 WASM 前端自动改用远程 CUDA。现有三秒起播记录不能直接套用到 SSH 隧道或新 GPU 链路。

## 释放空间后部署

建议先释放至少 5GB；脚本要求普通用户至少有 1GiB 可用。先在 Mac 的应用目录完成前端构建，再执行：

```sh
bash scripts/deploy-data2 --check
bash scripts/deploy-data2 --with-secrets
```

`--with-secrets` 通过 SSH 单独传输本机现有 `secrets.env`，权限 0600，不进入源码和 Git；不加该参数只部署程序。首次配置采用不含密钥的 `configs/lam-bailian.example.json`。重复部署保留服务器现有配置、角色和会话，更新本项目源码与可执行文件并重启本项目服务。

Mac 单独开启访问隧道，避免占用已有本地工作台的 8765：

```sh
ssh -N -L 18765:127.0.0.1:8765 data2
```

打开 `http://127.0.0.1:18765/`。localhost 满足浏览器麦克风的安全上下文要求。SSH 断开不影响 tmux 中的服务器进程。

启停检查：

```sh
ssh data2 'bash /data2/huangmr/code/avatar-harness/source/avatar-harness/scripts/data2-runtime status'
ssh data2 'bash /data2/huangmr/code/avatar-harness/source/avatar-harness/scripts/data2-runtime stop'
ssh data2 'bash /data2/huangmr/code/avatar-harness/source/avatar-harness/scripts/data2-runtime start'
```

验收应分别记录：服务器健康检查、真实百炼调用、浏览器渲染、合成录音实际起播和打断。静态编译成功只说明程序构建通过，不能当作部署或实时验收。
