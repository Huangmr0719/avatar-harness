# 百炼接入与语音流水线

本工作区的 LLM、ASR、TTS 使用百炼北京地域；Go 直接连接厂商协议。数字人资产和渲染仍使用原有 Three.js/VRM 或外部 Avatar 服务。

## 运行

```bash
./start configure bailian
./start stop
./start
```

向导可配置业务空间 ID、三个模型 ID、默认音色和一份隐藏输入的 API Key。默认分别为 `deepseek-v4.1-flash-1017`、`qwen3-asr-flash-realtime`、`qwen-audio-3.1-tts-flash`，默认音色 `longanhuan_v3.1`。DeepSeek 使用 `enable_thinking: false`；角色单独配置的 `voice` 优先于默认音色。

2026-10-29 核对百炼官方发布列表后，按用户要求选用 2026-10-17 发布的最新 DeepSeek Flash 快照，未改回千问。修改配置、密钥或可执行文件后再次 `./start`，会自动停止旧进程并加载更新，避免旧进程继续显示缺少密钥。

密钥使用 `DASHSCOPE_API_KEY`，保存在服务端环境变量或 `.avatar-harness/secrets.env`，不返回浏览器。重新打开百炼向导会保留已配置的百炼模型与默认音色。密钥须与北京地域、业务空间及模型权限匹配。业务空间专属域名优先；未提供空间 ID 时使用仍受支持的 `dashscope.aliyuncs.com`。

只保存无密钥预设：`./start configure bailian --defaults`。该命令替换三个语音对话 provider，保留角色、Avatar provider、工具和渲染配置。通用 `init --defaults` 仍保持各服务待配置。

## 已实现的链路

1. 浏览器 AudioWorklet 以约 32ms 帧采集音频，经连续重采样转为 16 kHz 单声道 PCM。默认 80ms 持续有声确认插话，保留约 300ms 预录，400ms 静音后提交。阈值可通过 `limits.voice_onset_ms` / `voice_silence_ms` 配置。
2. 浏览器与 Go 使用持续 WebSocket，音频不等说完才发送。Go 的 `LiveASR` 同时上行音频、下行临时文字，前端展示可修订的 `text + stash`。每句使用独立的百炼会话；空闲时预热下一句连接，Manual commit 避免第二次静音等待。旧 `/audio` WAV 入口继续供其他 provider 使用。
3. DeepSeek 使用 OpenAI 兼容 SSE。模型需要工具时直接调用，参数聚合并通过原有 Schema 校验后才执行；模型以 `<speak/>` 提交可播报正文后，边生成边送入分句 TTS。该标记在服务端被移除，未输出标记的 provider 保留结束后合成路径；提交正文后又请求工具会停止该轮，不执行冲突调用。工具前言不会作为提前回应播出。
4. Qwen-Audio-TTS 使用 `/api-ws/v1/inference` 原生 WebSocket 和 24 kHz PCM。每轮的合成工作通道在 LLM 生成时并行预连接；收到本轮情感后发送带 `instruction` 的 `run-task`，等待 `task-started`，发送 `continue-task` 和 `finish-task` 及时冲刷短文本。二进制音频即到即转发，`task-finished` 后以新 UUID 复用同一连接处理下一段。旧 Qwen3-TTS 的 `/realtime` 与 append／commit 协议继续兼容，但不支持这套情感指令。每条并发通道独占连接和响应状态，完成或取消后关闭，不跨轮次共享。后续段按文本顺序进入浏览器的同一 AudioContext 样本时钟。输入网络积压会明确停止该次录音，不悄悄丢帧。
5. 再次开口或点击停止，先清除本地播放、排队音频和动作，再取消旧任务。输入 ID、轮次 ID 及连接代次隔离迟到识别和音频；停止有后台确认。ASR、LLM、工具和 TTS 共用取消上下文，取消会关闭上游语音连接。
6. 从末个有声输入帧计时。音频入队、收到音频包和有效起播分开：播放器检测首个非静音样本，在 AudioContext 真正运行到该时刻后报告；语音三秒结果为 `audio_within_budget`，音画同步仍单独检查。厂商错误、空结果、截断与提前断连不作为成功。

