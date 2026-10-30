# Avatar Harness

Go + Vue/Three.js 的桌面数字人 Harness，支持终端配置 provider、实时语音识别、流式回复、随时打断、本地 LAM 高斯渲染与语音口型、LLM 七类情感表情和语音控制。

- [应用源码与使用说明](avatar-harness/README.md)
- [Windows 同网段部署：给 Agent 的执行指南](avatar-harness/docs/windows-lan-agent.md)
- [本地 LAM、情感控制及验证记录](avatar-harness/docs/lam-local.md)
- [data2 GPU 模型服务、目录与启停](avatar-harness/docs/data2-models.md)
- [Provider / 外部 FastAPI 接口](avatar-harness/docs/provider-contract.md)
- [第三方来源与许可](avatar-harness/sources.lock.json)

`avatar-harness/` 是应用目录，`assets/` 保存原始角色资产和来源记录。保留这一层结构，以便资产准备脚本和文档中的相对路径正常工作。

## 首次构建

Windows 已安装 WSL 时，请按上面的 WSL2 指南执行；其中也提供原生 PowerShell 备选方案。本节命令用于 macOS / Linux。

需要 Go 1.24+、Node.js 22.12+、Git LFS。模型和角色二进制文件通过 LFS 保存；源码克隆完成后执行：

```sh
git lfs install
git lfs pull
cd avatar-harness
cd web
npm ci
npm test
npm run build
cd ..
./scripts/go test -race ./...
./scripts/build
```

## 使用当前 LAM 与百炼方案

以下初始化命令供全新克隆使用：

```sh
mkdir -p .avatar-harness
cp configs/lam-bailian.example.json .avatar-harness/config.json
./start configure bailian
./start serve --no-open
```

另开终端，从 `avatar-harness/` 运行 `bash scripts/connect-data2-models`，连接已部署的 data2 GPU 服务。百炼向导填写自己的北京地域 API Key；配置后打开 `http://127.0.0.1:8765/`。示例绑定当前 LAM 角色与龙天男声 `longtian_v3.1`，使用 DeepSeek Flash、Qwen-ASR-Realtime、Qwen-Audio-3.1-TTS-Flash。LAM 高斯渲染在浏览器，官方 LAM-A2E 推理在 data2；GPU 服务安装与固定权重来源见 [模型服务说明](avatar-harness/docs/data2-models.md)。

当前形象为第三张实验照片重建的版本，已校平动画的默认仰头，并按骨骼眼高设置镜头。形象 ZIP 随仓库通过 LFS 保存，来源与校验见 [角色记录](assets/characters/lam/current.json)。七类夸张情绪演示位于 `http://127.0.0.1:8765/demo/emotions`，日常首页保留简洁交互。原图与三份建模结果见 [照片实验](experiments/lam-musk-20261030/README.md)。

密钥、个人运行配置、会话历史、缓存、依赖目录和构建产物不纳入版本控制。API Key 只存服务端环境或本地 `secrets.env`。仓库保留模型与资产各自的来源、许可及已知限制。
