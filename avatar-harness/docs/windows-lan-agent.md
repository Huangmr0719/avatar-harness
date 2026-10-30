# Windows / WSL2 同网段部署：Agent 执行指南

适用场景：Windows 已安装 WSL，能访问 data2 所在网络。**Go Harness 和 SSH 隧道运行在同一个 WSL2 发行版；Edge / Chrome、麦克风及 LAM WebGL 渲染运行在 Windows。** data2 复用现有 GPU 模型服务，百炼提供 LLM、ASR 和 TTS。客户端无需 Python、CUDA 或本地 ONNX 权重。

不使用 WSL 时，见 [原生 PowerShell 备选方案](windows-native-agent.md)。旧的 `data2-deployment.md` 是整机迁移记录，不用于本次客户端部署。

## 1. 确认已有 WSL2

在 **Windows PowerShell** 中：

```powershell
wsl --list --verbose
wsl --status
wsl -d <已有发行版名称>
```

最后一行替换成实际名称，例如 `wsl -d Ubuntu-24.04`。选择 `VERSION` 为 `2` 的已有发行版，不重新安装 WSL。若只有 WSL1，先报告差异；转换会影响现有环境，由所有者决定，或改用原生方案。命令依据：[Microsoft WSL 基本命令](https://learn.microsoft.com/en-us/windows/wsl/basic-commands)。

以下未特别标注的命令均在该发行版的 **Bash** 中执行，使用普通 Linux 用户：

```bash
cat /etc/os-release
uname -m
command -v git git-lfs curl jq node npm go gcc || true
```

源码放在 WSL 自己的 `/home/...` 文件系统，不放 OneDrive 或 `/mnt/c/...`，以减少文件读取与权限差异。见 [Microsoft WSL 文件存储建议](https://learn.microsoft.com/en-us/windows/wsl/filesystems)。

## 2. 设置路径、连接参数和工具

**每个新 WSL 终端先执行同一段变量设置**，替换三个 `REPLACE_...` 值：

```bash
export HARNESS_REPO="$HOME/src/avatar-harness"
export HARNESS_APP="$HARNESS_REPO/avatar-harness"
export AVATAR_HARNESS_CONFIG="$HOME/.local/share/avatar-harness/windows-lan/config.json"
export HARNESS_DATA2_HOST='REPLACE_DATA2_HOST'
export HARNESS_SSH_USER='REPLACE_SSH_USER'
export HARNESS_SSH_PORT='REPLACE_SSH_PORT'
export HARNESS_SSH_KEY="$HOME/.ssh/id_ed25519_data2"
export PATH="$HARNESS_APP/.cache/node/bin:$PATH"
harness_ssh=(-p "$HARNESS_SSH_PORT" -i "$HARNESS_SSH_KEY" -o IdentitiesOnly=yes -o ConnectTimeout=10)
```

地址、用户、实际 SSH 端口由所有者提供。`HARNESS_SSH_KEY` 指向本机已获授权的 WSL 私钥；有现成 SSH agent 或其他认证方式时调整 `harness_ssh`。若缺少身份，让所有者完成本机授权；不要从 Mac 或服务器复制私钥，也不要把密钥写入聊天或仓库。

Ubuntu / Debian 缺少基础工具时安装：

```bash
sudo apt-get update
sudo apt-get install -y git git-lfs curl ca-certificates jq xz-utils build-essential
```

其他发行版安装同等工具，无需更换发行版。`build-essential` 提供 Go race 测试使用的 C 编译器。Go 至少 1.24，Node.js 至少 22.12；优先复用 WSL 内已有版本，不能使用 `/mnt/c/.../node.exe` 或 Windows 的 `node_modules`。

## 3. 克隆并校验角色资产

以下用于全新目录。若 `$HARNESS_REPO` 已存在，先核对远程、分支与本地改动，不覆盖目录。

```bash
set -euo pipefail
mkdir -p "$(dirname "$HARNESS_REPO")"
git clone https://github.com/Huangmr0719/avatar-harness.git "$HARNESS_REPO"
cd "$HARNESS_REPO"
git lfs install --local
git lfs pull
git lfs fsck
git rev-parse HEAD

harness_avatar='avatar-harness/web/public/models/lam/avatar.zip'
harness_expected_sha=$(jq -r '.sha256' assets/characters/lam/current.json)
harness_expected_bytes=$(jq -r '.bytes' assets/characters/lam/current.json)
printf '%s  %s\n' "$harness_expected_sha" "$harness_avatar" | sha256sum --check -
test "$(stat -c %s "$harness_avatar")" = "$harness_expected_bytes"
```

当前角色为 `colorado-2022`，ZIP 为 4,108,445 字节，SHA256 为 `5da20bf3ebde99f2cf2bbc1934674e1f41204bbb13970f0b7b3eadfc269ba3bb`。脚本以同一提交的 `current.json` 为准；LFS 指针文本不能作为 ZIP 加载。

### 仅在 Go / Node 缺失或版本过低时安装

在 `$HARNESS_APP` 中检查 `./scripts/go version`、`node --version`、`npm --version`；满足要求则跳过本小节。下面从官方分发站下载 Linux 版本、核对 SHA256，安装到项目缓存，不覆盖系统安装。

```bash
cd "$HARNESS_APP"
case "$(uname -m)" in
  x86_64) harness_go_arch=amd64; harness_node_arch=x64 ;;
  aarch64) harness_go_arch=arm64; harness_node_arch=arm64 ;;
  *) printf '%s\n' 'Unsupported architecture; report it before continuing.'; exit 1 ;;
esac
harness_download=$(mktemp -d)
```

**仅需安装 Go 时：**

```bash
test ! -e .cache/go-sdk/go  # 已有项目 SDK 时先检查，不覆盖
curl -fsSL 'https://go.dev/dl/?mode=json' -o "$harness_download/go-releases.json"
harness_go_file=$(jq -r --arg arch "$harness_go_arch" '[.[] | select(.stable)][0].files[] | select(.os == "linux" and .arch == $arch and .kind == "archive") | .filename' "$harness_download/go-releases.json")
harness_go_sha=$(jq -r --arg file "$harness_go_file" '.[].files[] | select(.filename == $file) | .sha256' "$harness_download/go-releases.json")
test -n "$harness_go_file" && test -n "$harness_go_sha"
curl -fL "https://go.dev/dl/$harness_go_file" -o "$harness_download/$harness_go_file"
printf '%s  %s\n' "$harness_go_sha" "$harness_download/$harness_go_file" | sha256sum --check -
mkdir -p .cache/go-sdk
tar -xzf "$harness_download/$harness_go_file" -C .cache/go-sdk
./scripts/go version
```

`./scripts/go` 自动选用此 SDK，并保持 Go 缓存位于项目内。来源：[Go 安装文档](https://go.dev/doc/install)与[官方下载目录](https://go.dev/dl/)。

**仅需安装 Node 时：**

```bash
test ! -e .cache/node  # 已有项目 Node 时先检查，不覆盖
curl -fsSL 'https://nodejs.org/dist/latest-v24.x/SHASUMS256.txt' -o "$harness_download/node-sha256.txt"
harness_node_file=$(awk -v arch="$harness_node_arch" '$2 ~ ("^node-v24[.][0-9.]+-linux-" arch "[.]tar[.]xz$") {print $2}' "$harness_download/node-sha256.txt")
test -n "$harness_node_file"
curl -fL "https://nodejs.org/dist/latest-v24.x/$harness_node_file" -o "$harness_download/$harness_node_file"
awk -v file="$harness_node_file" '$2 == file {print $1 "  " file}' "$harness_download/node-sha256.txt" > "$harness_download/node-check.txt"
(cd "$harness_download" && sha256sum --check node-check.txt)
mkdir -p .cache/node
tar -xJf "$harness_download/$harness_node_file" -C .cache/node --strip-components=1
export PATH="$HARNESS_APP/.cache/node/bin:$PATH"
node --version
npm --version
```

使用 Node 24 LTS 的 Linux 包；来源：[Node.js 官方下载](https://nodejs.org/en/download)。新终端执行第 2 节变量块即可恢复 PATH。

## 4. 构建和离线测试

```bash
cd "$HARNESS_APP/web"
npm ci
npm test
npm run build
cd "$HARNESS_APP"
./scripts/go test -race ./...
./scripts/build
```

每步成功后才继续。`web/assets.go` 嵌入 `web/dist`，所以前端必须先构建；`./start` 和 `./scripts/build` 不会生成前端。测试使用本地模拟服务；可选 `sqlite3` 不存在时只跳过旧数据库导入测试。

## 5. 创建独立配置，在本机输入百炼密钥

```bash
cd "$HARNESS_APP"
umask 077
mkdir -p "$(dirname "$AVATAR_HARNESS_CONFIG")"
if [ ! -e "$AVATAR_HARNESS_CONFIG" ]; then
  cp configs/lam-bailian.example.json "$AVATAR_HARNESS_CONFIG"
fi
./start --config "$AVATAR_HARNESS_CONFIG" configure bailian
./start --config "$AVATAR_HARNESS_CONFIG" doctor
```

在本机交互式终端隐藏输入 API Key。业务空间 ID 按实际账户填写，公共域名可留空；模型保持下表，默认音色填 `longtian_v3.1`。密钥须属于百炼北京地域且有对应模型权限。

| 项目 | 值 |
|---|---|
| LLM | `deepseek-v4.1-flash-1017`，关闭思考模式 |
| ASR | `qwen3-asr-flash-realtime` |
| TTS | `qwen-audio-3.1-tts-flash` |
| 当前角色音色 | `longtian_v3.1` |
| Avatar provider | `fastapi`，`http://127.0.0.1:18766` |
| 当前角色 renderer | `kind: lam`，`driver: remote`，`asset_url: /models/lam/avatar.zip` |

配置目录同时保存 `secrets.env`、会话与进程记录，不复制 Mac 的运行目录。`DASHSCOPE_API_KEY` 环境变量优先于 `secrets.env`。若已有本机安全渠道注入该变量，可用 `configure bailian --defaults` 免交互保存预设；角色自身的龙天音色优先于通用默认音色。密钥不要出现在命令参数、聊天或报告中。

`doctor` 此时只核对配置，不证明云端推理成功。远程模式不加载本地 ONNX 模型，不要改为本地驱动。

## 6. WSL 终端 A：保持 SSH 隧道

先执行第 2 节变量块。首次 SSH 连接，用所有者提供的可信主机指纹核对提示。

```bash
set -euo pipefail
[[ "$HARNESS_DATA2_HOST" != REPLACE_* && "$HARNESS_SSH_USER" != REPLACE_* && "$HARNESS_SSH_PORT" != REPLACE_* ]]
test -f "$HARNESS_SSH_KEY"
ssh "${harness_ssh[@]}" "$HARNESS_SSH_USER@$HARNESS_DATA2_HOST" true
ssh "${harness_ssh[@]}" -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -o ServerAliveCountMax=3 \
  -L 127.0.0.1:18766:127.0.0.1:18766 "$HARNESS_SSH_USER@$HARNESS_DATA2_HOST"
```

保持窗口运行，正常的 `ssh -N` 没有持续输出。Go 与隧道都在同一 WSL 发行版，provider URL 始终是 WSL 自己的 `http://127.0.0.1:18766`。

data2 的 `lam-driver` 位于 `/data2/huangmr/code/avatar-harness/`，GPU 0，端口 18766，仍只监听服务器 `127.0.0.1`。同一网络也使用隧道，不改为 `0.0.0.0`，不开放模型防火墙端口，不重新部署环境或启动训练。远程新增文件如有必要也只放 `/data2/huangmr/code/` 下。

## 7. WSL 终端 B：健康检查并启动服务

新终端执行第 2 节变量块，再运行：

```bash
set -euo pipefail
curl --noproxy '*' -fsS --max-time 10 http://127.0.0.1:18766/health |
  jq -e '.status == "ready" and .backend == "cuda" and .model == "official-lam-a2e-streaming" and (.names | length) == 52 and .input_samples == 34133'
cd "$HARNESS_APP"
./start --config "$AVATAR_HARNESS_CONFIG" serve --no-open --host 127.0.0.1 --port 8765
```

健康判断应输出 `true`，Harness 随后输出 `Web http://127.0.0.1:8765`。保持终端 B 运行。

另一个 WSL 终端进行无推理的连接验收：

```bash
set -euo pipefail
curl --noproxy '*' -fsS http://127.0.0.1:8765/api/health | jq -e '.status == "ok" and .runtime == "go"'
harness_session=$(curl --noproxy '*' -fsS -X POST http://127.0.0.1:8765/api/sessions \
  -H 'Content-Type: application/json' -d '{"character_id":"character-01"}' | jq -er '.id')
curl --noproxy '*' -fsS "http://127.0.0.1:8765/api/sessions/$harness_session/driver" |
  jq -e '.status == "ready" and .backend == "cuda"'
```

这只创建不含对话内容的验收会话，检查 Go → SSH → GPU 服务健康路由，不调用百炼或 GPU 推理。

## 8. Windows 浏览器验收

在 **Windows PowerShell** 中：

```powershell
Invoke-RestMethod -Uri 'http://127.0.0.1:8765/api/health' -TimeoutSec 10
Start-Process 'http://127.0.0.1:8765/'
```

Windows 通常能通过 localhost 访问 WSL2 内的服务。使用 Windows Edge / Chrome，无需 WSL GUI 浏览器。依据：[Microsoft WSL 网络说明](https://learn.microsoft.com/en-us/windows/wsl/networking)。

在该电脑完成以下检查；实际对话会使用已配置的百炼服务：

1. 角色出现、默认姿态平视，无 ZIP 解码或 WebGL 错误；浏览器启用硬件加速。
2. 输入一句简短中文，确认回复、龙天男声和口型；先点击页面以启用音频播放。
3. 点击“开始说话”，允许麦克风，完成真实识别和有声回应。权限由 Windows 浏览器处理，不需要把麦克风转接给 WSL。
4. 回答播放中再次说话，确认旧音频停止、新一轮接续；完成后关闭麦克风。
5. 可选访问 `http://127.0.0.1:8765/demo/emotions` 运行七情绪演示，观察七段播放和表情。它使用 TTS / 远程口型，不调用 LLM。

使用本机 `127.0.0.1`；普通远程 HTTP 内网地址不满足麦克风 API 的安全上下文要求。参见 [MDN 安全上下文](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts)。

报告语音延迟时，从最后有声输入帧计时，依据 AudioContext 实际播放观测；健康检查、文本输出和音频包到达不算“三秒语音验收”。未做真实设备检查时明确标注尚未验收。

## 9. 可选形象制作

当前角色已随仓库发布，日常运行无需本节。需要制作时，在终端 A 按 Ctrl+C 关闭旧隧道，再替换为：

```bash
ssh "${harness_ssh[@]}" -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -o ServerAliveCountMax=3 \
  -L 127.0.0.1:18766:127.0.0.1:18766 \
  -L 127.0.0.1:18767:127.0.0.1:18767 \
  -L 127.0.0.1:18768:127.0.0.1:18768 "$HARNESS_SSH_USER@$HARNESS_DATA2_HOST"
```

- Windows 浏览器打开 `http://127.0.0.1:18767/`：LAM 单图头部制作，上传有使用权限的照片，选择动作，保留导出 ZIP 选项后生成。现有 GPU 1 服务输出位于 `/data2/huangmr/code/avatar-harness/outputs/lam/`。
- `http://127.0.0.1:18768/` 是 GPU 2 的 LHM++ 人体制作。其 Gaussian PLY 不能直接用于当前 LAM 渲染器，人体显示与驱动需另行适配。
- 导入新 LAM ZIP：先停止 Harness，将文件放入 WSL 本地资产目录；对应角色 `asset_path` 填 WSL 绝对路径，`renderer.kind` 保持 `lam`、`renderer.driver` 保持 `remote`，`renderer.asset_url` 改为 `/api/characters/<角色ID>/asset/avatar.zip`，再启动。ZIP 不能交给 GLTFLoader。

只打开界面不应触发重建。服务未运行时报告具体服务，不自动重装模型或重启他人任务。详见 [data2 模型服务](data2-models.md)。

## 10. 故障、停止与更新

| 现象 | 定位与处理 |
|---|---|
| `node` / `npm` 来自 `/mnt/c`，或平台依赖失败 | 使用 WSL Linux 版本，在 WSL 文件系统重新 `npm ci`，不共享 Windows 依赖。 |
| `all:dist` 无匹配文件，或页面还是旧版 | 先 `npm run build`，再 `./scripts/build`；`./start` 不构建前端。 |
| ZIP 解码失败 / 头像空白 | 检查 LFS、`current.json` 的 SHA256，再检查浏览器硬件加速。 |
| SSH 超时或拒绝认证 | 核对地址、实际 SSH 端口、网络 / VPN、WSL 身份；不直连服务器 18766。 |
| WSL 的 `18766/health` 失败 | 检查隧道窗口的连接拒绝信息，再执行下方只读远程检查。 |
| WSL 能访问 8765，Windows 不能 | 先排查 Windows 是否有进程占用 8765；再检查 WSL localhost 转发。默认 NAT 模式下，查看 `%UserProfile%\.wslconfig` 的 `localhostForwarding` 是否被显式关闭。更改配置或重启 WSL 会影响其他任务，先说明影响；不要直接开放 `0.0.0.0` 或添加公网端口代理。 |
| Go 代理返回 409 / 502 | 409：当前角色应为 `lam` + `remote`；502：检查 `18766/health` 和 Avatar URL。 |
| 缺少密钥或百炼 401 / 403 | 核对北京地域、模型权限与环境变量覆盖关系；修改后重启 Harness。 |
| 有文本但无声 / 麦克风 | 点击页面，检查网站权限、Windows 麦克风权限、输入 / 输出设备和本机 URL。 |

只读远程检查，不启动模型：

```bash
ssh "${harness_ssh[@]}" "$HARNESS_SSH_USER@$HARNESS_DATA2_HOST" \
  'bash /data2/huangmr/code/avatar-harness/services/modelctl status lam-driver'
```

停止时在 Harness 终端按 Ctrl+C，再关闭隧道。也可在同一 WSL、同一配置下执行：

```bash
cd "$HARNESS_APP"
./start --config "$AVATAR_HARNESS_CONFIG" stop
```

再次启动：新终端重设第 2 节变量，建立隧道、检查健康并 `serve --no-open`，不必重新填写密钥。

更新前先停止 Harness，确认没有需要保留的未提交改动，再 `git -C "$HARNESS_REPO" pull --ff-only`、`git -C "$HARNESS_REPO" lfs pull`，重复资产校验和第 4 节构建。保留独立配置目录，不覆盖会话与密钥。

## 11. 完成报告与已有验证

Agent 报告实际 Git 提交、WSL 版本 / 发行版、Go / Node 版本、源码与配置路径、测试 / 构建结果、资产 SHA256、WSL 和 Windows 两侧健康检查，以及浏览器实测情况。说明服务如何停止 / 再启动，不附密钥、私钥或完整会话。

分别说明 **构建与连接完成** 和 **Windows 实机语音体验已验收 / 尚待用户验收**。观察到头像、真实麦克风识别、声音播放、口型与打断后，才称完整部署验收完成。

发布前已有证据（2026-10-31）：macOS 上 Go 全量 race 测试通过；前端沿用同版源码已通过的 53 项测试与类型检查 / 构建；Linux amd64 与 Windows amd64 交叉编译通过；data2 三个现有服务只读健康检查通过。这些不代替目标 Windows 的 WSL 网络、WebGL 与物理麦克风验收。
