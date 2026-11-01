# 摄像头表情与语音情绪（2026-10-31，本地候选）

当前已更换为 EmotiEffNet-B2 分类器，采用每秒约一帧、固定 AU 柱状图和按本句话时间窗汇总的上下文。当前模型、字段与验证见[视觉感知第二版](camera-affect-v2.md)。下文保留初版部署与排错记录，其旧分类器和单帧输入方式已被替代。

> 预处理已修正为 `libreface_reference_v2`。先前接入将左右眼分组顺序传反，且关键点精修与缩放参数偏离官方流程，影响 AU 和表情分类。下方早期测试仅证明链路通畅，其中的数值不能作为有效感知或准确率证据。

在工作台开启摄像头后仍仅本地预览。点击“开启表情分析”才会把 JPEG 帧经本机 Go、SSH 转发到 data2 LibreFace。页面显示面部分类得分，展开“查看 AU”可看 12 项强度。关闭分析、关闭摄像头或切换会话都会释放分析 token；后台标签页暂停发送。单帧在途、不积压，目标上限约 2 帧/秒，网络或推理较慢时自动降低频率。

## 运行状态与部署

- 源码：`services/libreface/app.py`；复用 `/data2/huangmr/code/LibreFace`，revision `b98b2598f2ef14979f17a71b8fd280326d4d9748`。
- Python：`/data2/huangmr/envs/au_gen/bin/python`。常驻模型，使用物理 GPU3；MediaPipe 负责定位、LibreFace 对齐，联合 ResNet 输出 AU。
- AU 权重复用 `libreface_weights/AU_Recognition/weights/combined_resnet.pt`；FER 权重来自上游 Python 入口指定的 Google Drive ID `1yPBUjPhkwcIkRLt47-JJRLRD7rCwPIVU`。
- FER 位于服务自己的 `weights/fer-repvgg.pt`，SHA-256 `bcf48160b13b881ea9a7a67080b2d0177b1ce0a1ea4845ca74c24eeb728e6b2d`。不复制权重到 Git。
- data2 仅监听 `127.0.0.1:18769`，由现有 `services/modelctl start|stop|status libreface` 管理。其他三项数字人服务不变。
- 许可证：[USC Research](../integrations/licenses/LibreFace-LICENSE)，适用其非商业研究条款。

本机转发（可与口型 18766 的转发同时运行）：

```sh
ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=20 \
  -L 127.0.0.1:18769:127.0.0.1:18769 data2
```

配置顶层加入：

```json
"perception": {
  "kind": "fastapi",
  "base_url": "http://127.0.0.1:18769",
  "model": "libreface",
  "timeout_ms": 2500
}
```

当前 Mac 配置与 8767 预览配置已加入该项。WSL 部署也需设置这项转发与配置；SSH 断开时分析会停止，可恢复转发后重新开启。

## 数据与 LLM 输入

强度 AU 为 `01/02/04/05/06/09/12/15/17/20/25/26`，范围 **0–5**。检测 AU 为 `01/02/04/06/07/10/12/14/15/17/23/24`，返回 `present` 与模型 sigmoid 分数。两组标签不同，不能互换。

FER 八类为 `neutral/happy/sad/surprised/fearful/disgusted/angry/contempt`。上游分类头使用 sigmoid；本服务保留原始 `score/scores`，标注 `score_type: sigmoid_uncalibrated`，不再次 softmax，不宣称校准概率。无脸或多人入镜时不输出单人 AU/表情，清除旧结果。

