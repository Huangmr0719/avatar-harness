// Audit and level the actual animation clips; never alter geometry or morphs.
import fs from 'node:fs/promises'
import assert from 'node:assert/strict'
import { AnimationMixer, Quaternion, Vector3 } from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { unzipSync, zipSync } from 'three/examples/jsm/libs/fflate.module.js'

const [mode, input, output] = process.argv.slice(2)
if (!['inspect', 'check', 'level'].includes(mode) || !input || (mode === 'level' && (!output || input === output))) {
  throw new Error('Usage: node web/scripts/lam-pose.mjs inspect|check input.zip; level input.zip output.zip')
}
const entries = unzipSync(await fs.readFile(input))
const member = name => Object.keys(entries).find(key => key.endsWith('/' + name) || key === name)
const skinName = member('skin.glb'), animationName = member('animation.glb')
assert.ok(skinName && animationName, 'LAM skin and animation are required')
const loader = new GLTFLoader()
const load = bytes => loader.parseAsync(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength), '')
const skin = await load(entries[skinName])

async function measure(bytes) {
  const { animations } = await load(bytes)
  return animations.map(clip => {
    const scene = skin.scene.clone(true)
    const head = scene.getObjectByName('head')
    assert.ok(head, 'Missing head bone')
    const mixer = new AnimationMixer(scene)
    mixer.clipAction(clip).play()
    const pitches = [], eyes = []
    for (let i = 0; i < 121; i++) {
      mixer.setTime(clip.duration * i / 121)
      scene.updateMatrixWorld(true)
      const origin = head.getWorldPosition(new Vector3())
      const forward = head.localToWorld(new Vector3(0, 0, 1)).sub(origin).normalize()
      pitches.push(Math.asin(Math.max(-1, Math.min(1, forward.y))) * 180 / Math.PI)
      const eye = scene.getObjectByName('lEye')
      if (eye) eyes.push(eye.getWorldPosition(new Vector3()).y)
    }
    mixer.stopAllAction()
    mixer.uncacheRoot(scene)
    const sorted = [...pitches].sort((a, b) => a - b)
    return { name: clip.name, median_pitch_deg: sorted[Math.floor(sorted.length / 2)],
      min_pitch_deg: Math.min(...pitches), max_pitch_deg: Math.max(...pitches),
      mean_eye_y: eyes.reduce((a, b) => a + b, 0) / eyes.length }
  })
}
const assertLevel = report => {
  for (const clip of report) assert.ok(Math.abs(clip.median_pitch_deg) < 1,
    `${clip.name}: sustained pitch ${clip.median_pitch_deg.toFixed(3)} degrees; expected within 1 degree of level`)
}
const before = await measure(entries[animationName])
if (mode !== 'level') {
  console.log(JSON.stringify(before, null, 2))
  if (mode === 'check') assertLevel(before)
} else {
  const bytes = entries[animationName].slice()
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const jsonLength = view.getUint32(12, true)
  const doc = JSON.parse(new TextDecoder().decode(bytes.subarray(20, 20 + jsonLength)))
  const binStart = 20 + jsonLength + 8
  const adjusted = new Set()
  for (const animation of doc.animations) {
    const pitch = before.find(clip => clip.name === animation.name).median_pitch_deg
    const channel = animation.channels.find(c => doc.nodes[c.target.node].name === 'head' && c.target.path === 'rotation')
    assert.ok(channel, 'Missing animated head rotation')
    const sampler = animation.samplers[channel.sampler]
    assert.equal(sampler.interpolation ?? 'LINEAR', 'LINEAR')
    assert.ok(!adjusted.has(sampler.output), 'Shared rotation output needs explicit handling')
    adjusted.add(sampler.output)
    const accessor = doc.accessors[sampler.output], buffer = doc.bufferViews[accessor.bufferView]
    assert.equal(accessor.componentType, 5126)
    assert.equal(accessor.type, 'VEC4')
    assert.ok(!accessor.sparse && !accessor.min && !accessor.max, 'Unexpected rotation accessor layout')
    const start = binStart + (buffer.byteOffset ?? 0) + (accessor.byteOffset ?? 0)
    const stride = buffer.byteStride ?? 16
    const correction = new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), pitch * Math.PI / 180)
    for (let i = 0; i < accessor.count; i++) {
      const offset = start + i * stride
      const q = new Quaternion(...[0, 1, 2, 3].map(j => view.getFloat32(offset + j * 4, true)))
      q.multiply(correction).normalize()
      q.toArray().forEach((value, j) => view.setFloat32(offset + j * 4, value, true))
    }
  }
  const after = await measure(bytes)
  assertLevel(after)
  const original = { ...entries }
  entries[animationName] = bytes
  const packed = zipSync(entries, { level: 6 })
  const roundTrip = unzipSync(packed)
  for (const key of Object.keys(original)) if (key !== animationName) assert.deepEqual(roundTrip[key], original[key])
  await fs.writeFile(output, packed)
  console.log(JSON.stringify({ input, output, unchanged_geometry: true, before, after }, null, 2))
}
