# 视觉感知：轻量分类、AU 图表与本句汇总

2026-10-31，本地候选。

## 当前模型

表情分类已换成作者发布的 **EmotiEffNet-B2 / enet_b2_8** ONNX，作为降低帧率后的试用默认。CPU 推理，两线程；模型 30,779,724 字节（29.4 MiB）。同时保留 15.3 MiB 的 B0 轻量备选。两者均独立于 AU 模型，不再调用旧 LibreFace 八类分类器。

- 来源：[EmotiEffLib](https://github.com/sb-ai-lab/EmotiEffLib)，revision `520a051c64cd191521e5934655314e769a319684`。
- B2 权重 SHA-256：`180a9d4845b59393de4511598a0d1d34b705034691ea32959ce5009db7cf52b7`；B0 是 `fa07e841fd06c7a67ee651ea4e6e4a3a2bb5695f47b37a7da50492526f59c898`。
- 按作者示例使用 MTCNN 人脸矩形；RGB、OpenCV resize（B2 为 260×260，B0 为 224×224）、ImageNet 归一化；八类顺序是 angry / contempt / disgusted / fearful / happy / neutral / sad / surprised。不能沿用旧分类器的通道顺序或 LF 的 FFHQ 对齐图。
- MTCNN 来自 `facenet-pytorch==2.6.0`，仅装在服务自己的 `python-packages/`，未修改原有环境；wheel SHA-256 `ecb82b27beb226d106f2219efe8f829b01b87a8595badd01545679bdb9f19cca`。
- EmotiEffLib 是 Apache-2.0，facenet-pytorch 是 MIT；许可证保存在 `integrations/licenses/`。

完整联调发现，CPU 上的人脸检测与分类偶发超过 2.5 秒请求上限。MTCNN 现复用 AU 服务已有的 CUDA 设备（data2 物理 GPU3）；无 CUDA 时继续使用 CPU。模型、检测阈值、裁剪、AU 和八类映射保持，EmotiEffNet ONNX 分类仍在 CPU 两线程运行。服务启动时先预热 MTCNN 三个阶段，再声明就绪；健康接口通过 `face_detector_device` 报告实际设备。启动预热不属于单帧请求。

同一张 1024×768 公开图片的独立探测中，预热后的 GPU 检测加 CPU 分类为 153–204ms；CPU/GPU 检测框最大坐标差约 0.0364 像素，显示精度下的八类分数一致。这是工程延迟与数值一致性检查，不是分类准确率评估。`test_gpu_face_detection_preserves_public_prediction` 固定了框与分数容差；完整感知检查 6 项通过，另对 1 张图片和 7 个视频帧保留官方输入一致性复核。

AU 已切换为 **LibreFace 2.0 / FG 2026 RepVGG**，固定 12 类、强度范围 0–5。权重来自官方仓库 `AU_Recognition/new_checkpoints_fm_repvgg/DISFA/all/repvgg.pt`，服务器副本 `repvgg_fm2026.pt` 与其 SHA-256 相同：`6b50fa3421de494fa6c4325477716b652878a27242ab37976d492e4c9fa0d8f5`（38,983,600 字节）。加载完整 state_dict 并严格校验，使用官方 Resize/CenterCrop/Normalize 与 `image_inference` 的乘 5、截断到 0–5 规则。

`au_source=libreface_2_0_repvgg_fm2026` 与 `expression_source` 分开标记。旧联合 ResNet 的 AU presence 头已不再调用，响应不再附带旧的 `au_detection`；页面与本句汇总只使用强度回归。

## 对 LF 2026 的核查

已检查 [LibreFace 2.0 项目](https://libreface.github.io/)与当前公开 Python 入口。项目有 2026 更新，但八类表情的 Python 入口仍指向与初版相同的 `repvgg.pt` 和 Google Drive ID。data2 已有的 `repvgg_fm2026.pt` 是 12 输出 AU 强度网络，不是八类表情分类器，不能直接替换当前分类任务。

初次替换只升级了分类器；用户随后明确选择 LF2.0 后，AU 强度已按上一节切换。官方研究 checkpoint 随 2026-06-19 的提交 `b98b2598f2ef14979f17a71b8fd280326d4d9748` 发布，已核对服务器副本。Python 包的旧下载 ID `14qEnWRew2snhdMdOVyqKFJ5rq5VZrfAX` 仍返回 ResNet，不能因为文件名带 `repvgg` 就视为 2026 新权重。

HF 官方 benchmark 列表查询得到 57 项，未发现对应 FER/AffectNet 的统一排行榜；没有据此宣称候选是最优，也没有把不同数据集的作者结果拼成同一准确率排名。

## 页面与采样

仍需先开启摄像头，再明确开启“表情分析”才上传。目标约 1 帧/秒；一帧在途，不积压。柱状图固定 12 类：AU01 / AU02 / AU04 / AU05 / AU06 / AU09 / AU12 / AU15 / AU17 / AU20 / AU25 / AU26。高度固定对应 0–5，平滑过渡，显示当前观测值。无有效观测时显示缺失，不把“未识别”当成强度为零。

八类输出使用 softmax 分数，仍为**未校准模型分数**，不是实际情绪正确概率。页面写“当前候选”。AU 描述面部动作，表情分类描述可见表情；两者不证明真实心理。

## 说完话时选取哪段视觉信息

服务在内存保留最多 128 帧、约两分钟的分析结果，不保存画面。实时语音的时间窗从本句开始输入，到末个有声帧结束；终点沿用 `end_age_ms`，不会推迟到 ASR 完成或模型开始回复。

收到最终转写时，冻结已经返回、且采集时间在该区间内的结果：

- 整体：本句有效帧的平均分类分布、候选、标签一致程度、分布熵、平均 AU 强度。
- 末尾：本句最后两秒的独立汇总，保留表情变化，不用最后一帧代替整句话。
- 质量：总帧数、有效人脸帧数、覆盖率与观察跨度。少于两帧时 `insufficient_samples`，不强行指定情绪。分布分散时 `mixed_or_uncertain`；均值最大分数达到 0.5 且至少两帧才提供 `dominant_label`，这是保守使用规则，不是经过校准的置信阈值。

等待 ASR 或等待数字人回应时的新表情不混入上一句话。已经冻结的快照也不会被迟到结果修改；不额外等待尚未完成的视觉推理，因此会如实报告采样不足。时间依据本机 Go 接收时刻与浏览器帧龄估算，不宣称硬件级音视频同步。

文字输入及无开口时间的兼容入口使用提交前约两秒，标记 `recent_before_submit`，不冒充完整语音区间。

## 插入位置与示例

JSON 观察放在对话历史之后、本句用户转写之前。它不写入对话历史或会话记忆；数字人自身的回应表情仍由 LLM 另行决定。不能把用户表情原样复制成数字人的表情。

以下仅示意字段，数值不是用户实测：

```json
{
  "type": "user_perception",
  "signals": {
    "face": {
      "window": {"kind":"utterance","duration_ms":6000,"age_ms":500},
      "sample_count":6,
      "valid_face_count":6,
      "status":"observed",
      "dominant_label":"happy",
      "au_intensities_mean":{"AU12":2.1},
      "ending":{"dominant_label":"neutral","sample_count":2}
    },
    "voice":{"emotion":"neutral","confidence":null}
  }
}
```

## 部署与检查

服务仍位于 `services/libreface/`，沿用 `modelctl ... libreface`、data2 回环 18769 与本机 SSH 转发；角色、口型服务不受模型替换影响。默认权重为 `weights/enet_b2_8.onnx`，可用 `EMOTION_MODEL=enet_b0_8_best_vgaf` 切回轻量备选。模型文件、隔离依赖不进 Git。

Python 检查覆盖 RGB 归一化、八类映射、无人脸/多人不强判、官方 AU 输入一致性、新分类真实 ONNX 推理。Go 检查覆盖本句时间窗、末尾趋势、短句/分散结果，以及完整 WebSocket→ASR→LLM 路径在等待期间出现新表情时仍选择原句数据。前端另做实际页面的 12 根柱、0–5 尺度和停止清理检查。

同一组作者原始人脸裁剪的 20 次 CPU 分类对照中，B2 中位数 59.62ms、P95 132.55ms，B0 中位数 38.71ms、P95 62.76ms。这里只含分类与 resize/normalize，不包含人脸检测、AU、网络、ASR 或 LLM。

作者原始示例图中的三张人脸，在两种模型上均复核得到快乐、愤怒、恐惧。原图有 EXIF 旋转，按作者 OpenCV 读取方式处理；没有把截图再裁剪后的差异当作模型准确率。这个小样本仅证明输入规格和输出响应，不构成真实摄像头准确率评估，也不足以宣称 B2 比 B0 更准。选择 B2 是本轮低帧率下的试用策略。
