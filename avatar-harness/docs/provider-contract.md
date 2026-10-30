# Go Harness provider 与数字人协议

## 接入方式

`kind: openai` 只用于原生兼容 LLM；`kind: fastapi` 用于标准 HTTP 适配器；`kind: internal` 用于已注册并编译的 Go 函数。FastAPI 是服务提供商侧的一种实现方式，协议本身不限制服务端语言。模型可以仍用 Python、PyTorch 和 CUDA。

`kind: bailian` 用于 Go 原生百炼语音 WebSocket 适配器。ASR 和旧 Qwen3-TTS-Realtime 使用 `wss://…/api-ws/v1/realtime`，适配器添加 `model` 查询参数；Qwen-Audio-TTS 使用 `wss://…/api-ws/v1/inference`，模型写入 `run-task.payload.model`，不添加查询参数。使用 `./start configure bailian` 可将 LLM、ASR、TTS 统一设置为北京地域，共用服务端 `DASHSCOPE_API_KEY`。LLM 继续使用 `kind: openai`。百炼预设不覆盖 Avatar、角色资产或工具配置。

所有外部密钥由 Go 从 `api_key_env` 读取并在服务端发送 Bearer header，不下发浏览器。`timeout_ms` 为每次调用上限；上下文取消会终止 HTTP 等待。外部模型还需要自行响应断连 / 取消，HTTP 取消不等于 GPU 算子立即停算。

## 实时麦克风 WebSocket

`GET /api/sessions/{id}/voice` 升级为同源 WebSocket，当前由百炼 ASR 提供持续识别。连接到上游并完成配置后返回 `{"type":"ready","sample_rate":16000}`。

客户端先发送 `{"type":"start","input_id":"唯一ID"}`，随后发送二进制 PCM S16LE、16 kHz、单声道帧（每帧至多 6400 字节）。Go 同时向上游写音频和读转录，返回 `{"type":"transcript","input_id":"…","text":"临时文字","final":false}`。

400ms 静音后客户端发送 `{"type":"commit","input_id":"…","end_age_ms":400}`。`end_age_ms` 从末个有声帧计，不从识别完成计。最终识别只触发匹配输入的一轮回复；`turn.started` 和模型调用上下文中均携带 `input_id`。回复音频继续通过会话 SSE 的 `pipeline.audio` 到达浏览器。

`{"type":"cancel","request_id":"停止请求ID"}` 会关闭当前识别、取消该轮并预热下次连接，返回 `{"type":"cancelled","request_id":"…"}`。再次 `start` 同样取消旧轮。连接关闭停止识别与当前回复。过期输入结果不能启动新回复，客户端也拒绝旧轮音频。

未提供连续通道的外部 provider 保留 HTTP `/audio` 整句输入，不能把 NDJSON 下行等同于双向音频输入。

## `/invoke` 请求结构

```json
{
  "capability": "tts",
  "input": {"text": "你好", "voice": "voice-id"},
  "context": {"session_id": "s", "turn_id": "t", "request_id": "r"}
}
```

```json
{"status":"ok","output":{},"error":""}
```

状态为 `ok/deferred/error/timeout/cancelled`。工具执行前按其 JSON Schema 校验，批次还受 `tool_batch_ms` 限制。状态不是 `ok` 时不要附加伪造模型输出。

内部 Go 函数采用相同的数据结构：

```go
bus.Register("my_model", func(ctx context.Context, request provider.Request, emit provider.Emit) (provider.Result, error) {
    // 在此调用实际实现；循环必须响应 ctx.Done()。
    return provider.OK(actualOutput), nil
})
```

`actualOutput` 由真实模型或确定性工具生成，示例代码没有提供假推理。配置 `factory: my_model` 后重新编译。Python 的 `module:function` 使用外部 FastAPI 包装。

## `/stream`

可选 NDJSON 或单行 `data:` SSE。每行一个完整 JSON 对象；SSE 可用 `[DONE]` 结束。

LLM：

