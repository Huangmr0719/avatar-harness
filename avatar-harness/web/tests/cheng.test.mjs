import assert from 'node:assert/strict'
import test from 'node:test'
import * as THREE from 'three'
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js'
import { createChengAvatar, isChengAvatar, animateChengIdle, setChengExpression } from '../src/characters/cheng.ts'
import { exportChengGLB } from '../scripts/export-cheng.mjs'

test('original avatar has finite geometry, head controls, and actual facial deformations', () => {
  const avatar = createChengAvatar(), channels = new Set()
  assert.ok(isChengAvatar(avatar))
  assert.ok(avatar.getObjectByName('Head').isBone)
  assert.equal(avatar.getObjectByName('Head').parent.name, 'Neck')
  let triangles = 0
  avatar.traverse(node => {
    if (!node.isMesh) return
    const position = node.geometry.getAttribute('position')
    assert.ok(Array.from(position.array).every(Number.isFinite), node.name)
    triangles += (node.geometry.index?.count ?? position.count) / 3
    for (const [name, index] of Object.entries(node.morphTargetDictionary ?? {})) {
      channels.add(name)
      assert.ok(node.geometry.morphAttributes.position[index].array.some(value => Math.abs(value) > .00001), `${node.name}: ${name}`)
    }
  })
  assert.ok(triangles < 150000, `triangles: ${triangles}`)
  for (const name of ['jawOpen', 'eyeBlinkLeft', 'eyeBlinkRight', 'mouthSmileLeft', 'mouthSmileRight']) assert.ok(channels.has(name), name)
  const bounds = new THREE.Box3().setFromObject(avatar)
  assert.ok(bounds.max.y > 1.3 && bounds.max.y < 1.5)
})

test('idle turns the head and blinks without opening the mouth', () => {
  const avatar = createChengAvatar(), head = avatar.getObjectByName('Head')
  animateChengIdle(avatar, 1)
  assert.notEqual(head.rotation.y, 0)
  animateChengIdle(avatar, 4.47)
  const white = avatar.getObjectByName('EyeWhite_1')
  assert.ok(white.morphTargetInfluences[white.morphTargetDictionary.eyeBlinkLeft] > .99)
  const mouth = avatar.getObjectByName('MouthCavity')
  assert.equal(mouth.morphTargetInfluences[mouth.morphTargetDictionary.jawOpen], 0)
  setChengExpression(avatar, 'jawOpen', .6)
  assert.equal(mouth.morphTargetInfluences[mouth.morphTargetDictionary.jawOpen], .6)
  animateChengIdle(avatar, 5)
  assert.equal(white.morphTargetInfluences[white.morphTargetDictionary.eyeBlinkLeft], 0)
})

test('generated GLB round-trips with no external resources and keeps morph channel names', async () => {
  const bytes = await exportChengGLB(), view = new DataView(bytes)
  assert.equal(view.getUint32(0, true), 0x46546c67)
  const json = JSON.parse(new TextDecoder().decode(new Uint8Array(bytes, 20, view.getUint32(12, true))).trim())
  assert.equal(json.images, undefined)
  assert.ok(json.buffers.every(buffer => !buffer.uri))
  assert.ok(json.nodes.some(node => node.name === 'Head'))
  const loaded = await new GLTFLoader().parseAsync(bytes, '')
  assert.ok(isChengAvatar(loaded.scene))
  const mouth = loaded.scene.getObjectByName('MouthCavity')
  assert.ok('jawOpen' in mouth.morphTargetDictionary)
  setChengExpression(loaded.scene, 'jawOpen', .5)
  assert.equal(mouth.morphTargetInfluences[mouth.morphTargetDictionary.jawOpen], .5)
})
