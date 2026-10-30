# Windows 原生 PowerShell 部署：备选方案

已有 WSL2 时优先使用 [Windows / WSL2 Agent 部署指南](windows-lan-agent.md)。本页保留不使用 WSL 的原生运行路径。

目标：在一台能通过 SSH 访问 data2 的 Windows 电脑上，运行 Go Harness 和浏览器 LAM 数字人；复用 data2 已部署的模型服务。主路径为原生 PowerShell，无需 WSL、Python、CUDA 或本地 ONNX 权重。

本文适用于当前 `main` 的 `configs/lam-bailian.example.json`。`docs/data2-deployment.md` 记录旧的整机迁移方案，不用于本次 Windows 客户端部署。

## 1. 先确定变量和边界

以下路径互相独立：源码放在普通本地目录，配置、密钥及会话放在当前 Windows 用户的 LocalAppData。不要把 Mac 的 `.avatar-harness/` 或 `secrets.env` 复制过来。data2 的地址、SSH 用户和端口由仓库所有者提供；不要把它们猜成公共示例值。

每个新 PowerShell 终端先执行这段变量设置，并替换三个 `REPLACE_...` 值：

```powershell
$ErrorActionPreference = 'Stop'
$RepoDir = Join-Path $env:USERPROFILE 'source\avatar-harness'
$AppDir = Join-Path $RepoDir 'avatar-harness'
$StateDir = Join-Path $env:LOCALAPPDATA 'AvatarHarness\windows-lan'
$ConfigPath = Join-Path $StateDir 'config.json'
$Data2Host = 'REPLACE_DATA2_HOST'
$Data2User = 'REPLACE_SSH_USER'
$Data2Port = 'REPLACE_SSH_PORT'
$Data2Key = Join-Path $env:USERPROFILE '.ssh\id_ed25519_data2'
$SshArgs = @('-p', $Data2Port, '-i', $Data2Key, '-o', 'IdentitiesOnly=yes', '-o', 'ConnectTimeout=10')
```

`$Data2Key` 必须是这台 Windows 已获准使用的私钥路径。若本机尚无可用身份，请让所有者完成授权；不要从其他机器复制私钥、把密钥交给聊天记录，或自行修改服务器的 SSH 授权配置。已有其他 SSH 认证方式时，相应调整 `$SshArgs`。

部署范围：

| 位置 | 运行内容 |
|---|---|
| Windows | Go HTTP 服务 `127.0.0.1:8765`、浏览器 WebGL 渲染、麦克风、OpenSSH 客户端 |
| 百炼北京地域 | `deepseek-v4.1-flash-1017`、`qwen3-asr-flash-realtime`、`qwen-audio-3.1-tts-flash` |
| data2 GPU 0 | `lam-driver`，官方 LAM-A2E streaming，回环端口 `18766` |
| data2 GPU 1 / 2 | 可选 LAM / LHM++ 形象制作，回环端口 `18767` / `18768` |

同一网络不代表模型端口可直连。保留 data2 的 `127.0.0.1` 监听，通过 SSH 转发；不要改成 `0.0.0.0` 或开放模型防火墙端口。复用 `/data2/huangmr/code/avatar-harness/`，不重新部署 GPU 环境、不启动训练。远程新增文件如有需要也只放在 `/data2/huangmr/code/` 下。

## 2. 检查并补齐 Windows 工具

需要 Windows PowerShell 5.1 或 PowerShell 7、Git、Git LFS、Go 1.24+、Node.js 22.12+、Windows OpenSSH Client，以及支持 WebGL 的 Edge / Chrome。浏览器启用硬件加速；不要求 Windows 有 CUDA 显卡。

```powershell
Get-Command git, go, node, npm.cmd, ssh.exe
git --version
git lfs version
go version
go env GOOS GOARCH
node --version
npm.cmd --version
ssh.exe -V
```

`go env GOOS` 应为 `windows`；架构应与本机一致。仅对缺失或低于最低版本的工具安装 / 更新。可以使用 WinGet：

