<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import Icon from './components/Icon.vue'
import AvatarStage from './components/AvatarStage.vue'
import FaceControls from './components/FaceControls.vue'
import { useHarness } from './useHarness'
import type { Renderer } from './types'
import { BASIC_EMOTIONS, type ExpressionStyle } from './expressionControl'
import { CHENG_ASSET } from './characters/chengAsset'

const app = useHarness()
const { facePanelOpen, faceSupported, faceLive, faceOverrides, latency, expressionState, state, sessions, selected, entries, connected, busy, playing, error, activeTurn, character, moduleStates, renderer, renderState, renderError, cameraStream, cameraState, micState, micLevel, liveTranscript, voiceConnectionState, feedback } = app
const stage = ref<InstanceType<typeof AvatarStage> | null>(null), cameraVideo = ref<HTMLVideoElement | null>(null)
const log = ref<HTMLElement | null>(null), fileInput = ref<HTMLInputElement | null>(null), draft = ref('')
const importOpen = ref(false), settingsOpen = ref(false), historyOpen = ref(false), resetToken = ref(0)
const sourceType = ref('local'), sourceURL = ref(''), sourceError = ref(''), copied = ref(false), fileName = ref('')
const expressionLabels: Record<ExpressionStyle, string> = { neutral: '中性', happy: '快乐', sad: '悲伤', angry: '愤怒', fearful: '恐惧', surprised: '惊讶', disgusted: '厌恶', natural: '自然', focused: '认真', concerned: '关切' }
const driverState = ref('loading'), driverMessage = ref('')
function driverStatus(state: string, message = '') { driverState.value = state; driverMessage.value = message }
let localURL = ''
const roles = [{ key: 'llm', name: 'LLM' }, { key: 'asr', name: 'ASR' }, { key: 'tts', name: 'TTS' }, { key: 'avatar', name: '形象' }]
const configureCommand = computed(() => state.value?.providers.asr.kind === 'bailian' ? './start configure bailian' : './start configure all')
const moduleLabel = (role: string) => {
  const actual = moduleStates.value[role]?.state
  if (actual === 'running') return '运行中'
  if (actual === 'error' || actual === 'timeout') return '异常'
  if (role === 'avatar' && renderState.value === 'error') return '异常'
  if (role === 'avatar' && renderState.value === 'ready') return renderer.value.kind === 'lam' ? driverState.value === 'ready' ? '驱动就绪' : driverState.value === 'error' ? '驱动异常' : '模型加载中' : '预览就绪'
  if (role === 'avatar' && renderState.value === 'embedded') return '页面已载入'
  if (role === 'avatar' && renderState.value === 'loading') return '载入中'
  if (state.value?.providers[role]?.status === 'missing_key') return '缺少密钥'
  return state.value?.providers[role]?.configured ? '已配置' : role === 'llm' ? '待配置' : '待接入'
}
const moduleClass = (role: string) => ['异常', '驱动异常'].includes(moduleLabel(role)) ? 'failure' : ['运行中', '已配置', '预览就绪', '驱动就绪', '页面已载入'].includes(moduleLabel(role)) ? 'positive' : 'pending'
const stageDescription = computed(() => renderState.value === 'ready' ? renderer.value.kind === 'lam' ? `LAM 高斯渲染 · ${driverMessage.value || '语音模型加载中'}` : '浏览器 Three.js 渲染' : renderState.value === 'embedded' ? '外部渲染页面 · 加载完成' : renderState.value === 'loading' ? '正在载入形象' : renderState.value === 'error' ? '形象显示失败' : '形象尚未连接')
const metric = (value?: number) => value == null ? '—' : `${Math.round(value)} ms`
const latencyHelp = computed(() => `${latency.value.source === 'voice' ? '语音：从最后有声输入帧' : '文字：从发送请求'}到首个有声音频实际播放。Avatar：首个已解码音频块的重采样、推理及动作映射耗时。TTS：首段合成开始到上游首音频包。LLM：首次请求到可播报正文，含结构化前缀及工具往返。分项存在重叠，且未穷尽识别、分段、排队与网络，不能直接相加。`)
const feedbackLabel = computed(() => feedback.value.av_ms != null ? `${feedback.value.origin === 'voice' ? '语音' : '文本'}到音画 ${feedback.value.av_ms.toFixed(0)} ms` : feedback.value.audio_ms != null ? `音频起播 ${feedback.value.audio_ms.toFixed(0)} ms · 音画待确认` : '音画反馈尚未测量')
const micDescription = computed(() => voiceConnectionState.value === 'connecting' ? '正在连接实时识别' : `${micState.value === 'on' ? '正在聆听' : micState.value === 'requesting' ? '等待麦克风权限' : '麦克风未开启'} · ${state.value?.providers.asr.configured ? state.value.providers.asr.live_input ? '实时识别 · 可随时插话' : '整句语音识别' : '语音识别待接入'}`)
const time = (at: number) => new Date(at * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
const label = (kind: string, source?: string) => kind === 'user' ? '用户' : kind === 'assistant' ? source === 'local_command' ? '命令' : '角色' : kind === 'tool' ? '工具' : '系统'
async function run(action: () => Promise<unknown>) { try { await action() } catch (exc) { app.notice(String(exc), 'error') } }
async function send() { const text = draft.value.trim(); if (!text) return; draft.value = ''; await app.send(text) }
function openImport() { sourceError.value = ''; importOpen.value = true }
function loadFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (!/\.(glb|vrm)$/i.test(file.name)) { sourceError.value = '请选择二进制 GLB 或 VRM 文件。LAM ZIP 请使用专用渲染页面。'; return }
  if (localURL) URL.revokeObjectURL(localURL)
  localURL = URL.createObjectURL(file); fileName.value = file.name
  app.setRenderer({ kind: 'scene', asset_url: localURL }); importOpen.value = false
  app.notice(`已读取本地形象 ${file.name}，文件未上传。`)
}
function applyURL() {
  try {
    const url = new URL(sourceURL.value, location.href)
    if (!['http:', 'https:'].includes(url.protocol) || !sourceURL.value.trim()) throw new Error('请输入 HTTP(S) 地址。')
    const kind = sourceType.value as Renderer['kind']
    app.setRenderer(['scene', 'lam'].includes(kind) ? { kind, asset_url: url.href } : { kind, url: url.href })
    importOpen.value = false; fileName.value = ''; app.notice('已切换形象来源，等待浏览器确认加载结果。')
  } catch (exc) { sourceError.value = String(exc) }
}
function loadSample() { app.setRenderer({ kind: 'scene', asset_url: '/models/VRM1_Constraint_Twist_Sample.vrm' }); fileName.value = 'VRM 开源预览示例 · © pixiv Inc.'; importOpen.value = false; app.notice('正在加载 pixiv 开源 VRM 示例，仅验证浏览器渲染；语音驱动仍需接入 Avatar provider。') }
function loadLAM() { app.setRenderer({ kind: 'lam', asset_url: '/models/lam/avatar.zip' }); fileName.value = 'LAM · 本地高斯角色'; importOpen.value = false }
function loadCheng() { app.setRenderer({ kind: 'scene', asset_url: CHENG_ASSET }); fileName.value = '澄 · 原创代码角色'; importOpen.value = false; app.notice('已选择原创二次元角色「澄」：本地转头与眨眼已启用，语音口型驱动尚未接入。') }
async function copyCommand() { try { await navigator.clipboard.writeText(configureCommand.value); copied.value = true } catch { copied.value = false } }
function keydown(event: KeyboardEvent) { if (event.key === 'Escape') { importOpen.value = false; settingsOpen.value = false; historyOpen.value = false; app.closeFacePanel(); if (busy.value || playing.value) void run(app.stop) } }
watch(stage, value => { app.mediaHooks.value = value })
watch(selected, () => { fileName.value = '' })
watch(cameraStream, async value => { await nextTick(); if (cameraVideo.value) { cameraVideo.value.srcObject = value; if (value) void cameraVideo.value.play().catch(exc => app.notice(String(exc), 'error')) } })
watch(() => entries.value.length + entries.value.map(entry => entry.text.length).reduce((a, b) => a + b, 0), async () => { await nextTick(); if (log.value) log.value.scrollTop = log.value.scrollHeight })
onMounted(() => { void app.init(); window.addEventListener('keydown', keydown) })
onUnmounted(() => { window.removeEventListener('keydown', keydown); if (localURL) URL.revokeObjectURL(localURL) })
</script>

