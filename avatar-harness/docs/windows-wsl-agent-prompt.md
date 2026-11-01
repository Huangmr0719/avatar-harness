# 给 Windows Agent 的 WSL 部署 Prompt

将下面的任务交给 Windows 上的 Agent。实际 SSH 主机、端口、用户名使用用户已授权的连接信息；本仓库不保存私钥或 API Key。

---

请直接在这台 Windows 已有的 WSL2 中部署最新版数字人 Avatar Harness，执行安装、配置、构建、连接和验证，直到页面可用。

仓库：https://github.com/Huangmr0719/avatar-harness

先读取仓库根目录 `AGENTS.md`、`avatar-harness/README.md` 和 `avatar-harness/docs/windows-lan-agent.md`，以当前源码和文档为准。应用在 `avatar-harness/` 子目录。不要只给计划或重复询问已经明确授权的常规操作；缺少密钥、SSH 授权或必须人工操作的设备验收时，明确指出那一步，并完成其余独立工作。

部署目标：Go Harness 和两个 SSH 转发都运行在同一个 WSL2 发行版；Windows Edge/Chrome 负责显示、麦克风和摄像头。使用 WSL 用户目录中的 `~/src/avatar-harness`，不在 OneDrive 或 `/mnt/c` 构建，不混用 Windows Node、Go 或 node_modules。复用现有发行版和工具，缺少时安装 Linux Git、Git LFS、Go 1.24+、Node 22.12+（可用 Node 24 LTS）、构建工具。不在客户端安装 Python/CUDA，不下载本地口型或 AU 权重。

1. PowerShell 检查 `wsl --list --verbose`，进入现有 WSL2。检查已有仓库和未提交内容，保留用户改动。全新目录克隆；更新用 `git fetch`、确认分支后 `git pull --ff-only`，不强制覆盖。执行 Git LFS 拉取与校验，按 `assets/characters/lam/current.json` 核对角色 ZIP 的哈希和大小，不能把 LFS 指针当成模型。
2. 在 `avatar-harness/web` 执行 `npm ci`、`npm test`、`npm run build`，成功后回到应用目录执行 `./scripts/go test -race ./...`、`./scripts/build`。必须先生成前端再构建 Go。
3. 配置放在 `~/.local/share/avatar-harness/windows-lan/config.json`，保留已有配置、密钥和会话。新配置参考 `configs/lam-bailian.example.json`，使用本机百炼向导或已有环境变量提供 `DASHSCOPE_API_KEY`。密钥只在本机隐藏输入，不写聊天、命令参数、仓库或交付报告。
4. LLM 用 `deepseek-v4.1-flash-1017`，轻度内部思考（`enable_thinking=true`、`reasoning_effort=low`、`max_tokens=2048`）；ASR 用 `qwen3-asr-flash-realtime`；TTS 用 `qwen-audio-3.1-tts-flash`，当前角色音色 `longtian_v3.1`。Avatar 用 `http://127.0.0.1:18766`，renderer 保持 `lam`、`driver: remote`、`asset_url: /models/lam/avatar.zip`、`speech_jaw_max: 0.32`。顶层 `perception` 使用 FastAPI `http://127.0.0.1:18769`、3000ms 超时。内部思考不能显示或播报。
5. 用这台机器已获授权的 SSH 身份连接 data2，核对可信主机指纹。建立只监听 WSL `127.0.0.1` 的转发：本机 18766 → data2 `127.0.0.1:18766`，本机 18769 → data2 `127.0.0.1:18769`。启用 `ExitOnForwardFailure` 和保活。不要复制其他电脑的私钥，不开放 GPU 服务端口，不重装或重启远端模型、修改其他研究任务。18769 应返回 `au_model=libreface_2_0_repvgg_fm2026`，不能静默降级为旧模型。
6. 健康检查通过后，在应用目录运行 `./start --config "$AVATAR_HARNESS_CONFIG" serve --no-open --host 127.0.0.1 --port 8765`。保持 Harness 和隧道可持续运行，交付对应终端或本机 tmux 会话；不擅自改为开机启动。Windows 浏览器打开 `http://127.0.0.1:8765/`。端口冲突时检查占用并选择空闲本机端口，不杀无关进程或改为公网监听。
7. 分别验证 Windows 与 WSL 的健康接口、头像/WebGL、文本回应、真实声音、麦克风识别、口型及插话取消。开启摄像头只预览；明确开启“表情分析”后才上传，确认固定 12 类 AU、0–5 柱状图及关闭后的清理。检查历史错误能说明原因且不重复堆叠，交流支持侧栏可展开，`/demo/prompts` 可加载。真实模型测试会调用已配置的云服务，用独立测试会话，保留用户历史。

仅当实际 Windows 设备验证了麦克风、有声回应、口型和取消后，才报告完整验收通过。无法代做的步骤明确标为待用户验收，不用健康检查或音频包到达冒充实际起播。最终简要报告：Git 提交、WSL/Go/Node 版本、源码与配置路径、运行 URL、隧道与服务的停止/再启动方式、检查结果和剩余事项。
