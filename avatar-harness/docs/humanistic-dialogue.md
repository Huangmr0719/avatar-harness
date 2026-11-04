# 人本支持的实现与验收

定位仍为通用对话助手，借鉴以人为中心的交流原则。机制的通过率不是临床疗效；75% 有效率的定义继续待确认。按用户后续要求，在线模型审校已移除，语义原则仅由生成 Prompt/方法卡执行，现行链路见 [直接输出协议](support-direct-stream.md)。

## 开发对比开关

“开启表情分析”右侧的“人本主义对话”开关按会话持久保存，刷新后恢复；新建或清空的浏览器会话默认关闭。关闭时使用普通对话，开启时使用人本主义生成 Prompt、方法卡和现有支持状态。已有会话保留当前支持状态。切换会中断当前回答及播放，保留历史、来源记忆、渲染和感知。

显式开关会固定交流方式，模型不再自动调用 `set_support_mode` 或 `start_mindfulness` 改变它；其他工具仍可使用。高级面板里主动选择支持路径或点击正念视频按钮仍有效。开关不依赖摄像头、表情分析或形象渲染是否就绪。

关闭路径仍是现有普通生成；开启路径仍是独立风险检查与生成并行，没有新增模型请求或在线审校。两种模式的系统指令、方法上下文和风险编排不同，因此该开关用于实际模式对比；需要严格控制其他条件的 Prompt 消融时，继续使用 `/demo/prompts`。

开关验收包括真实页面同排位置、鼠标与键盘操作、刷新保持、会话隔离、失败回滚；后端回归覆盖 Prompt 分支、请求预算、阻止模型覆盖、取消晚到输出、版本冲突及重启后的记忆保留。

## 对话机制

| 编号 | 机制与边界 | 实现 | 验收证据 |
| --- | --- | --- | --- |
| H1 | 准确共情：依据用户表达，未确认的原因保持未知，允许矛盾；传感器不能证明心理 | 模式专用 `humanisticDialoguePrompt`、`reflect-need`；`feeling/need/preference` 仅接受当前原话 | `TestHumanisticUnderstandingRequiresCurrentUserEvidence` 验证来源；留出对话语义复核理解是否准确 |
| H2 | 接纳：允许负面体验，不羞辱、强行乐观或赞美施压；不替伤害或未证实指控背书 | 生成使用人本约束；倾听卡明确接受感受与认可行为的区别 | `TestHumanisticPrinciplesAndStateUseOneGenerationWithoutReview` 验证约束送达及两次请求预算；离线对话评估接纳与边界 |
| H3 | 自主：只听、不分析、不提问、撤回与停止影响下一轮；直接问答直接完成 | `respect-direction`、既有意图/提问/停止控制、`support_preference` 原话记忆 | `TestHumanisticQuestionsRespectListeningAndBurden`、`TestHumanisticMovesFollowIntentWithoutCBTStageProgression`；留出对话复核变相追问与建议 |
| H4 | 苏格拉底式澄清：愿意探索且存在实际未知时，用一个开放问题核对含义；不预设隐藏动机、不用反问劝服、不机械反驳认知 | `verify-understanding`、`explore-meaning`；保留现有提问频率/深度；明确探索请求不误判为 answer | 方法选择与问题负担测试；开放性、诱导性与是否重复已知信息通过留出对话逐条语义复核 |
| H5 | 纠正、真诚与连续性：采用新说法，不虚构经历、感受、关系或疗效 | 共享人本约束；`correction`、带来源的 `replaces`；累积 `superseded` 防旧引用复活 | `TestHumanisticCorrectionSuppressesOnlyReplacedPhrase`、`TestHumanisticCorrectionFiltersOverlappingQuotesByOriginalSource`、`TestHumanisticCorrectionRejectsForgedOrFutureReferences`、`TestCorrectionMemoryPersistsAndOwnsReferenceSlices`；留出对话复核真实性 |

本次留出语义复核由助手执行，不是独立人工盲评。单元测试使用本地固定响应，验证程序约束和链路契约，不声称能证明真实模型的共情质量。语义行为不使用关键词/问号数代替判断。允许合理改述，不要求固定措辞，也不要求每轮先共情或先问问题。

## 记忆

仍是同一会话的原话记忆，不引入跨会话画像。新增两类：

- `support_preference`：用户明确表达的交流方式和边界，不推断人格。
- `correction`：保留纠正的新含义与否定范围；可用 `replaces: [{message, quote}]` 引用较早、确实存在的用户原话片段。纠正助手理解而无旧用户原话时，无需替代引用。

替代只屏蔽对应片段；同一原话里其他未变的条件继续可检索。历史文件保留原文。`SessionMemory.superseded` 累积已验证引用，即使之后的 12 条记忆选择省略纠正条目，旧片段也不会重新作为有效记忆注入。检索只输出连续的原文片段，不拼接伪造引语。旧记录缺少新字段时兼容读取。

程序校验来源、时间顺序与引用范围；“是否确实构成替代”仍由既有记忆整理模型判断，不能把引用合法等同于语义正确。临时情绪不转成永久标签。用户纠正尚未进入后台记忆时，仍通过当前消息/近期历史及 Prompt 的最新表达优先规则生效。

## 链路与测量

当前：风险检查与回答生成并行 → 风险通过后直接按句输出。模型审校、有界模型改写/修复均已移除；工具预算与后台记忆整理保留。风险 Prompt、渲染、感知、TTS、取消和旧事件过滤不变。以下留出报告对应此前带审校的历史版本，不代表直接输出链路效果。

留出验证使用修改前/后的独立构建、相同模型参数、角色、用户输入、参考历史、支持参数及感知输入，交替版本先后；两批诊断各计划重复两次，最终新样本每组一次（4 组、每版 12 轮）。比较两个服务的 `current` 列，不把演示页的历史 `legacy` 列当成本次基线。风险在每个服务的一对演示列内共享；跨版本独立执行并检查一致性。生成、审校、方法卡与记忆解释构成共同改动，不将结果归因为单一 Prompt 句子。

逐轮保存候选、最终正文、程序验收、语义复核、修正/修复次数、兜底、模型请求、首段文字与完成延迟。失败与兜底保留在分母中。固定参考历史排除了前一轮回答差异，不能代表自由演化对话；文本首段延迟不是 AudioContext 起播延迟。后台记忆在对照页中禁用，其来源、替代、持久化与非阻塞性由独立测试验证。

本次最终小样本和剩余限制见 [2026-11-04 验证报告](humanistic-validation-20261104.md)。本轮未执行真实摄像头、麦克风或音画播放验收；相关源码、配置与原会话保留，前端回归通过。

原理来源：[BACP 以人为中心咨询](https://www.bacp.co.uk/about-therapy/types-of-therapy/person-centred-counselling)、[Rogers 原文](https://psychclassics.yorku.ca/Rogers/therapy.htm)。方法卡是原创工程转述；AI 的坦诚与一致性是工程转译，不能等同于人的主观真诚。