<template>
  <div class="studio">
    <header class="studio-header">
      <div class="brand"><svg viewBox="0 0 28 28" aria-hidden="true"><path d="m3 25 9-17 4 7-5 10z" fill="#79e4b0"/><path d="m12 7 4-5 12 23h-8z" fill="#83b8df"/><path d="m12 25 4-7 4 7z" fill="#b3f1db"/></svg><span>Avatar Harness</span></div>
      <div class="header-divider" />
      <label class="role-select"><select :value="selected?.character_id" aria-label="选择角色" @change="run(() => app.newSession(($event.target as HTMLSelectElement).value))"><option v-for="item in state?.characters" :key="item.id" :value="item.id">{{ item.name }}</option></select><Icon name="down" :size="15" /></label>
      <nav class="module-strip" aria-label="模块运行状态"><button v-for="role in roles" :key="role.key" :title="moduleStates[role.key]?.message || (role.key === 'avatar' ? '形象显示与语音驱动分别接入；预览就绪不代表驱动模型就绪。' : '已配置只说明配置齐全；运行中与异常来自实际调用。')" @click="settingsOpen = true"><i class="dot" :class="moduleClass(role.key)" /><span>{{ role.name }}</span><small>{{ moduleLabel(role.key) }}</small></button><button @click="settingsOpen = true"><i class="dot" :class="connected ? 'positive' : 'pending'" /><span>工具</span><small>{{ connected ? '可用' : '连接中' }}</small></button></nav>
    </header>

    <main class="workspace">
      <section class="avatar-workspace" aria-label="数字人工作区">
        <header class="stage-header">
          <div class="stage-identity"><h1>{{ character?.name ?? '角色 01' }}</h1><p>{{ stageDescription }}</p><small v-if="fileName">{{ fileName }}</small></div>
          <div class="stage-actions"><label v-if="renderer.kind === 'lam'" class="outline">表情 <select :value="expressionState.mode" aria-label="表情风格" @change="app.setExpressionMode(($event.target as HTMLSelectElement).value)"><option value="auto">自动 · {{ expressionLabels[expressionState.style] }}</option><option v-for="emotion in BASIC_EMOTIONS" :key="emotion" :value="emotion">{{ expressionLabels[emotion] }}</option><optgroup label="交流风格"><option value="focused">认真</option><option value="concerned">关切</option></optgroup></select></label><button v-if="renderer.kind === 'lam'" class="outline" :aria-pressed="facePanelOpen" :disabled="renderState !== 'ready'" @click="app.toggleFacePanel">部位调节</button><button class="outline" @click="openImport"><Icon name="upload" :size="15" />形象来源</button><button class="outline" :disabled="!['scene', 'lam'].includes(renderer.kind) || renderState !== 'ready'" @click="resetToken++"><Icon name="reset" :size="17" />重置视角</button></div>
        </header>
        <FaceControls v-if="facePanelOpen && renderer.kind === 'lam'" :supported="faceSupported" :live="faceLive" :overrides="faceOverrides" @change="app.setFaceChannel" @action="app.previewBrow" @reset="app.resetFace" @close="app.closeFacePanel" />
        <div class="stage-canvas">
          <div class="avatar-frame" :class="{ 'is-ready': renderState === 'ready' || renderState === 'embedded' }" aria-label="数字人显示框">
            <AvatarStage ref="stage" :renderer="renderer" :turn-id="activeTurn" :reset-token="resetToken" :expression="expressionState.style" :intensity="expressionState.intensity" :pad="character?.pad" @status="app.setRenderStatus" @driver="driverStatus" />
            <div v-if="renderState === 'none' || renderState === 'loading' || renderState === 'error'" class="stage-empty" :class="{ 'has-error': renderState === 'error' }">
              <Icon name="bust" :size="90" />
              <h2>{{ renderState === 'loading' ? '正在载入形象' : renderState === 'error' ? '形象加载失败' : '等待形象连接' }}</h2>
              <p>{{ renderState === 'error' ? renderError : renderState === 'loading' ? '资源加载完成后将在此处显示' : '导入 GLB / VRM，或连接数字人服务' }}</p>
              <button v-if="renderState !== 'loading'" class="outline" @click="openImport"><Icon name="upload" />导入形象</button>
            </div>
          </div>
        </div>
        <div class="interaction-controls">
          <div class="control-dock" role="group" aria-label="交互控制栏"><button :class="{ enabled: cameraState === 'on' }" @click="run(app.toggleCamera)"><Icon name="camera" :size="19" /><span>{{ cameraState === 'on' ? '关闭摄像头' : cameraState === 'requesting' ? '取消请求' : '开启摄像头' }}</span></button><button class="voice-control" :class="{ listening: micState === 'on' }" :aria-pressed="micState === 'on'" :disabled="renderer.kind === 'lam' && driverState !== 'ready'" @click="run(app.toggleMic)"><Icon name="mic" :size="21" /><span>{{ micState === 'on' ? '结束聆听' : micState === 'requesting' ? '取消请求' : '开始说话' }}</span></button><button class="stop-control" :disabled="!busy && !playing && moduleStates.asr?.state !== 'running'" aria-label="停止当前回应" @click="run(app.stop)"><Icon name="stop" :size="16" /><span>停止</span></button></div>
          <p><span v-if="micState === 'on'" class="mic-level"><i :style="{ transform: `scaleX(${Math.min(1, micLevel * 12)})` }" /></span>{{ micDescription }}</p>
          <div v-if="liveTranscript" class="live-transcript" aria-live="polite" :title="liveTranscript"><span>正在识别</span>{{ liveTranscript }}</div>
        </div>
        <footer class="runtime-footer"><span><Icon name="prompt" :size="18" /><i class="dot" :class="connected ? 'positive' : 'pending'" />Go 运行时<span class="runtime-version">{{ state?.version }}</span></span><div class="latency-strip" role="group" aria-label="本轮延迟统计" :title="latencyHelp"><span class="latency-total" :class="{ slow: (latency.total_ms ?? 0) > 3000 }"><small>{{ latency.source ? latency.source === 'voice' ? '语音' : '文字' : '' }}</small> Total Delay <b data-latency="total">{{ metric(latency.total_ms) }}</b></span><span>Avatar Delay <b data-latency="avatar">{{ metric(latency.avatar_ms) }}</b></span><span>TTS Delay <b data-latency="tts">{{ metric(latency.tts_ms) }}</b></span><span>LLM Delay <b data-latency="llm">{{ metric(latency.llm_ms) }}</b></span><span class="latency-sync" :title="feedbackLabel" aria-label="音画同步状态">{{ feedback.av_ms != null ? '已同步' : feedback.audio_ms != null ? '音画待确认' : '待测量' }}</span></div></footer>
      </section>

      <div class="side-column">
        <section class="camera-panel" aria-label="摄像头输入模块">
          <header class="camera-header"><span><Icon name="camera" :size="17" />我的摄像头</span><small :class="{ positive: cameraState === 'on' }">{{ cameraState === 'on' ? '已开启' : cameraState === 'requesting' ? '待授权' : cameraState === 'error' ? '不可用' : '未开启' }}</small></header>
          <div class="camera-preview">
            <video ref="cameraVideo" autoplay muted playsinline :class="{ visible: cameraState === 'on' }" aria-label="摄像头本地预览" />
            <div v-if="cameraState !== 'on'" class="camera-empty"><Icon name="camera" :size="26" /><span>{{ cameraState === 'requesting' ? '等待摄像头权限' : cameraState === 'error' ? '摄像头不可用' : '摄像头未开启' }}</span></div>
            <div class="camera-caption"><i v-if="cameraState === 'on'" class="dot positive" />仅本地预览</div>
          </div>
        </section>
      <aside class="log-rail" aria-label="会话日志">
        <header class="log-header"><button class="log-title" @click="historyOpen = !historyOpen">会话日志<Icon name="down" :size="13" /></button><div><button class="outline" @click="run(() => app.newSession())"><Icon name="plus-circle" :size="17" />新会话</button><button class="icon-button" aria-label="清空日志" :disabled="!entries.length" @click="run(app.clear)"><Icon name="trash" :size="18" /></button></div></header>
        <div v-if="historyOpen" class="history-popover"><strong>历史会话</strong><button v-for="session in sessions" :key="session.id" :class="{ selected: selected?.id === session.id }" @click="run(() => app.select(session)); historyOpen = false"><span>{{ state?.characters.find(item => item.id === session.character_id)?.name ?? session.character_id }}</span><small>{{ new Date(session.created_at * 1000).toLocaleDateString('zh-CN') }} {{ time(session.created_at) }}</small></button></div>
        <div ref="log" class="log-content" aria-live="polite">
          <div v-if="!entries.length" class="log-empty">会话与工具事件将在这里记录</div>
          <article v-for="entry in entries" :key="entry.id" class="log-entry" :class="[entry.kind, entry.status === 'error' || entry.status === 'timeout' ? 'failed' : '']">
            <time>{{ time(entry.at) }}</time>
            <div class="log-event"><div class="log-event-header"><span class="source-label">{{ label(entry.kind, entry.source) }}</span><small v-if="entry.draft">生成中</small></div>
              <details v-if="entry.kind === 'tool'"><summary><code>{{ entry.name }}</code><span class="tool-outcome">{{ entry.status === 'running' ? '运行中' : entry.status === 'ok' ? '完成' : entry.status }}<small v-if="entry.elapsed_ms != null"> {{ entry.elapsed_ms.toFixed(1) }} ms</small></span></summary><h4>参数</h4><pre>{{ JSON.stringify(entry.args, null, 2) }}</pre><h4>结果</h4><pre>{{ JSON.stringify(entry.result, null, 2) }}</pre></details>
              <details v-else-if="entry.source === 'local_command' && entry.text.trim().startsWith('{')"><summary>命令结果</summary><pre>{{ entry.text }}</pre></details>
              <p v-else>{{ entry.text }}</p>
            </div>
          </article>
        </div>
        <div v-if="error" class="error-line" role="alert">{{ error }}</div>
        <form class="log-composer" @submit.prevent="send"><input v-model="draft" aria-label="文字输入或命令" placeholder="文字输入 / 命令" autocomplete="off" /><button type="submit" aria-label="发送文字或命令" :disabled="!draft.trim() || !connected"><Icon name="send" :size="20" /></button></form>
      </aside>
      </div>
    </main>

    <div v-if="importOpen" class="dialog-backdrop" @click.self="importOpen = false"><section class="dialog" role="dialog" aria-modal="true" aria-labelledby="import-title"><header><h2 id="import-title">形象接入</h2><button class="icon-button" aria-label="关闭形象接入" @click="importOpen = false"><Icon name="close" /></button></header><p>选择显示形式。模型服务的 FastAPI 接口在终端配置。</p><label class="field-label">显示形式<select v-model="sourceType" aria-label="形象显示形式"><option value="local">本地 GLB / VRM 文件</option><option value="lam">LAM ZIP 资源 URL</option><option value="scene">GLB / VRM 资源 URL</option><option value="iframe">LAM / 数字人渲染页面</option><option value="video">服务端视频 URL</option></select></label><div v-if="sourceType === 'local'" class="local-import"><Icon name="upload" :size="30" /><p>文件由当前浏览器读取和渲染</p><button class="outline" @click="loadLAM">载入 LAM 语音驱动形象</button><button class="outline" @click="fileInput?.click()">选择 GLB / VRM 文件</button><button class="text-button" @click="loadSample">载入开源示例</button><a href="/models/NOTICE.md" target="_blank" rel="noopener">角色来源与许可</a><input ref="fileInput" class="file-input" type="file" accept=".glb,.vrm" aria-label="选择本地形象文件" @change="loadFile" /></div><div v-else><label class="field-label">{{ sourceType === 'iframe' ? '专用渲染页面地址' : '资源地址' }}<input v-model="sourceURL" type="url" placeholder="https://..." aria-label="形象资源地址" /></label><p class="subtle-copy">{{ sourceType === 'iframe' ? 'LAM 高斯资产由专用页面渲染；该服务必须允许嵌入。' : sourceType === 'scene' ? '资源需要允许浏览器跨域读取；口型与表情由驱动 API 另行提供。' : '支持浏览器原生可播放的视频格式。WebRTC 可通过专用渲染页面接入。' }}</p><button class="primary" @click="applyURL">连接形象</button></div><p v-if="sourceError" class="error-line">{{ sourceError }}</p><footer><button class="text-button" :disabled="!state?.providers.avatar.configured" @click="run(app.resolveAvatar); importOpen = false"><Icon name="link" :size="15" />从 Avatar provider 获取</button><span>永久配置：<code>./start configure-avatar</code></span></footer></section></div>
    <div v-if="settingsOpen" class="dialog-backdrop" @click.self="settingsOpen = false"><section class="dialog provider-dialog" role="dialog" aria-modal="true" aria-labelledby="provider-title"><header><h2 id="provider-title">模块与服务</h2><button class="icon-button" aria-label="关闭模块状态" @click="settingsOpen = false"><Icon name="close" /></button></header><p>{{ state?.providers.asr.kind === 'bailian' ? '对话、语音识别与语音合成使用阿里云百炼，共用一份 API Key。' : '模型与服务在终端配置，支持外部 FastAPI 接入。' }}</p><div v-for="role in roles" :key="role.key" class="provider-detail"><strong>{{ role.name }}</strong><span :title="state?.providers[role.key]?.model || state?.providers[role.key]?.kind">{{ state?.providers[role.key]?.model || state?.providers[role.key]?.kind }}</span><small>{{ moduleLabel(role.key) }}</small></div><p class="subtle-copy">API 密钥与接入地址在终端配置；配置齐全后仍需实际调用验证。</p><div class="command-copy"><code>{{ configureCommand }}</code><button class="outline" @click="copyCommand">{{ copied ? '已复制' : '复制命令' }}</button></div><a href="/docs" target="_blank" rel="noopener">查看接口说明</a></section></div>
  </div>
</template>