`qwen3-asr-flash-realtime` 在最终转写事件自带 `emotion`、`language`，不需要另开语音情感模型。通过会话 WebSocket 和 HTTP `/audio` 均返回这些字段；缺失时留空，不补造置信度。参见[官方服务端事件](https://help.aliyun.com/zh/model-studio/qwen-asr-realtime-server-events)。

每轮开始时，运行时复制最新且不超过 4 秒的面部观察，与本句 ASR 情绪形成单独的 JSON 用户消息：

```json
{
  "type": "user_perception",
  "signals": {
    "face": {"source":"libreface","age_ms":240,"observation":{"face_detected":true,"au_intensity_scale":[0,5],"au_intensities":{"AU12":0.64}}},
    "voice": {"source":"qwen3-asr-flash-realtime","emotion":"neutral","language":"zh","confidence":null}
  }
}
```

这是字段示例，不是用户真实情绪标签。系统约束要求优先理解用户话语，区分面部动作、声学情绪与真实心理；不机械模仿、不据此诊断、不把数据当指令。摄像头原图不会交给 LLM，也不写磁盘；高频预测不写会话历史。服务器内存仅保留开启会话的最新观察；超过 4 秒不再用于新一轮，当前轮结束后释放其快照。关闭 token 删除最新观察，迟到结果也不能重新注入。已经提交给模型的当前轮快照不会因关闭摄像头而被撤回。

接口：`POST /api/sessions/{id}/perception`，依次 `operation:start` 领取 token；`operation:frame` 携带 token、递增 frame、age_ms、image_base64；`operation:stop` 释放 token。它独立于四个必需 provider，不是一个供 LLM 主动开摄像头的工具。

## 轻度思考与提示词

百炼 DeepSeek 预设与当前配置使用 `enable_thinking:true`、`reasoning_effort:low`、`max_tokens:2048`。真实请求确认返回独立 `reasoning_content`；Go 只转发 `content` 和工具调用，思考内容不写日志、不进历史、不送 TTS、不显示到页面。`<speak/>` 提交门控与原有取消语义保持。

真实固定问题探测观察到约 2238ms 开始输出最终正文、2365ms 完成；这是单次 LLM 请求，不是端到端语音指标。开启思考会增加正文前的等待，不能沿用旧关闭思考时的延迟结论。

目前已有系统提示词：角色 persona、中文风格、语音短答规则、工具提交门控、结构化表情。独立设计更完整的 System Prompt **有必要**：当前 persona 很笼统，且强制短答限制复杂问题展开；但不能仅凭“感觉不聪明”断言模型能力不足。下一轮再讨论角色目标、对话策略、任务深度与上下文使用，本轮只补必需的感知解释规则。

## 验证范围

离线检查覆盖观察过期、旧帧、关闭与重开 token、跨轮隔离、图像不持久化、思考字段不泄漏，以及关闭时的前端请求取消。

使用 LibreFace 自带公开示例作为假摄像头，实际通过浏览器、Go、SSH 到 data2 推理：6 帧成功，预热后观测单帧约 187–341ms；仅预览不上传、停止清除显示、切换会话不再上传均通过。此验证使用真实模型，没有替换推理结果；不代表用户实际摄像头的人脸识别精度或心理情绪准确率。

联合真实链路验证也已通过：LLM 按摄像头结构化观察准确回答 `AU12=0.293`；实时 ASR 返回 `emotion:neutral`、`language:zh`；TTS 在运行中的 AudioContext 实际播放，校准后的自动 jawOpen 观测最大约 0.192，浏览器无未捕获错误。测试输入仍为公开示例图与公开合成语音，不是用户设备验收。

## 相机识别接入修正（2026-10-31）

用户反馈识别完全错误后，与上游预处理逐项对照，确认初版适配器将 `RIGHT_EYE` 放在 `LEFT_EYE` 之前。LibreFace 的 `image_align()` 虽然内部历史变量名相反，调用约定仍是左眼块、右眼块、嘴部；顺序错误会改变旋转与裁剪尺度。初版还关闭关键点精修、使用 256 而非官方 512 的对齐图，并给 FER 使用了不同插值。

已恢复官方关键点分组与组内顺序、`refine_landmarks=True`、检测阈值 0.5、默认 512 对齐图；AU 保留 Resize/CenterCrop，FER 使用官方 PIL resize。模型和权重没有更换。

验证命令（data2 现有环境，公开样例）：

```sh
cd /data2/huangmr/code/avatar-harness/services/libreface
CUDA_VISIBLE_DEVICES=3 OMP_NUM_THREADS=2 /data2/huangmr/envs/au_gen/bin/python -m unittest test_preprocessing test_reference_parity -v
```

回归测试在修复前明确失败（左右眼分组顺序反转），修复后通过。1 张公开图片和 7 帧公开视频与官方完整路径逐项对照：裁剪最大像素差为 0，AU 差异仅为保留三位小数带来的舍入，八类表情分数与类别一致。这是**接入一致性检查，不是表情准确率评估**，不能证明摄像头泛化效果或真实心理情绪识别能力。