```json
{"type":"text","text":"回复片段"}
{"type":"tool.delta","name":"工具名称"}
{"type":"done","tool_calls":[{"id":"call-1","type":"function","function":{"name":"get_current_time","arguments":"{}"}}]}
```

工具只在 `done` 含完整参数后执行。无工具时也必须返回 `done` 和空调用列表。`tool.delta` 是进度，不触发执行。原生 OpenAI 兼容流由 Go 自动聚合参数分片。

TTS：

```json
{"type":"audio","output":{"audio_base64":"实际音频","format":"pcm_s16le","sample_rate":24000,"channels":1}}
{"type":"done"}
```

音频支持 PCM S16LE 或完整 WAV，不能把任意压缩字节标成 PCM。每个包都要有采样信息；Go 按真实样本数生成 PTS。必经语音合成由流水线调用，不需要 LLM 每句先进行一次工具选择。

Go 语音流水线的 `Bus.NewTTSStream` 为每条合成工作通道管理本轮会话：百炼在 LLM 生成期间异步建连；Qwen-Audio-TTS 收到已提交的情感后发送带 `instruction` 的 `run-task`，每个文本段使用独立 UUID，以 `task-finished` 为完成边界，随后复用连接。旧 Qwen3-TTS 提前配置会话，重复 append／commit，以 `response.done` 为边界。取消或本轮完成后关闭连接。并发通道不共享同一条 WebSocket。FastAPI／内部 Go provider 仍使用原有逐段 `/stream`／`/invoke`，同时收到最新 `expression_intent`；单独调用 `Bus.Stream` 或 `Invoke` 保留一次性调用语义。

## 各能力输入

| 能力 | 输入 | 结果 |
|---|---|---|
| LLM | messages、tools | content、完整 tool_calls；或上述流事件 |
| ASR | audio_base64、format、mime_type、sample_rate、channels | `{"text":"实际识别文本"}` |
| TTS | text、voice、expression_intent.style / intensity / pad，可选 instruction | 实际音频数据 |
| Avatar | operation、character_id；animate 时附 audio、expression_intent | 展示描述或逐帧动作 |
| 工具 | 已注册 schema 对应参数 | 确定性结果或真实模型结果 |

本机摄像头模块默认仅预览，尚未自动接入视觉模型；图像分析可作为独立工具扩展，不会偷偷把摄像头画面注入 LLM。

## Avatar 展示描述

配置 Avatar FastAPI 后，工作台调用 `operation: describe`。服务返回以下之一，可包在 `output.renderer` 中：

```json
{"status":"ok","output":{"renderer":{"kind":"scene","asset_url":"https://你的服务/角色.glb"}}}
```

```json
{"status":"ok","output":{"renderer":{"kind":"iframe","url":"https://你的服务/lam-renderer"}}}
```

```json
{"status":"ok","output":{"renderer":{"kind":"video","url":"https://你的服务/live-video","includes_audio":true}}}
```

`lam` 在浏览器专用高斯渲染器中加载 LAM ZIP。默认本地路径在 Worker 中推理 TTS 音频，无需 Avatar FastAPI（见 [Mac 本地 LAM](lam-local.md)）。配置 `renderer.driver: remote` 后改用 Avatar provider 的 GPU 服务，保留角色原有资产地址。

远程 LAM 的同源入口为 `GET/POST /api/sessions/{id}/driver`。GET 向 provider 请求 `operation: health`，POST 请求 `operation: infer_window`，输入为 34133 个 16 kHz 单声道 `pcm_f32le` 采样的 `audio_base64`。后端返回 64 × 52 的帧优先 `weights`，通道顺序由健康检查的 `names` 给出；浏览器只使用新增音频对应的后缀帧。此模式不创建本地模型 Worker，也跳过普通 `animate` 分包调用。重置中止 HTTP 等待并丢弃过期结果；已经启动的 GPU 算子可能继续完成。部署见 [data2 模型服务](data2-models.md)。

