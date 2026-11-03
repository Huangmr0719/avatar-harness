# data2 内网网页部署

客户端只需要桌面浏览器；Go Harness、云端语音接入、模型调用和视频存储在 data2。LAM 高斯渲染以及麦克风/摄像头采集仍由访问者的浏览器完成。

## 推荐：SSH 隧道访问，免装证书

最省事的方式是双击启动文件：macOS 使用 [`open-data2-web.command`](../scripts/open-data2-web.command)，Windows 使用 [`open-data2-web.cmd`](../scripts/open-data2-web.cmd)。它们使用当前部署的服务器地址和该电脑已有的 SSH 登录身份，连通后自动打开网页，终端窗口保持连接；已连接时 macOS 文件直接打开网页。首次 SSH 登录仍按终端提示完成。SSH 连接与网页加载已在 macOS 实际验证；Windows 启动文件需在目标电脑验证。

服务器已部署完成，客户端无需克隆或构建项目。使用客户端自己的 SSH 登录身份，在 Windows PowerShell 或 macOS 终端执行：

```bash
ssh -t -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -L 127.0.0.1:18765:127.0.0.1:8765 data2 "bash /data2/huangmr/code/avatar-harness/web-service/service start && exec bash -l"
```

`data2` 是已配置的 SSH 别名；没有别名时将其替换为 `-p SSH端口 用户名@服务器内网IP`。首次连接核对服务器 SSH 指纹，使用该电脑已有的授权身份登录。

保持这个终端连接，在同一电脑的桌面浏览器打开 **http://127.0.0.1:18765/**。本机 18765 映射到服务器的 8765，避免与客户端可能运行的本地工作台冲突。网页登录由 SSH 连接授权，无需再输入 HTTPS 网页口令；麦克风和摄像头仍由浏览器询问权限。浏览器将回环地址视为安全上下文，无需导入 HTTPS 证书，依据见 [MDN 安全上下文](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts)。

在 SSH 终端输入 `exit` 关闭连接和隧道；服务器服务继续运行。需要停止服务器服务时使用下方 `service stop` 命令。客户端若提示本机 18765 被占用，改用空闲本机端口，并同步修改浏览器地址。

本方式已验证网页 200、视频范围请求 206 和实时 ASR WebSocket 就绪；实际 Windows 麦克风、扬声器起播及音画同步仍由该电脑验收。

## 制作形象并自动加入角色列表

右上角 **制作形象** 打开同源 `/create-avatar/`，复用 data2 的 LAM 网页。上传照片、可选填写形象名称后生成；成功后会自动保存，显示“已加入角色列表”。回到原工作台窗口，角色下拉框自动更新，原有交谈和角色保持原样。保存失败时保留生成的 ZIP，并提供“重试保存”，不必重新建模。

服务器私有配置顶层启用入口：

```json
"avatar_creator": { "base_url": "http://127.0.0.1:18767" }
```

LAM 服务使用仓库中的 `services/lam-creator/launch.py` 和 `harness_ui.py`。生成完成回调默认上传至同机 `http://127.0.0.1:8765/api/characters/import`；Harness 使用其他监听端口时，在 LAM 服务设置 `AVATAR_HARNESS_IMPORT_URL`。回调接收端应为实际承载工作台的 Harness。浏览器无需额外转发 18767 端口。

新角色以内容哈希去重，使用当前默认角色的音色、人格和 PAD，配备远程 LAM 驱动。资产保存在 `runtime/state/characters/`，列表保存在 `runtime/state/characters.json`，重启后仍可选择。照片、生成资产、角色列表和密钥都属于私有运行数据，不随代码提交 GitHub。

删除不满意的形象：先在左上角选择该自定义角色，再点击旁边的“删除形象”并确认。角色会从列表移除，Harness 保存的 ZIP 会被清理；相关历史会话保留并切回默认角色。内置角色没有删除入口。此操作不删除制作服务保留的源照片或原始导出。

## 服务布局

- 独立目录：`/data2/huangmr/code/avatar-harness/web-service/`。
- `bin/avatar-harness` 为 Linux amd64 静态程序，内嵌网页和现有 LAM 资产。
- `bin/caddy` 提供内网 HTTPS 和访问口令，使用官方静态发布包及官方校验文件。
- `runtime/state/config.json`、`secrets.env`、访问身份和会话仅保存在服务器私有目录。
- `media/` 保存当前正念 MP4，不进入 Git。来源和用途限制沿用媒体记录。
- 工作台仅监听 `127.0.0.1:8765`；模型继续使用服务器回环 `18766`、`18769`。
- 网关只绑定实际内网 IP 的 `8443` 端口，无公网隧道或公网入口。

部署网关时，将 `configs/Caddyfile.lan.example` 复制为服务器私有的 `Caddyfile`，替换内网 IP、访问用户名和密码哈希。不得把口令明文或真实哈希写回公开模板。网关对网页、API、视频、SSE 和 WebSocket 统一要求认证，代理关闭响应缓冲。

