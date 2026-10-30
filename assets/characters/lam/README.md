# LAM 角色资产

## 当前角色（2026-10-31）

当前 `avatar-harness/web/public/models/lam/avatar.zip` 使用第三张实验照片（colorado-2022）在 data2 的官方 LAM-20K 服务中生成的形象，替换此前用户提供的展示包。发布包校平了共用动画的持续抬头偏置；SHA-256：`5da20bf3ebde99f2cf2bbc1934674e1f41204bbb13970f0b7b3eadfc269ba3bb`，4,108,445 字节。

原始生成包保持在实验目录，SHA-256：`324805f6c556a02bae9278b9f7982284390b07149a0d05feb08b66a0568f0ec7`。仅修改 animation.glb 的头部旋转关键帧，各段俯仰中位数由 3.64–5.80° 校到约 0°，保留点头/转头变化；其余 ZIP 条目内容逐字节不变。可从 `avatar-harness/` 运行 `node web/scripts/lam-pose.mjs check web/public/models/lam/avatar.zip` 复核；`level 输入.zip 输出.zip` 用于校平其他同骨骼形象。

- 原照片：[Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Elon_Musk_Colorado_2022_(cropped_2).jpg)，U.S. Air Force / Trevor Cokley，美国联邦政府作品，在美国属于公有领域；采用该页面的上游裁剪版本。
- 生成来源与模型记录：`experiments/lam-musk-20261030/`。这是实验合成形象，非本人录制；现有通用男声音色保持原样。
- 51 个命名形变，含全部五个眉部控制；七段动画已按现有 harness 顺序导出。几何、外观和绑定来自该次实际重建，未沿用旧角色的高斯数据。
- 切换记录和旧资产备份路径见 `current.json`。旧的用户原包保持原样。
- LAM 代码、权重及 FLAME 资产遵循各自上游许可，模型来源见 `avatar-harness/sources.lock.json`。

## 原角色存档

用户提供的原包：`chatting_avatar_20261030091048.zip`，4,102,147 字节。

- 来源：https://virutalbuy-public.oss-cn-hangzhou.aliyuncs.com/share/aigc3d/LAM_Chatting_Avatar/chatting_avatar_20261030091048.zip
- SHA-256：`e9e2cd00ff3ae91a8adc5e18b65ed810f5bf01d37aea23b17c5fd0010bf2937b`
- 原包包含 `skin.glb`、`offset.ply`、`animation.glb`、`vertex_order.json`；不包含语音驱动模型或许可说明。资产授权以用户生成／取得资产时的约定为准。
- `skin.glb` 有 51 个命名面部形变，缺少 `tongueOut`；不能宣称角色实现了全部 52 通道。

此前发布版本由该原包选取并重排待机与说话片段得到，网格、高斯和顶点顺序保持原样。此次替换前的发布包已保存到 `backups/`。

恢复原角色（从 `avatar-harness/` 运行；随后重新构建前端与 Go 二进制）：

```sh
python3 scripts/prepare-lam-avatar.py ../assets/characters/lam/chatting_avatar_20261030091048.zip web/public/models/lam/avatar.zip
```

本地语音驱动和部署说明见 `avatar-harness/docs/lam-local.md`。