`scene` 在浏览器 Three.js 中加载 GLB/VRM，资源需允许 CORS；本地资产通过 CLI 注册，或浏览器直接读取 File 而不上传。`iframe` 服务需允许嵌入；当前只确认页面加载，完整专用客户端的数据 / 音频通道仍要适配。`video` 使用原生播放器，只接受浏览器支持的传输格式；不能把 `webrtc://` 或 RTMP 地址直接赋给 video.src，WebRTC 使用专用页面 / 信令适配器。

静态资产、动画驱动和最后渲染是三件事。普通网格和 LAM 高斯资产不是同一格式，LAM ZIP 不能由 GLTFLoader 直接加载。

## LAM 的 LLM 结构化表情

LAM 角色的最终回复固定为以下键顺序（需要工具时仍输出原生 `tool_calls`）：

```json
{"emotion":"sad","intensity":0.4,"speech":"<speak/>听起来这件事让你很难受，我们可以慢慢聊。"}
```

运行时流式解析 `speech` 字符串，处理跨包 Unicode、引号、换行和反斜杠转义，保留 `<speak/>` 提交门控。基础七类为 `neutral/happy/sad/angry/fearful/surprised/disgusted`，强度为 0–1；旧 `natural/focused/concerned` 继续兼容。元数据和 JSON 包装不送进 TTS，也不写成 assistant 正文。LAM 回合统一使用 `response_format: {"type":"json_object"}`，包括工具可用的首轮；原生 `tool_calls` 仍按原协议执行。只靠提示词可能产生正文但缺失情感元数据，不能把这种回答视为已执行表情。旧 provider 的纯 `<speak/>` 正文仍兼容。

提交回答时下发 `pipeline.expression`：

提示词仍要求情绪、强度在 speech 之前，以便语音开始前确定控制。合法 JSON 若改变键顺序，正文照常流式输出，后置元数据在解析完成时补发或更新表情；正常前缀不重复发送。模型侧历史保留原结构化回复，页面、会话事件和 TTS 只接收解码后的正文。

```json
{"style":"sad","intensity":0.4,"duration_ms":8000,"source":"llm_structured"}
```

它沿用会话 SSE 的 `turn_id`，不经过 Avatar 推理请求，不等待音频，也不触发额外 LLM 请求。浏览器忽略历史重放、取消轮和过期轮的控制事件；自动表情默认到期恢复中性，若本轮排程音频仍未播完，则延长至播放末尾后 250ms。手动选择覆盖自动脸部状态，语音继续采用本轮 LLM 的情感意图。

TTS 共享提交后的 `style/intensity`，按七类映射成普通话自然语言 `instruction`；强度为零时使用中性语气。指令写入任务参数，不混入播报正文。FastAPI／内部 TTS 也收到同一份 `expression_intent`，能否执行由该 provider 实现决定。

## Avatar 动作数据

Go 将 TTS 的真实最终音频送给 `operation: animate`：

```json
{
  "capability":"avatar",
  "input":{
    "operation":"animate",
    "character_id":"character-01",
    "audio":{"audio_base64":"实际音频","format":"pcm_s16le","sample_rate":24000,"channels":1,"pts_ms":0},
    "expression_intent":{"pad":{"pleasure":0.2,"arousal":0,"dominance":0}}
  },
  "context":{"session_id":"s","turn_id":"t"}
}
```

返回：

```json
{
  "status":"ok",
  "output":{
    "frames":[
      {"offset_ms":0,"blendshapes":{"jawOpen":0.1,"mouthSmileLeft":0.2,"mouthSmileRight":0.2},"head_quaternion":[0,0,0,1]},
      {"offset_ms":40,"blendshapes":{"jawOpen":0.4},"expressions":{"happy":0.2}}
    ]
  }
}
```

上面的数值仅说明协议结构，运行时不会自动产生这些示例帧。

`offset_ms` 相对当前音频包；也可用 `pts_ms` 指定该轮全局时间。blendshape 使用资产对应的命名，优先 ARKit 名称；VRM 另支持 `expressions` 和有限的 ARKit 到 VRM 预设映射。这种映射不会让 VRM 凭空获得完整 52 维面部控制。头部四元数为局部 xyzw。

