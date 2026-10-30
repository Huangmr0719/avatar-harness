# 澄 · 原创代码角色

这是本项目用 Three.js 程序生成的原创二次元半身角色，不使用 Blender、外部人物模型、贴图或生成模型权重。

- 生成源代码：`avatar-harness/web/src/characters/cheng.ts`（相对项目根目录）。
- 成品：`avatar-harness/web/public/models/cheng.glb`。
- 蓝灰短发、青绿色眼睛、米白外套；头发、虹膜、服装、脸部均为几何网格。
- `Neck` / `Head` 为层级控制节点，头部附属网格随头转动；不是全身蒙皮角色。
- 实际形变通道：`jawOpen`、`eyeBlinkLeft`、`eyeBlinkRight`、`mouthSmileLeft`、`mouthSmileRight`。没有完整 ARKit 52 或五元音口型。
- 项目播放器为该角色添加本地待机转头和眨眼；这些程序动画不代表音频推理，也不计为有效音画响应。语音驱动需另接。

## 重新生成

在 `avatar-harness/web/` 执行：

```sh
npm run model:cheng
npm test
npm run build
```

生成脚本仅使用项目现有的 Node.js 与 Three.js，模型不引用外部文件。修改后重建 Go 可执行文件，新的内置资产才会进入桌面程序。

在工作台点击「形象来源 → 载入原创二次元角色 · 澄」。不修改现有角色配置；需要永久绑定时可在现有配置向导中选择该 GLB。

开发预览：启动现有 Vite 开发服务器，访问 `/tests/character-preview.html`，检查正面、左右转头、眨眼和张嘴。

## 来源

几何与配色由本项目代码生成，非第三方角色改编；生成使用了 AI 编程辅助。没有另行引入第三方角色资产许可。Three.js 等运行库继续遵守各自许可。