## 启停

服务器 `service` 是 `scripts/data2-web-runtime` 的部署副本，使用独立 tmux socket，与现有模型服务分开管理：

```bash
ssh data2 'bash /data2/huangmr/code/avatar-harness/web-service/service start'
ssh data2 'bash /data2/huangmr/code/avatar-harness/web-service/service status'
ssh data2 'bash /data2/huangmr/code/avatar-harness/web-service/service restart'
ssh data2 'bash /data2/huangmr/code/avatar-harness/web-service/service stop'
```

它在 SSH 退出后继续运行；当前不宣称已经设置机器重启后的自动启动。日志在 `runtime/logs/`。后台工作台和网关由两个 tmux 会话管理，停止操作只针对这个目录的工作台与网关。

## 百炼配置

若服务器外网直连受限，可在私有配置顶层设置 `cloud_proxy_url`，例如 `http://127.0.0.1:3128`。它只作用于 OpenAI 兼容 LLM 和百炼 ASR/TTS 的出站请求；本机 FastAPI 模型服务保持直连。支持 HTTP CONNECT 和 SOCKS5，复用已有 HTTP 连接池。不要把带代理认证信息的地址提交 Git。

在服务器自己的终端中配置密钥：

```bash
ssh -t data2 '/data2/huangmr/code/avatar-harness/web-service/bin/avatar-harness --config /data2/huangmr/code/avatar-harness/web-service/runtime/state/config.json configure bailian'
ssh data2 'bash /data2/huangmr/code/avatar-harness/web-service/service restart'
```

密钥保存在 `runtime/state/secrets.env`，权限为 `0600`。迁移已有机器的密钥需要先得到所有者对源文件与目标服务器的明确授权；程序部署本身不视为密钥迁移授权。缺少密钥时，网页、资产、视频和 GPU 感知仍可验证，云端实时语音端点返回未配置。

## 可选：直接内网 HTTPS 的浏览器证书

内网 IP 使用 Caddy 专用 CA，根证书为：

`runtime/caddy-data/caddy/pki/authorities/local/root.crt`

通过已授权 SSH 等可信渠道把这个**公开证书**交给访问者。CA 私钥始终留在服务器私有目录。访问者自行决定是否将该 CA 加入其设备的受信任证书；网页部署不会自动修改客户端信任设置。

macOS 当前用户可在确认来源后安装根证书：

```bash
security add-trusted-cert -r trustRoot -p ssl \
  -k "$HOME/Library/Keychains/login.keychain-db" \
  /实际下载路径/data2-avatar-root.crt
```

然后重新打开浏览器，访问提供的内网 HTTPS 地址并输入网页访问口令。麦克风与摄像头仍需由访问者在浏览器授权。证书错误页面不能代替证书安装。

Windows 用户把公开证书保存为 `Downloads\data2-avatar-root.crt` 后，在 PowerShell 执行以下命令，将其加入**当前用户**的根证书存储；确认系统提示后重新打开 Edge/Chrome：

```powershell
certutil -user -addstore Root "$env:USERPROFILE\Downloads\data2-avatar-root.crt"
```

命令依据：[Microsoft certutil 文档](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/certutil#-addstore)。客户端无需安装 Go、Node、模型或配置 SSH 隧道。

## 本次部署验证（2026-11-03）

- 内网 HTTPS 网页、API、视频范围读取、SSE 和 WebSocket 均通过；未认证请求返回 401，跨来源 WebSocket 返回 403。
- LAM 资产校验通过；52 通道驱动推理可用；LibreFace/EmotiEffNet 检出人脸并返回 12 个 AU。
- 百炼通过服务器已有的 `127.0.0.1:3128` 代理连接；模型请求仍直连回环地址。
- 两次合成语音按实时节奏输入的测试，均在说完前返回识别片段；首个回复音频包分别约 1773 ms、1728 ms 到达。
- 第二次回复打断成功，取消确认约 0.6 ms；这些是服务器本机传输测量，不能代表浏览器扬声器停止时间。
- Go 全套 race 测试通过。浏览器实际起播、音画同步和麦克风体验仍需在客户端信任证书后验收，以上数据不作为三秒体验目标的完整验收。

## 适用边界

当前是带口令的共享工作台，同一登录身份下的会话可共享，未实现不同用户之间的账号与数据隔离。正式多人独立使用前应增加身份和存储隔离；这与多台电脑访问同一测试工作台是不同要求。

健康检查、HTTPS 认证、静态资产、视频范围读取、SSE、WebSocket、真实云端语音及浏览器实际起播分别记录。服务器健康不代表客户端麦克风和三秒语音目标已经验收。

依据：[浏览器媒体采集的 HTTPS 要求](https://developer.mozilla.org/en-US/docs/Web/API/MediaDevices/getUserMedia)、[Caddy 内部证书](https://caddyserver.com/docs/caddyfile/directives/tls)、[Caddy 访问认证](https://caddyserver.com/docs/caddyfile/directives/basic_auth)。