浏览器本地 LAM 帧可附 `speech_energy`（0–1，相对 RMS）。它只为小幅眉眼重音提供时间信号，不是情感分类结果；缺省为零。神经口型、LLM 情感姿态与程序眨眼分层组合，发音通道不被情感层改写。

浏览器使用 AudioContext 的实际分段排程对齐动作，在音频间隙保持已播放时刻。只有参数确实匹配资产并应用、且获得实际渲染记录，才可以提交音画起播记录。页面载入、日志、首 token、音频排队或静态网格本身不算音画反馈。

当前音频包调用 Avatar `/invoke` 后按序发出；`/stream` 可单独转发，但长音频的渐进式 Avatar 推理还需要真实后端联调。模型首次加载、长包等待和云端网络均未由 Go 重构消除。

## 运行状态与测量

`module.status` 是实际调用产生的运行 / 完成 / 异常状态；配置快照只标注配置齐全，不能等同推理健康。摄像头和麦克风状态来自浏览器，形象“预览就绪”仅说明资源与渲染成功。

从 VAD 末个有声帧计时，计入默认 400ms 端点等待、最终 ASR、LLM、TTS 与网络。客户端只有在首个非静音样本进入运行中的 AudioContext 后才上报 `{"modality":"audio","meaningful":true,"playback_observed":true,"elapsed_ms":…}`；服务端返回 `audio_within_budget`。纯音频不会满足旧的音画 `qualifies/within_budget` 条件。文本请求只记诊断，物理体验与数字人同步仍需真实设备验收。

`pipeline.timing` 提供分段诊断，不计作播放达标：

- `stage: tts.segment`：`segment` 是从 0 开始的文本段号，`characters` 为 Unicode 字符数，`queue_ms` 为该段入队到工作通道开始处理的等待。
- `stage: tts.connection`：`dial_ms`／`configure_ms` 是该连接最初准备的耗时；`wait_ms` 是当前文本段仍需等待连接就绪的时间，`reused` 表示此前是否已在此连接提交过文本。复用段里的初始准备耗时不是新的重复开销，不能逐段相加。
- `stage: tts.first_audio`：`elapsed_ms` 为工作通道开始处理到收到该段首个音频包的时间。并发后段可能先完成，但仍会等前段输出；该数值不是 SSE 到达、浏览器起播或音画同步耗时。

## 桌面延迟统计

日常界面仅显示整体响应时间与音画状态。以下分项、驱动读数和配置面板仅在 Vite 开发模式显示，不出现在默认 Go 用户页面；固定七类情绪演示位于独立的 `/demo/emotions` 页面。

底栏显示本轮首次观测，不用预估值填充；没有数据时显示“—”，新轮或新会话清空，停止后保留已测值并拒绝旧轮补写。

| 字段 | 计时口径 |
|---|---|
| Total Delay | 从最后有声输入帧（文字请求从发送时）到 AudioContext 中首个非静音样本实际播放 |
| Avatar Delay | 首个已解码音频块在浏览器中的重采样、Wav2ARKit 推理与动作映射准备；包含本地等待，不包含音频解码 |
| TTS Delay | `tts.first_audio` 的 segment=0，首段合成工作开始到上游首音频包，不等同有声播放 |
| LLM Delay | `llm.first_speech`，本轮首次 LLM 请求开始到首个已提交的可播报正文，包含结构化前缀及可能的工具往返 |

若模型遗漏 `<speak/>` 标记，工具可用时仍等待完整回答确认没有工具调用，再记录 LLM Delay 并送入 TTS；不会把工具前言当作首个有效回答，也不会用整轮生成耗时补造计时。

语音合成与模型生成可以重叠；Total 还包括 VAD、ASR、分段、排队和网络等，因此不要求三个分项之和等于 Total。音画确认另保留“已同步／待确认”状态，hover 可查实际音画反馈时间。
