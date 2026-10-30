# LAM 角色资产

用户提供的原包：`chatting_avatar_20261030091048.zip`，4,102,147 字节。

- 来源：https://virutalbuy-public.oss-cn-hangzhou.aliyuncs.com/share/aigc3d/LAM_Chatting_Avatar/chatting_avatar_20261030091048.zip
- SHA-256：`e9e2cd00ff3ae91a8adc5e18b65ed810f5bf01d37aea23b17c5fd0010bf2937b`
- 原包包含 `skin.glb`、`offset.ply`、`animation.glb`、`vertex_order.json`；不包含语音驱动模型或许可说明。资产授权以用户生成／取得资产时的约定为准。
- `skin.glb` 有 51 个命名面部形变，缺少 `tongueOut`；不能宣称角色实现了全部 52 通道。

当前发布版本在 `avatar-harness/web/public/models/lam/avatar.zip`。网格、高斯和顶点顺序保持原样，只从原动画中选择待机与说话片段，按渲染器要求排列。

重新生成（从 `avatar-harness/` 运行）：

```sh
python3 scripts/prepare-lam-avatar.py ../assets/characters/lam/chatting_avatar_20261030091048.zip web/public/models/lam/avatar.zip
```

本地语音驱动和部署说明见 `avatar-harness/docs/lam-local.md`。
