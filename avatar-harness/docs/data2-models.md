# data2 数字人模型服务

Windows 客户端使用 [Windows Agent 部署指南](windows-lan-agent.md)，同样通过 SSH 隧道访问以下现有服务。

Mac 保留 Go harness、麦克风/摄像头采集和浏览器界面。data2 运行 GPU 驱动与离线形象制作。所有新增服务器文件位于 `/data2/huangmr/code/avatar-harness/`；现有 `/data2/huangmr/envs/au_gen` 作为只读 CUDA 依赖来源，未升级或降级。

| 服务 | 模型 | 默认 GPU | data2 本机端口 |
|---|---|---|---|
| `lam-driver` | 官方 LAM-A2E streaming，完整 PyTorch 权重 | 0 | 18766 |
| `lam-creator` | 官方 LAM-20K，单图头部重建和对话 ZIP 导出 | 1 | 18767 |
| `lhm-creator` | 官方 LHMPP-700M-SMPLX-FREE，多视图人体重建 | 2 | 18768 |

LHM++ 选择依据是已公开权重、多视图支持及标准 Gaussian PLY 导出；没有把未公开 PixelShuffle 权重或未实测的全局最优当作部署结果。LHM 输出不能直接当作 LAM ZIP 使用，人体渲染/驱动适配需单独实现。

## Mac 连接

```sh
bash scripts/connect-data2-models
```

保持终端连接后：

- GPU 驱动健康检查：`http://127.0.0.1:18766/health`
- LAM 形象制作：`http://127.0.0.1:18767/`
- LHM++ 形象制作：`http://127.0.0.1:18768/`

这三个服务只监听 data2 的回环地址。此部署提供 Mac 通过 SSH 调用的路径，没有开放公网端口。

在 LAM 页面上传照片、选择示例动作，保持导出 ZIP 选项开启后点击 Generate。页面直接提供下载，持久结果保存在 `outputs/lam/avatar-*.zip`，同名 PNG/MP4 用于预览。每次生成使用独立名字；导出包自动保留现有 harness 使用的七段动画顺序。制作过程需要离线重建和 Blender 导出，不占实时回复的三秒预算。

## harness provider

在 Mac 配置中设置：

```json
"avatar": {
  "kind": "fastapi",
  "base_url": "http://127.0.0.1:18766",
  "streaming": false,
  "timeout_ms": 2000
}
```

LAM 角色的 renderer 保留原资产地址，并加上 `"driver":"remote"`。浏览器通过同源 Go 路由 `/api/sessions/{id}/driver` 获取模型状态和推理，不直接连接外部地址、不携带 provider 密钥。选择远程模式后不创建 WASM 模型 Worker；中断会取消 HTTP 请求并丢弃旧轮动作，不回退加载本地模型。Go 的普通 Avatar 分包路径跳过这一模式，避免同一音频重复推理。

输入为 16 kHz 单声道浮点音频，使用官方 64 帧滚动上下文（34133 采样）；仅把新增音频的后缀动作送入音画时间线。LLM 情感、程序眨眼和语音重音仍由原混合器组合。当前高斯渲染继续在 Mac 浏览器；迁移推理不等于已迁移渲染。

## data2 启停

```sh
ssh data2 'bash /data2/huangmr/code/avatar-harness/services/modelctl status lam-driver'
ssh data2 'bash /data2/huangmr/code/avatar-harness/services/modelctl start lam-creator'
ssh data2 'bash /data2/huangmr/code/avatar-harness/services/modelctl stop lam-creator'
```

会话使用项目独立的 `runtime/tmp/tmux.sock`，日志在 `runtime/logs/`。`modelctl` 只管理本项目三个明确命名的会话。

## 依赖与来源

- 实时驱动直接使用现成 `au_gen`。LAM/LHM 的专用依赖位于项目 `envs/`，通过只读路径复用同一套 PyTorch 2.2.2 / CUDA 11.8。没有重复下载或修改原 PyTorch。
- 共用 CUDA 扩展在 `envs/cuda` 编译；LHM 对新版 SDPA 上下文和 uint32 网格索引做了等价兼容转换，模型参数未变。
- LAM 学习参数完整加载；FLAME 的 `J_regressor_up` 缓冲由随附人脸模型构造。LHM 权重加载必须无缺失/尺寸不匹配。
- LAM/LAM-A2E 权重固定 Hugging Face revision 并校验 SHA256；LHM++、先验与动作资产从魔搭逐文件固定 revision、校验 SHA256，清单在各模型目录的 `download-manifest.json`。
- 形象导出的 Blender 4 使用 `tools/glibc-2.35` 私有兼容库；系统 glibc 未更改。它只在导出骨骼/形变 GLB 时运行，不参与实时音频推理。
- 代码、模型权重、FLAME/SMPL-X 资产许可分别保留在上游目录和模型清单中；LAM/LHM 权重的非商业限制不能用代码的 Apache 许可替代。

健康检查、真实模型推理、图片重建、ZIP/PLY 导出和三秒语音验收是不同证据。已有本地语音观测不能直接用于远程链路的 P95 承诺。

## 已执行检查（2026-10-30）

- 三个服务的健康/界面配置均已从 Mac 通过 SSH 转发访问。Go 临时隔离实例也实际完成 GPU 推理代理，随后停止；原 Mac 8765 主服务保持关闭。
- LAM-A2E 实际输出 64 × 52 个有效系数；已记录单窗口 GPU 计算 17.8–34.9 ms，早期三次 Mac API 往返 86–290 ms。输入是合成音频，只验证运算与传输，不证明情感质量或持续负载延迟。
- LAM 使用官方 james.png 完整重建、生成 435 帧预览并成功下载约 4.06 MB ZIP。最终含导出的调用耗时 121.5 秒；包内 51 个形变通道包含全部五个眉部控制，动画为待机优先的七段。记录为 `runtime/state/lam-generation-check.json`，示例为 `outputs/lam/avatar-tmpo76jzima.{zip,png,mp4}`。
- LHM++ 使用官方四视图示例完成实际 CUDA 重建与 30 帧视频，完整权重全部匹配；导出 `outputs/lhmpp-validation/avatar-tpose.ply`，160000 个 Gaussian、17 个属性，数值均有限。视频在同目录 `output.mp4`。已查看两种模型的实际视频帧，未做身份保真/表情自然度的系统评测。
- 前端 43 项测试、Vue 类型检查/构建、Go 全量 race 测试、vet 和二进制构建通过。远程驱动测试覆盖不加载本地 Worker、滚动窗口时间戳和取消后丢弃旧结果。

此次未重跑真实麦克风 → 百炼 → GPU 驱动 → 浏览器实际播放的三秒验收，也未把 LHM 人体接入当前 LAM 渲染器。