当前 VAD 仍是带迟滞和持续时间门限的能量检测，未导入 Silero。真实浏览器检查使用合成录音，未代用户打开物理麦克风或摄像头；回声、环境噪声、实际语音体验与数字人同步需在设备上联调。云端模型首段等待会波动，特别是工具调用包含两次 LLM 请求，当前不能保证所有请求稳定三秒。

## 情感语气

LLM 同一条 JSON 回复给出 `emotion`、`intensity`、`speech`，流式解析后按中性、快乐、悲伤、愤怒、恐惧、惊讶、厌恶七类映射 TTS `instruction`，同时发送脸部表情事件。低强度轻微表达，零强度使用中性；愤怒、恐惧、厌恶仍保持自然克制。整个过程不增加一次 LLM 或情感模型请求。

每段保留同一情感快照，指令写入任务参数，只有 `speech` 正文进入合成输入。原有 `focused/concerned/natural` 继续兼容。手动菜单用于脸部预览；自动模式按 LLM 情感驱动。API 接受指令与七类听感可辨是不同验证目标，听感仍需逐类试听。

## 分段流水线参数

- `limits.tts_first_chunk_ms`：首段默认 180ms；`limits.tts_chunk_ms`：后续段默认 400ms。旧配置不含这些字段也会加载默认值，无需修改现有配置文件。
- 预算从该段开始缓冲文字计，不被每个新 token 重新推迟。遇到句末标点可以立即提交；首两段还可在软标点提前提交。未遇标点时，首段、第二段和后续段分别采用约 24／48／96 字符的长度目标。
- 到达时间预算后，在可切分边界提交已有内容；首段至少约 8 个中文字或 4 个英文词，后续至少约 12 个中文字或 6 个英文词。未完成的英文词、数字或过短片段继续等待新文字／最终结束，不能把软预算当成绝对提交时限。
- `limits.tts_concurrency` 仍默认 2。各通道并发合成，结果按段号和实际 PCM 样本数保序；待合成队列容量等于通道数，每段的音频包队列容量为 4。慢消费者会反压上游，取消仍能打断这些等待。
- `<speak/>` 正文提交门槛未改变。分段计时器只处理已获准合成的正文，不会释放工具前言。最终 ASR 确认、工具校验、Avatar 同步策略和模型选择均保留。

实现、计时字段及验证数据见 [实时分段流水线优化](realtime-pipeline-optimization.md)。

## AIRI 示例参考