```powershell
winget install --id Git.Git --exact --source winget
winget install --id GitHub.GitLFS --exact --source winget
winget install --id GoLang.Go --exact --source winget
winget install --id OpenJS.NodeJS.LTS --exact --source winget
```

只运行需要的安装行，每行完成后检查退出状态。安装后重新打开终端，重设第 1 节变量并复查版本。若没有 WinGet，使用 [Git](https://git-scm.com/downloads/win)、[Git LFS](https://git-lfs.com/)、[Go](https://go.dev/dl/) 和 [Node.js](https://nodejs.org/en/download) 的官方安装包。

若缺少 `ssh.exe`，在管理员 PowerShell 安装客户端，随后回到普通用户终端：

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0
```

只需 SSH Client，无需安装或开启 Windows SSH Server。命令依据：[Microsoft OpenSSH 安装说明](https://learn.microsoft.com/en-us/windows-server/administration/openssh/openssh_install_firstuse)。

## 3. 克隆仓库并取回 LFS 资产

以下用于全新目录；若 `$RepoDir` 已存在，先核对其远程仓库、分支与未提交修改，不覆盖目录。

```powershell
New-Item -ItemType Directory -Force (Split-Path $RepoDir -Parent) | Out-Null
git clone https://github.com/Huangmr0719/avatar-harness.git $RepoDir
if ($LASTEXITCODE -ne 0) { throw 'git clone failed' }
Set-Location $RepoDir
git lfs install --local
if ($LASTEXITCODE -ne 0) { throw 'git lfs install failed' }
git lfs pull
if ($LASTEXITCODE -ne 0) { throw 'git lfs pull failed' }
git lfs fsck
if ($LASTEXITCODE -ne 0) { throw 'Git LFS assets are incomplete' }
git rev-parse HEAD

$AvatarFile = Join-Path $AppDir 'web\public\models\lam\avatar.zip'
$AvatarInfo = Get-Content (Join-Path $RepoDir 'assets\characters\lam\current.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if ((Get-Item $AvatarFile).Length -ne $AvatarInfo.bytes -or
    (Get-FileHash $AvatarFile -Algorithm SHA256).Hash.ToLowerInvariant() -ne $AvatarInfo.sha256) {
    throw 'LAM asset does not match current.json; check git lfs pull'
}
```

当前发布角色为 `colorado-2022`，ZIP 为 4,108,445 字节，SHA256 为 `5da20bf3ebde99f2cf2bbc1934674e1f41204bbb13970f0b7b3eadfc269ba3bb`。上面的检查读取同一提交中的来源记录，后续资产升级时以该记录为准。LFS 指针文本不能作为 ZIP 使用。

## 4. 先构建前端，再测试并编译 Go

原生 Windows 不运行 `./scripts/go`、`./scripts/build` 或 `./start`；它们是 Unix shell 脚本。这里使用等价的本机命令，并把 Go 缓存保持在项目内。`npm.cmd` 避免 PowerShell 对 `npm.ps1` 的执行策略限制。

```powershell
Set-Location $AppDir
$env:GOCACHE = Join-Path $AppDir '.cache\go-build'
$env:GOMODCACHE = Join-Path $AppDir '.cache\go-mod'
$env:GOPATH = Join-Path $AppDir '.cache\gopath'
$env:CGO_ENABLED = '0'

Set-Location (Join-Path $AppDir 'web')
npm.cmd ci
if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
npm.cmd test
if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed' }
npm.cmd run build
if ($LASTEXITCODE -ne 0) { throw 'Frontend typecheck/build failed' }

Set-Location $AppDir
go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
New-Item -ItemType Directory -Force (Join-Path $AppDir 'bin') | Out-Null
go build -trimpath -ldflags='-s -w' -o .\bin\avatar-harness.exe ./cmd/avatar-harness
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
```

`web/assets.go` 将 `web/dist` 嵌入可执行文件，因此每次改前端后都要依次重建前端和 Go。构建完成后，运行时不需要 Node.js。Windows 此路径使用普通 Go 测试，不安装 C 工具链来运行 `-race`；缺少可选 `sqlite3` 时，旧 SQLite 导入测试会跳过。Windows 使用 ACL，配置测试中的 POSIX 权限位检查仅适用于非 Windows 平台。

## 5. 创建独立配置并在本机输入密钥

```powershell
New-Item -ItemType Directory -Force $StateDir | Out-Null
if (-not (Test-Path $ConfigPath)) {
    Copy-Item (Join-Path $AppDir 'configs\lam-bailian.example.json') $ConfigPath
}
$HarnessExe = Join-Path $AppDir 'bin\avatar-harness.exe'
& $HarnessExe --config $ConfigPath configure bailian
if ($LASTEXITCODE -ne 0) { throw 'Bailian configuration failed' }
& $HarnessExe --config $ConfigPath doctor
if ($LASTEXITCODE -ne 0) { throw 'Configuration check failed' }
```

在本机交互式终端完成向导：业务空间 ID 按实际账户填写，使用公共域名时留空；三个模型保留第 1 节列出的 ID，默认音色填 `longtian_v3.1`。在隐藏输入处填写有对应模型权限的**百炼北京地域** API Key。Agent 不应把密钥放进命令行、聊天、提交或验收报告。

密钥保存到 `$StateDir\secrets.env`，变量名为 `DASHSCOPE_API_KEY`；配置与会话也位于 `$StateDir`。若启动终端已经注入此环境变量，向导会复用它，环境值优先于 `secrets.env`。需要无人值守配置时，先由本机安全渠道注入该变量，再执行 `configure bailian --defaults`；当前角色仍优先使用其 `voice: longtian_v3.1`。

示例保留这几个关键设置；复用旧配置时逐项核对：

```text
host                              127.0.0.1
port                              8765
providers.avatar.kind             fastapi
providers.avatar.base_url         http://127.0.0.1:18766
characters[0].id                  character-01
characters[0].voice               longtian_v3.1
characters[0].renderer.kind       lam
characters[0].renderer.driver     remote
characters[0].renderer.asset_url  /models/lam/avatar.zip
```

`doctor` 这里只核对配置，不证明云端推理成功。不要将 `renderer.driver` 改为本地模式：当前发布方案不附带本地 ONNX 权重。

## 6. 终端 A：建立 SSH 隧道

先验证网络与身份。首次连接时，使用所有者提供的可信 SSH 主机指纹核对提示，不关闭主机密钥检查。

```powershell
if ($Data2Host -like 'REPLACE_*' -or $Data2User -like 'REPLACE_*' -or $Data2Port -like 'REPLACE_*') {
    throw 'Fill in the data2 connection variables first'
}
if (-not (Test-Path $Data2Key)) { throw 'Provide an authorized local SSH key or adjust SshArgs' }
Test-NetConnection -ComputerName $Data2Host -Port ([int]$Data2Port)
& ssh.exe @SshArgs "$Data2User@$Data2Host" 'true'
if ($LASTEXITCODE -ne 0) { throw 'SSH authentication failed' }

& ssh.exe @SshArgs -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -o ServerAliveCountMax=3 -L '127.0.0.1:18766:127.0.0.1:18766' "$Data2User@$Data2Host"
```

保持此终端运行。正常的 `ssh -N` 没有持续输出；Ctrl+C 结束隧道。这个转发把 **Windows 的** `127.0.0.1:18766` 连接到 **data2 的** `127.0.0.1:18766`。不要把配置改成 `http://data2内网IP:18766`。

## 7. 终端 B：检查 GPU 服务并启动 Harness

新终端重设第 1 节变量，然后运行：

```powershell
$Driver = Invoke-RestMethod -Uri 'http://127.0.0.1:18766/health' -TimeoutSec 10
$Driver | Select-Object status, backend, model, sample_rate, input_samples
if ($Driver.status -ne 'ready' -or $Driver.backend -ne 'cuda' -or
    $Driver.names.Count -ne 52 -or $Driver.input_samples -ne 34133) {
    throw 'The remote LAM driver is not ready'
}
Set-Location $AppDir
& (Join-Path $AppDir 'bin\avatar-harness.exe') --config $ConfigPath serve --no-open --host 127.0.0.1 --port 8765
```

应看到 `official-lam-a2e-streaming`，随后 Harness 输出 `Web http://127.0.0.1:8765`。保持终端 B 运行。

**Windows 停止 / 重启：**在终端 B 按 Ctrl+C，等待进程退出后再改配置、更新构建或重新启动。当前 CLI 的 `stop` 及“配置变化后自动重启”依赖 `os.Process.Signal(os.Interrupt)`，Windows 不支持此调用；不要依赖它们。见 [Go 的平台说明](https://pkg.go.dev/os#Process.Signal)。`--no-open` 后用下一节命令打开浏览器。

## 8. 终端 C：验证本机入口和实际体验

以下健康检查不调用百炼推理，也不运行 GPU 推理：

```powershell
$WebHealth = Invoke-RestMethod -Uri 'http://127.0.0.1:8765/api/health' -TimeoutSec 10
if ($WebHealth.status -ne 'ok' -or $WebHealth.runtime -ne 'go') { throw 'Harness is not ready' }
$Session = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8765/api/sessions' -ContentType 'application/json' -Body '{"character_id":"character-01"}'
$ProxyHealth = Invoke-RestMethod -Uri "http://127.0.0.1:8765/api/sessions/$($Session.id)/driver" -TimeoutSec 10
if ($ProxyHealth.status -ne 'ready' -or $ProxyHealth.backend -ne 'cuda') { throw 'Harness cannot reach the GPU driver' }
Start-Process 'http://127.0.0.1:8765/'
```

这会创建一个不含对话内容的验收会话，用于验证 Go → SSH → GPU 服务的健康路由。

在这台 Windows 上完成以下验收；实际文字 / 语音会调用已配置的百炼服务：

1. 角色正确出现，默认姿态平视；浏览器没有 ZIP 解码或 WebGL 错误。
2. 输入一句简短中文，确认有回复、龙天男声与口型。点击页面后再判断是否有声，以满足浏览器音频播放要求。
3. 点击“开始说话”，允许麦克风，完成一句话的真实识别与回应；Windows 的“隐私和安全性 → 麦克风”也要允许桌面应用访问。
4. 回答播放中再次开口，确认旧音频停止、新一轮正常接续；结束后关闭麦克风。
5. 可选打开 `http://127.0.0.1:8765/demo/emotions`，点击七情绪演示，检查七段实际播放及表情变化。这会使用 TTS 与远程口型，不调用 LLM。

在本机使用 `http://127.0.0.1:8765`，不要改用另一台电脑的 HTTP 内网地址采集麦克风；浏览器把 localhost 视为可信本地来源，普通远程 HTTP 不满足设备权限所需的安全上下文。参见 [MDN：安全上下文](https://developer.mozilla.org/en-US/docs/Web/Security/Secure_Contexts#when_is_a_context_considered_secure)。

不要把健康检查、文字输出或音频包到达当作“三秒语音验收”。若报告延迟，应从最后有声输入帧计时，并依据实际 AudioContext 播放观测；没有完成真实麦克风 / 播放检查时明确记为未验证。

## 9. 可选：连接已有形象制作界面

日常运行当前角色不需要本节。需要制作新形象时，在终端 A 先 Ctrl+C 关闭旧隧道，再用同一组 SSH 变量转发三个端口：

```powershell
$Forwards = @(
    '-L', '127.0.0.1:18766:127.0.0.1:18766',
    '-L', '127.0.0.1:18767:127.0.0.1:18767',
    '-L', '127.0.0.1:18768:127.0.0.1:18768'
)
& ssh.exe @SshArgs -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 -o ServerAliveCountMax=3 @Forwards "$Data2User@$Data2Host"
```

- LAM 头部制作：打开 `http://127.0.0.1:18767/`，上传有使用权限的照片，选择动作，保留导出 ZIP 选项，再生成和下载。现有服务的结果保存在 `/data2/huangmr/code/avatar-harness/outputs/lam/`。
- LHM++ 人体制作：打开 `http://127.0.0.1:18768/`。其 Gaussian PLY 不能直接交给当前 LAM 渲染器，人体显示和驱动尚需另行适配。
- 导入新 LAM ZIP 时，先停止 Harness，把文件保存在本地独立资产目录；在对应角色的 `asset_path` 填绝对路径，`renderer.kind` 保持 `lam`、`renderer.driver` 保持 `remote`，`renderer.asset_url` 改为 `/api/characters/<角色ID>/asset/avatar.zip`，再启动。不要将 ZIP 当作 GLB / VRM 加载。

只打开已有界面不应触发重建。GPU 服务若未运行，先报告具体服务；不要为了客户端部署自动重装模型或重启其他人的任务。服务详情见 [data2-models.md](data2-models.md)。

## 10. 故障定位与更新

| 现象 | 处理 |
|---|---|
| `npm.ps1` 被禁止执行 | 使用本文的 `npm.cmd`，不改全局执行策略。 |
| `all:dist` 找不到文件，或页面仍是旧版 | 先在 `web/` 完成 `npm.cmd run build`，再编译 Go。 |
| ZIP 解码失败、头像空白 | 先检查 `git lfs pull` 与 `current.json` 的 SHA256，再检查 WebGL / 硬件加速。 |
| SSH 超时 | 核对服务器地址、实际 SSH 端口、网络 / VPN 和 `Test-NetConnection`；模型端口不需对外开放。 |
| `Permission denied (publickey)` | 核对用户、本机密钥路径及该公钥是否已获授权；不尝试复制其他机器私钥。 |
| SSH 提示本地端口已占用 | 检查 `Get-NetTCPConnection -State Listen -LocalPort 18766`，复用或关闭自己原来的隧道；不要结束无关进程。 |
| 隧道存在但 `/health` 失败 | 查看 SSH 窗口是否出现远端连接拒绝；用下方只读命令检查服务。 |
| Go 代理返回 409 / 502 | 409：核对当前角色 `lam` / `remote`；502：先检查本机 `18766/health`，再检查 Avatar provider URL。 |
| 百炼 401 / 403 或缺少密钥 | 核对北京地域、模型权限与本机 `DASHSCOPE_API_KEY`；已有环境变量会覆盖 `secrets.env`。重启 Harness 后生效。 |
| JSON 首字符错误、配置无法解析 | PowerShell 5.1 的 `Set-Content -Encoding UTF8` 会写 BOM。保留原始复制文件，或用 `.NET UTF8Encoding(false)` 写无 BOM 的 UTF-8 JSON。 |
| 有文本但无声 / 麦克风不工作 | 点击页面、检查网站权限、Windows 麦克风权限与输出设备；检查是否打开了本机 `127.0.0.1`。 |
| `stop` 报 Windows 不支持的操作 | 回到运行 Harness 的终端按 Ctrl+C；退出后重启，不再尝试自动重启路径。 |

只读远程检查（在设过第 1 节变量的终端中）：

```powershell
& ssh.exe @SshArgs "$Data2User@$Data2Host" 'bash /data2/huangmr/code/avatar-harness/services/modelctl status lam-driver'
```

源码更新：先 Ctrl+C 停止 Harness，检查仓库没有需要保留的未提交改动，再运行 `git -C $RepoDir pull --ff-only`、`git -C $RepoDir lfs pull`，重复资产校验和第 4 节构建。保留 `$StateDir`，不重新覆盖配置与会话。

## 11. Agent 完成报告

记录实际 Git 提交、Windows / Go / Node 版本、源码与配置路径、前端与 Go 检查结果、资产 SHA256、两个健康入口和 Go 代理检查结果，以及当前服务 / 隧道如何停止和再次启动。不要附密钥、私钥内容或完整会话历史。

把结果分清：**构建与连接完成**，以及**Windows 实机语音体验已验收 / 尚待用户验收**。只有实际观察到头像、真实麦克风识别、有声播放、口型与打断后，才报告完整部署验收完成；仅交叉编译不能证明这些行为。

本指南发布前的验证范围（2026-10-31）：Windows amd64 交叉编译及 macOS 上的 Go 全量 race 测试通过；data2 三个现有服务的只读健康检查通过。Windows 本机的安装、物理麦克风与 WebGL 播放仍由执行本指南的 Agent 和用户验收。
