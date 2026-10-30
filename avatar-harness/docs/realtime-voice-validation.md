# 实时语音验证 · 2026-10-29

当前运行配置：百炼北京接入，`deepseek-v4.1-flash-1017`（关闭思考）＋`qwen3-asr-flash-realtime`＋`qwen3-tts-flash-realtime`。最新 DeepSeek Flash 按用户指定保留，不用千问代替。

## 已完成

- 持续 PCM 上行与并行临时识别；临时文字在用户说完前到达。
- 400ms 静音后提交最终识别；模型提交正文后边生成边分句合成，TTS 音频增量播放。
- 插话、停止和连接关闭贯穿 ASR/LLM/工具/TTS。旧输入 ID、旧轮次及迟到音频被隔离，本地排队音频立即停止。
- 以末个有声输入帧为起点。音频入队、音频包到达、AudioContext 起播和首个非静音样本分别处理；语音指标与数字人同步分开。
- 启动时检测配置、密钥与可执行文件更新，自动重启旧工作台。此前“密钥已经填写仍提示缺少”的原因是旧进程未加载新凭据，已修复并验证真实服务调用。

## 真实百炼与浏览器检查

输入使用已知文本的云端合成录音，按实时速度经过实际 PCM/VAD/实时客户端、Go、百炼 ASR、LLM、TTS 和浏览器播放器。未打开物理麦克风或摄像头。

| 检查 | 结果 | 证据 |
|---|---:|---|
| DeepSeek 普通问答：浏览器首个有声样本起播 | 1844ms；临时识别在说完前到达 | [原始记录](realtime-browser-deepseek-voiced.json)、[截图](realtime-deepseek-voice-proof.jpg) |
| DeepSeek 普通问答：此前音频缓冲区起播 | 2531ms | [原始记录](realtime-browser-deepseek.json) |
| DeepSeek ＋真实时钟工具：此前音频缓冲区起播 | 6847ms，超过目标 | [原始记录](realtime-browser-deepseek-tool.json) |
| DeepSeek 生成途中停止 | 后台确认取消 0.6ms；收到首包后停止 | [原始记录](realtime-deepseek-interrupt.json) |
| DeepSeek 模型本身两次首段文字 | 647ms、1052ms | [原始记录](realtime-deepseek-model-probe.json) |

1844ms 一轮的分解：端点等待 429ms、提交后 ASR 235ms、最终转录至首段文字 689ms、首句后 TTS 首包 360ms；音频包在 1713ms 到达，首个非静音样本在 1844ms 进入运行中的音频时钟。

工具一轮的主要等待是 ASR 之后两次 LLM 请求：约 5121ms，TTS 首包另外约 478ms。当前功能链路已经可运行，**不能宣称所有请求稳定满足三秒**。这是少量样本，不是 P95 或生产 SLA。语音起播检查不等于物理扬声器可听性、真实麦克风噪声表现或数字人音画同步验收。

切换 DeepSeek 前的千问检查也保留：浏览器 3960ms 的 [基线](realtime-browser-baseline.json)、5379ms 的 [分段记录](realtime-browser-profile-before.json)，以及 [候选模型小样本比较](realtime-model-candidates.json)。这些记录不能用来代替当前 DeepSeek 的结果。

## 自动检查

- `./scripts/go test -race ./...` 通过：持续识别、取消读写、迟到转录、首句提前合成、工具前言隔离、起播指标区分、关闭后的迟到输入、密钥更新检测，以及原有协议、会话和存储检查。
- `npm test` 13 项通过；Vue 类型检查与 Vite 构建通过。
- 本地协议测试使用明确的脚本化结果，不与上面的真实云端检查混用。

检查工具见 [百炼说明](bailian.md)。凭据只在本机服务端读取，检查记录不含密钥。工作台默认不上传摄像头画面。

## 分段流水线优化后的追加检查

2026-10-29：保留 DeepSeek＋Qwen ASR／TTS，以独立临时配置启动新二进制，回放“请用四句简短的话解释光合作用”的云端合成录音。未修改已有配置、历史或常用服务，未访问物理麦克风／摄像头；检查后临时服务已停止。

- 最终 ASR 662.2ms、首文本 1217.1ms、首音频包 1668.9ms，均从末个有声输入帧计。
- 回复分成 5 段，共收到 59 个音频包；2 条 TTS 通道完成初始建连配置，后 3 段复用已有连接，文本就绪后的连接等待均小于 0.01ms。
- 服务端首音频事件比 `assistant.message` 提交完整正文早 233ms；后段曾先完成合成，但发出顺序与 PCM PTS 保持一致。
- 没有 `pipeline.error` 或 `turn.error`。原始传输和连接记录见 [realtime-pipeline-voice-probe.json](realtime-pipeline-voice-probe.json)。

这是单次**首包传输**检查，不是非静音 AudioContext 起播，不能与上面的 1844ms 浏览器结果直接比较或据此声称端到端改善幅度。分段机制的可重复前后对照使用独立本地夹具，见 [优化记录](realtime-pipeline-optimization.md)。