已读取 [webai-example-realtime-voice-chat](https://github.com/proj-airi/webai-example-realtime-voice-chat)，参考版本 `b7a8fa42efa246de9e771c6473afd70b5eb52ad7`。来源与版本记录在 [sources.lock.json](../sources.lock.json)，MIT 许可保存在 [integrations/licenses/](../integrations/licenses/webai-example-realtime-voice-chat-LICENSE)。

主要入口为 [`apps/vad-asr-chat-tts/src/pages/index.vue`](https://github.com/proj-airi/webai-example-realtime-voice-chat/blob/b7a8fa42efa246de9e771c6473afd70b5eb52ad7/apps/vad-asr-chat-tts/src/pages/index.vue)。其思路与当前 Harness 对应关系：

| 示例实现 | 当前 Harness |
|---|---|
| input-speech → transcription → token → sentence-ready → tts-output 事件链 | Go provider / 会话事件与 SSE，保持模块独立 |
| 流文本通过 `<break/>` 切分句子 | 现有标点分句器，无需模型生成特殊标记 |
| 多句 TTS 异步完成后按 index 播放 | 有界并发、有序通道与音频样本 PTS |
| 浏览器 VAD 使用 16 kHz Silero，400ms 静音分句 | 当前使用能量 VAD、80ms 起声确认与 400ms 静音；未导入 Silero |
| ASR 上传 WAV，TTS 每句返回音频后 decode | 当前百炼通道双向持续传输，TTS 增量转发；兼容保留 WAV 入口 |

示例的主语音流程未包含工具循环，也未实现贯穿 ASR/LLM/TTS 的逐轮取消。当前复用其模块划分思路，保持 Go 控制面和已实现的取消、工具校验与数字人同步协议；未将整个前端工作流框架引入运行时，未复制示例实现代码。

## 验证

- `./scripts/go test -race ./...`：通过。包括说完前临时转录、第一句在 LLM 结束前进入音频通道、新语音取消 LLM、停止识别后拒绝迟到最终结果、工具前言隔离、实际起播与入队区分等。
- `npm test`：13 项浏览器音频、连续重采样、VAD、权限、迟到事件与调度测试通过。
- `npm run build`：Vue 类型检查与构建通过。
- 已使用保存的密钥调用真实百炼 ASR/LLM/TTS，并通过独立浏览器合成录音检查实际 AudioContext 起播。成功和失败记录均保留，见 [实时语音验证](realtime-voice-validation.md)。模型延迟与控制面测试不混用。
- 分段优化后又以独立临时配置完成一次真实百炼合成录音回放：5 段、59 个音频包、2 条合成连接，后 3 段复用连接；首包 1668.9ms，首包发出时模型正文尚未生成完。此轮只测传输，未重新验收浏览器起播，见 [原始记录](realtime-pipeline-voice-probe.json)。

可复核命令（会调用真实云端模型）：

```bash
node --experimental-strip-types scripts/voice-probe.mjs --output docs/voice-check.json
node --experimental-strip-types scripts/voice-probe.mjs --interrupt --output docs/voice-stop.json
node scripts/voice-browser-check.mjs
```

浏览器检查打开 `http://127.0.0.1:8770/`，点击运行，使用实际 PCM/VAD/实时客户端与播放器。检查页是独立开发工具，不进入生产工作台；检查完停止该临时服务即可。

## 官方协议依据

- [OpenAI 兼容 Chat 与北京域名](https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions)
- [文本模型与工具调用支持](https://help.aliyun.com/zh/model-studio/text-generation-model)
- [DeepSeek API](https://help.aliyun.com/zh/model-studio/deepseek-api)、[最新模型发布记录](https://help.aliyun.com/en/model-studio/newly-released-models)
- [WebSocket 接入与鉴权](https://help.aliyun.com/zh/model-studio/realtime-websocket-overview)
- [ASR 客户端事件](https://help.aliyun.com/zh/model-studio/qwen-asr-realtime-client-events)、[ASR 服务端事件](https://help.aliyun.com/zh/model-studio/qwen-asr-realtime-server-events)、[ASR 域名迁移说明](https://help.aliyun.com/zh/model-studio/qwen-asr-realtime-python-sdk)
- [Qwen-Audio-TTS 客户端事件](https://help.aliyun.com/en/model-studio/qwen-audio-tts-client-events)、[服务端事件](https://help.aliyun.com/en/model-studio/qwen-audio-tts-server-events)、[情感指令与标签](https://help.aliyun.com/zh/model-studio/realtime-tts-user-guide)
- [旧 TTS 客户端事件](https://help.aliyun.com/zh/model-studio/qwen-tts-realtime-client-events)、[TTS 服务端事件](https://help.aliyun.com/zh/model-studio/qwen-tts-realtime-server-events/)、[音色列表](https://help.aliyun.com/zh/model-studio/qwen-tts-voice-list)
- [万相数字人视频](https://help.aliyun.com/zh/model-studio/wan-s2v-api)：异步任务，当前不用于实时前台。
