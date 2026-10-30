# Avatar Harness

Go + Vue/Three.js 的桌面数字人 Harness，支持终端配置 provider、实时语音识别、流式回复、随时打断、本地 LAM 高斯渲染与语音口型、LLM 七类情感表情和语音控制。

- [应用源码与使用说明](avatar-harness/README.md)
- [本地 LAM、情感控制及验证记录](avatar-harness/docs/lam-local.md)
- [data2 部署、目录与启停](avatar-harness/docs/data2-deployment.md)
- [Provider / 外部 FastAPI 接口](avatar-harness/docs/provider-contract.md)
- [第三方来源与许可](avatar-harness/sources.lock.json)

`avatar-harness/` 是应用目录，`assets/` 保存原始角色资产和来源记录。保留这一层结构，以便资产准备脚本和文档中的相对路径正常工作。

## 首次构建

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

百炼向导填写自己的北京地域 API Key；配置后打开 `http://127.0.0.1:8765/`。示例绑定当前 LAM 角色与龙天男声 `longtian_v3.1`。默认模型为 DeepSeek Flash、Qwen-ASR-Realtime、Qwen-Audio-3.1-TTS-Flash。本地渲染和语音口型使用浏览器 WASM，无需 GPU 服务；云端模型需账号权限。

密钥、个人运行配置、会话历史、缓存、依赖目录和构建产物不纳入版本控制。API Key 只存服务端环境或本地 `secrets.env`。仓库保留模型与资产各自的来源、许可及已知限制。
