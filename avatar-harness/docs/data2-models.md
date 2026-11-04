# data2 数字人模型服务

当前推荐将工作台放在 data2，经一个 SSH 隧道访问，见 [data2 内网网页部署](data2-web-lan.md)。右上角“制作形象”打开 LAM 网页，生成成功后自动加入该工作台角色列表。以下 Mac 独立运行后端的方式继续保留；建模服务的自动保存回调应指向实际使用的工作台。

Windows 客户端使用 [Windows Agent 部署指南](windows-lan-agent.md)，同样通过 SSH 隧道访问以下现有服务。

Mac 保留 Go harness、麦克风/摄像头采集和浏览器界面。data2 运行 GPU 驱动与离线形象制作。所有新增服务器文件位于 `/data2/huangmr/code/avatar-harness/`；现有 `/data2/huangmr/envs/au_gen` 作为只读 CUDA 依赖来源，未升级或降级。

| 服务 | 模型 | 默认 GPU | data2 本机端口 |
|---|---|---|---|
| `lam-driver` | 官方 LAM-A2E streaming，完整 PyTorch 权重 | 3 | 18766 |
| `lam-creator` | 官方 LAM-20K，单图头部重建和对话 ZIP 导出 | 3 | 18767 |
| `lhm-creator` | 官方 LHMPP-700M-SMPLX-FREE，多视图人体重建 | 3 | 18768 |
| `libreface` | LibreFace 2.0 AU + EmotiEffNet 表情分析 | 3 | 18769 |

四个模型服务默认共用物理 GPU 3，启动脚本中的 `AVATAR_GPU` 仍可显式覆盖。限制可见显卡后，程序内部的 `cuda:0` 对应物理 GPU 3。端口与工作台配置无需更改；修改分配后需重启相应模型服务。建模和实时驱动共用算力，重建时的响应延迟需另行测量。

LHM++ 选择依据是已公开权重、多视图支持及标准 Gaussian PLY 导出；没有把未公开 PixelShuffle 权重或未实测的全局最优当作部署结果。LHM 输出不能直接当作 LAM ZIP 使用，人体渲染/驱动适配需单独实现。

## Mac 连接

```sh
bash scripts/connect-data2-models
```

保持终端连接后：

- GPU 驱动健康检查：`http://127.0.0.1:18766/health`
- LAM 形象制作：工作台右上角“制作形象”（`/create-avatar/`，由 Go 代理 18767）
- LHM++ 形象制作：`http://127.0.0.1:18768/`

这三个服务只监听 data2 的回环地址。此部署提供 Mac 通过 SSH 调用的路径，没有开放公网端口。

### 连接中断后的恢复

若本机 `18766/health` 连接被拒绝，而 data2 本机健康检查正常，重新建立上述 SSH 隧道。页面此时标注“口型离线”，仍可打开麦克风、识别和播放语音；离线动作不计为音画同步。隧道恢复后点击人物区域的“重连口型”，无需刷新或新建会话。首次加载失败也可用同一入口重试。

在 LAM 页面上传照片后可直接点击 Generate，预览动作已有默认选择，也可改选其他示例动作。缺省动作在后端同样自动补全；导出 ZIP 始终开启。未检测到可用人脸或拟合失败时，页面显示对应的照片处理提示。页面直接提供下载，持久结果保存在 `outputs/lam/avatar-*.zip`，同名 PNG/MP4 用于预览。每次生成使用独立名字；导出包自动保留现有 harness 使用的七段动画顺序。制作过程需要离线重建和 Blender 导出，不占实时回复的三秒预算。

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

会话使用项目独立的 `runtime/tmp/tmux.sock`，日志在 `runtime/logs/`。`modelctl` 只管理本项目四个明确命名的会话。

## 依赖与来源

- 实时驱动与 LAM 建模使用 `envs/lam`，LHM 使用 `envs/lhm`；两个专用环境固定 Transformers 4.41.2 / Tokenizers 0.19.1，通过只读路径复用 `au_gen` 中的 PyTorch 2.2.2 / CUDA 11.8。共享环境的 Transformers 升级不会替换这些本地版本。没有重复下载或修改原 PyTorch。
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

## LibreFace 摄像头分析

新增独立服务 `libreface`，GPU3、回环 18769；复用 `/data2/huangmr/code/LibreFace`。本机需同时转发 18769，在页面显式开启“表情分析”后才上传摄像头帧。具体配置、AU 标签、模型分数语义及停止规则见[摄像头感知](camera-perception.md)。
