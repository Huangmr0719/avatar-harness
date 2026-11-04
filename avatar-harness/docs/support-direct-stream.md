# 直接输出支持回答

按用户要求，在线支持链路已删除模型审校、审校改写和有界模型修复。普通轮不再请求 `support.guard` 或 `support.repair`。

当前链路：风险检查与回答生成并行 → 风险为普通/研究时直接按句输出 → 完成后保存来源合法的状态增量。没有工具的普通支持轮只有两次模型请求：风险一次，生成一次；必要工具仍受原有批次上限约束。语义要求由生成 Prompt 和方法卡承担，不再由第二次模型检查兜底。

## 输出与保留机制

- 风险检查仍有独立 Prompt、当前用户原话依据、4 秒上限。风险未返回前不输出生成正文、不执行工具；紧急、待确认或检查失败仍取消普通生成并采用固定安全回应。
- 保留 `emotion/intensity/speech`、`<speak/>` 提交门与工具前言隔离。支持流式的 provider 在风险通过后，每个完整句子经过本地字数、问句数量等约束校验即可进入页面及 TTS，不等待整段生成。
- 用户指定精确句数时，程序需要完整回答才能核对，所以该类正文仍等生成结束后校验；这不再触发另一次模型调用。格式或传输失败没有模型重试。已输出部分仅保存已发送前缀，不追加未发送尾部。
- 渲染、感知、语音合成/播放、取消及旧轮过滤不变。原话会话记忆和完成后的后台整理保留。

## 同次生成的可选状态增量

支持模式可在 `speech` 后追加 `support`，禁止把状态计划放到正文前：

```json
{"emotion":"neutral","intensity":0.1,"speech":"<speak/>你希望先把经过讲完。","support":{"intent":"listen","facts":[{"key":"need","quote":"先听我说"}]}}
```

`support` 沿用 intent/facts/goal_quote/feedback/feedback_quote/action 的字段。程序校验当前用户原话、允许类别、行动时长、动作是否实际出现在最终正文；独立风险结果覆盖其中任何自报风险。视频不作为生活行动写入，安排不等于实际播放。

该字段可缺省。无效元数据被丢弃，不能改写或阻断一条格式合法的正文，也不会另起模型请求。事实状态每轮只提交一次。原话匹配只证明来源，不证明模型对含义、意愿或反馈的解释正确。

## 对照页

`/demo/prompts` 两侧使用相同的直接输出链路，共用一次风险检查；普通一对回答为一次风险、两次生成。`conditions.review_mode` 为 `none`，页面明确标注“模型审校已移除”，不把零次审校显示为审校通过。历史导出的修正计数字段继续兼容，新运行始终为零。

生成正文与实际输出的差异仅可能来自程序约束失败、传输失败或风险兜底，不再来自模型审校改写。历史带审校版本的实验报告保持原样，不能作为现行链路的延迟结果。

## 验收

- `TestSupportDoesNotReviewOrRewriteOriginalAnswer`：普通轮仅两次请求，原始正文不被改写，没有审校/修复事件。
- `TestSupportSpeechStreamsBeforeGenerationAndMetadataFinish`：生成及状态尾部未完成时，首句已进入 TTS。
- `TestSupportRiskAndComposeOverlapWithoutEarlyRelease`、`TestSupportUrgentRiskCancelsSpeculativeAnswer`、`TestSupportRiskFailureClosesGate`：风险并行及拦截保留。
- `TestSupportExactSentenceCountUsesLocalValidationWithoutRewrite`、`TestSupportLocalConstraintFailureNeverCallsARepairModel`：格式约束由程序执行，失败不增加模型调用。
- `TestSupportSingleGenerationRecordsOnlySourceGroundedAction`、`TestSupportInvalidOptionalMetadataDoesNotBlockValidSpeech`：状态、动作来源与降级行为。
- `TestSupportInterruptedPrefixRemainsInConversation`、`TestSupportGenerationFailurePreservesOnlyStreamedPrefix`：取消/失败只保存实际输出。
- `TestPromptComparisonFreezesActualInputsAndSharesOnlyRisk`、`TestPromptComparisonStreamsGeneratedPrefixWithoutReview`：对照页每列一次生成请求、直接流式输出及隔离。
