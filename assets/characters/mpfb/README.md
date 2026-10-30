# MPFB 原型角色资产

用途：先跑通浏览器 3D 角色、表情、上半身动作和语音驱动，再替换为定制人物。

## 文件

- `mpfb.glb`：上游原始角色，36,815,920 字节，未修改。
- `asset_inspection.json`：本轮对 GLB 容器、骨骼、面部通道和外部资源依赖的结构检查记录。

## 来源与许可依据

- 原始文件：https://github.com/met4citizen/TalkingHead/blob/main/avatars/mpfb.glb
- 上游说明：https://github.com/met4citizen/TalkingHead/blob/main/README.md
- 上游明确写明此 `mpfb.glb` 由 Blender + MPFB 创建，角色资产采用 CC0。
- CC0 文本：https://creativecommons.org/publicdomain/zero/1.0/
- 许可依据针对这个角色文件；不能推及同一仓库的其他人物、动作或背景资产。
- 对照上游 Git blob SHA-1：`cd2ebbe1fe8bcbf3d5b2dfbf1262a7f76814e68f`。
- SHA-256：`63c645a2a863b9972e9a9c2ed576a1de4c390b8475508e1473e69c87a3ee299c`。

## 已检查的结构

- GLB 2.0，8 个 mesh、1 套 skin、67 个骨骼关节。
- 52 个 ARKit 标准面部通道名全部存在。
- 15 个 Oculus viseme 名称中有 14 个，缺少 `viseme_sil`；不能声称完整包含全部 15 个。静音/闭口行为需在播放器中检查。
- 文件没有内嵌动画片段；身体手势需要播放器程序动画或另行加载动作。
- 无外部 buffer 或图片 URI；该 GLB 的这些资源均为内嵌。

这是结构检查，尚未做浏览器渲染、各通道实际形变、骨骼蒙皮或 Audio2Face 驱动测试。

## 最短接入步骤

1. 使用 TalkingHead 的现成角色播放器加载 `mpfb.glb`，切换到上半身视角。
2. 用表情滑杆/直接通道控制逐项检查张嘴、闭唇、眨眼、眉毛、微笑和皱眉。
3. 检查内置手势，例如抬手、指示、赞同和耸肩；额外动作再加载兼容骨骼动画。
4. 接入 Omni 音频与 Audio2Face 输出，配置形变映射、值域与时间戳。52 个通道名齐全不等于已自动完成 A2F 适配。
5. 跑通同步和打断后，再定制外观。

TalkingHead：https://github.com/met4citizen/TalkingHead

## 后续定制

使用 Blender + MPFB，按上游教程修改年龄、体型、肤色、发型和服装，同时保持兼容骨骼及面部通道。新增素材的许可分别记录。

教程：https://github.com/met4citizen/TalkingHead/blob/main/blender/MPFB/MPFB.md

交付给美术时要求：GLB 和可编辑源文件、身体骨骼、ARKit 52 个面部形变、眼睛与牙齿结构、可用材质、3–5 个基础动作，以及全部素材的使用授权说明。具体多边形数与纹理尺寸按画面需求调整，不在原型阶段追求精细外观。
