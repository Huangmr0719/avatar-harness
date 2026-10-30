import * as THREE from 'three'
import { CHENG_MARKER } from './chengAsset.ts'

// Original, texture-free geometry. Coordinates are in metres, facing +Z.
type Point = [number, number, number]
type Ring = [number, number, number]
type Warp = (point: THREE.Vector3) => THREE.Vector3

const colors = {
  skin: '#f5cdbb', blush: '#e99999', hair: '#253c58', hairLight: '#466580',
  ink: '#182336', cream: '#e5e6df', white: '#fff6eb', teal: '#398e92',
  gold: '#d4ae70', orange: '#dc805f', shirt: '#293e4c', mouth: '#874653',
}

function material(color: string, unlit = false) {
  return unlit
    ? new THREE.MeshBasicMaterial({ color, side: THREE.DoubleSide })
    : new THREE.MeshStandardMaterial({ color, roughness: .87, metalness: 0, side: THREE.DoubleSide })
}

function mesh(parent: THREE.Object3D, name: string, geometry: THREE.BufferGeometry, mat: THREE.Material) {
  const object = new THREE.Mesh(geometry, mat)
  object.name = name
  parent.add(object)
  return object
}

function surface(rows: number, columns: number, point: (u: number, v: number) => Point) {
  const positions: number[] = [], indices: number[] = []
  for (let row = 0; row <= rows; row++) {
    for (let column = 0; column <= columns; column++) positions.push(...point(row / rows, column / columns))
  }
  for (let row = 0; row < rows; row++) {
    for (let column = 0; column < columns; column++) {
      const a = row * (columns + 1) + column, b = a + columns + 1
      indices.push(a, b, a + 1, a + 1, b, b + 1)
    }
  }
  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3))
  geometry.setIndex(indices)
  geometry.computeVertexNormals()
  const normals = geometry.getAttribute('normal'), normal = new THREE.Vector3()
  for (let i = 0; i < normals.count; i++) {
    normal.fromBufferAttribute(normals, i)
    if (normal.lengthSq() < .000001) normal.set(0, 0, 1)
    else normal.normalize()
    normals.setXYZ(i, normal.x, normal.y, normal.z)
  }
  return geometry
}

function morph(object: THREE.Mesh, name: string, warp: Warp) {
  const source = object.geometry.getAttribute('position'), values: number[] = []
  for (let i = 0; i < source.count; i++) {
    const original = new THREE.Vector3().fromBufferAttribute(source, i)
    const target = warp(original.clone()).sub(original)
    values.push(target.x, target.y, target.z)
  }
  const attribute = new THREE.Float32BufferAttribute(values, 3)
  attribute.name = name
  object.geometry.morphTargetsRelative = true
  object.geometry.morphAttributes.position ??= []
  object.geometry.morphAttributes.position.push(attribute)
  object.updateMorphTargets()
}

function curve(parent: THREE.Object3D, name: string, points: Point[], radius: number, mat: THREE.Material) {
  const path = new THREE.CatmullRomCurve3(points.map(point => new THREE.Vector3(...point)))
  return mesh(parent, name, new THREE.TubeGeometry(path, 32, radius, 6, false), mat)
}

function ellipsoid(parent: THREE.Object3D, name: string, position: Point, scale: Point, mat: THREE.Material) {
  const geometry = new THREE.SphereGeometry(1, 32, 24)
  geometry.scale(...scale)
  geometry.translate(...position)
  return mesh(parent, name, geometry, mat)
}

// Cubic interpolation gives the face and clothing a continuous silhouette.
function profile(rings: Ring[], y: number): [number, number] {
  let i = 0
  while (i + 2 < rings.length && rings[i + 1][0] < y) i++
  const p1 = rings[i], p2 = rings[i + 1], p0 = rings[Math.max(0, i - 1)], p3 = rings[Math.min(rings.length - 1, i + 2)]
  const t = THREE.MathUtils.clamp((y - p1[0]) / (p2[0] - p1[0]), 0, 1)
  const interpolate = (index: 1 | 2) => {
    const m1 = (p2[index] - p0[index]) / (p2[0] - p0[0]) * (p2[0] - p1[0])
    const m2 = (p3[index] - p1[index]) / (p3[0] - p1[0]) * (p2[0] - p1[0])
    return Math.max(0, (2 * t ** 3 - 3 * t ** 2 + 1) * p1[index] + (t ** 3 - 2 * t ** 2 + t) * m1 + (-2 * t ** 3 + 3 * t ** 2) * p2[index] + (t ** 3 - t ** 2) * m2)
  }
  return [interpolate(1), interpolate(2)]
}

const faceRings: Ring[] = [
  [0, .006, .018], [.035, .071, .096], [.085, .122, .143], [.16, .175, .179],
  [.25, .217, .199], [.34, .232, .199], [.44, .23, .184], [.53, .206, .158],
  [.61, .148, .105], [.657, .055, .035], [.667, .001, .001],
]

function gaussian(x: number, center: number, width: number) { return Math.exp(-(((x - center) / width) ** 2)) }

function faceZ(x: number, y: number) {
  const [width, depth] = profile(faceRings, y)
  const front = Math.sqrt(Math.max(0, 1 - (x / Math.max(width, .001)) ** 2))
  const nose = .032 * gaussian(x, 0, .027) * gaussian(y, .253, .052)
  const bridge = .008 * gaussian(x, 0, .025) * gaussian(y, .327, .07)
  return depth * front + nose + bridge
}

function makeFace(head: THREE.Object3D) {
  const geometry = surface(100, 112, (u, v) => {
    const y = .667 * u, angle = -v * Math.PI * 2
    const [width, depth] = profile(faceRings, y)
    const x = width * Math.sin(angle), front = Math.cos(angle)
    return [x, y, front > 0 ? faceZ(x, y) : depth * front * .94]
  })
  const skin = new THREE.Color(colors.skin), blush = new THREE.Color(colors.blush), vertexColors: number[] = []
  const position = geometry.getAttribute('position')
  for (let i = 0; i < position.count; i++) {
    const x = position.getX(i), y = position.getY(i), z = position.getZ(i)
    const cheeks = (gaussian(x, .145, .05) + gaussian(x, -.145, .05)) * gaussian(y, .253, .038)
    const amount = z > 0 ? cheeks * .35 + gaussian(x, 0, .021) * gaussian(y, .235, .025) * .14 : 0
    const color = skin.clone().lerp(blush, amount)
    vertexColors.push(color.r, color.g, color.b)
  }
  geometry.setAttribute('color', new THREE.Float32BufferAttribute(vertexColors, 3))
  mesh(head, 'Face', geometry, new THREE.MeshStandardMaterial({ vertexColors: true, roughness: .94 }))
  for (const side of [-1, 1]) {
    ellipsoid(head, `Ear_${side}`, [side * .224, .283, -.002], [.033, .063, .026], material(colors.skin))
    ellipsoid(head, `EarInner_${side}`, [side * .24, .287, .013], [.014, .036, .009], material('#d79c94'))
  }
}

function makeEye(head: THREE.Object3D, side: number) {
  const cx = side * .105, cy = .347, halfWidth = .084, halfHeight = .052
  const blink = side < 0 ? 'eyeBlinkRight' : 'eyeBlinkLeft'
  const eyeZ = (x: number, y: number, offset = 0) => {
    const dx = (x - cx) / halfWidth, dy = (y - cy - side * .095 * (x - cx)) / halfHeight
    return faceZ(x, y) + .006 + .014 * Math.max(0, 1 - dx * dx - dy * dy) + offset
  }
  const addBlink = (object: THREE.Mesh) => morph(object, blink, point => {
    const baseline = cy - .006 + side * .095 * (point.x - cx)
    point.y = baseline + (point.y - baseline) * .018
    point.z = faceZ(point.x, point.y) + .010 + (point.z - eyeZ(point.x, cy)) * .1
    return point
  })
  const white = mesh(head, `EyeWhite_${side}`, surface(12, 64, (u, v) => {
    const angle = v * Math.PI * 2, x = cx + halfWidth * u * Math.cos(angle)
    const y = cy + halfHeight * u * Math.sin(angle) * (1 + side * .12 * Math.cos(angle)) + side * .095 * (x - cx)
    return [x, y, eyeZ(x, y)]
  }), material(colors.white, true))
  addBlink(white)

  const irisRadius = .0365
  const irisGeometry = surface(12, 80, (u, v) => {
    const angle = v * Math.PI * 2, x = cx + irisRadius * u * Math.cos(angle)
    const y = cy - .001 + irisRadius * 1.17 * u * Math.sin(angle)
    return [x, y, eyeZ(x, y, .0015)]
  })
  const irisColors: number[] = []
  for (let row = 0; row <= 12; row++) {
    for (let column = 0; column <= 80; column++) {
      const radius = row / 12, angle = column / 80 * Math.PI * 2
      const top = Math.max(0, Math.sin(angle))
      const color = new THREE.Color('#73c9b2').lerp(new THREE.Color('#173c59'), .17 + top * .68)
      color.lerp(new THREE.Color('#143c4e'), THREE.MathUtils.smoothstep(radius, .84, 1))
      color.multiplyScalar(1 + Math.sin(angle * 25) * .065 * radius)
      irisColors.push(color.r, color.g, color.b)
    }
  }
  irisGeometry.setAttribute('color', new THREE.Float32BufferAttribute(irisColors, 3))
  addBlink(mesh(head, `Iris_${side}`, irisGeometry, new THREE.MeshBasicMaterial({ vertexColors: true, side: THREE.DoubleSide })))
  const disc = (name: string, x: number, y: number, rx: number, ry: number, color: string, offset: number) => {
    const object = mesh(head, `${name}_${side}`, surface(4, 40, (u, v) => {
      const angle = v * Math.PI * 2, px = x + rx * u * Math.cos(angle), py = y + ry * u * Math.sin(angle)
      return [px, py, eyeZ(px, py, offset)]
    }), material(color, true))
    addBlink(object)
  }
  disc('Pupil', cx, cy + .002, .013, .026, '#102e40', .0025)
  disc('EyeLight', cx - .013, cy + .019, .009, .012, '#ffffff', .004)
  disc('EyeLightSmall', cx + .016, cy - .018, .0035, .0045, '#e1fff4', .004)

  const lid = (upper: boolean) => {
    const points: Point[] = []
    for (let i = 0; i <= 16; i++) {
      const angle = Math.PI * i / 16, x = cx + halfWidth * Math.cos(angle)
      const y = cy + (upper ? 1 : -1) * halfHeight * Math.sin(angle) * (1 + side * .12 * Math.cos(angle)) + side * .095 * (x - cx)
      points.push([x, y, eyeZ(x, y, .002)])
    }
    const lash = curve(head, `${upper ? 'Upper' : 'Lower'}Lash_${side}`, points, upper ? .0035 : .0016, material(upper ? colors.ink : '#9b6d72', true))
    morph(lash, blink, point => {
      const dx = THREE.MathUtils.clamp((point.x - cx) / halfWidth, -1, 1)
      const open = cy + (upper ? 1 : -1) * halfHeight * Math.sqrt(1 - dx * dx) * (1 + side * .12 * dx) + side * .095 * (point.x - cx)
      const closed = cy - .006 + .006 * dx * dx + side * .095 * (point.x - cx)
      point.y += closed - open
      point.z = faceZ(point.x, point.y) + (upper ? .017 : .015)
      return point
    })
  }
  lid(true); lid(false)
  const wing: Point[] = [[cx + side * .068, cy + .033, eyeZ(cx + side * .068, cy + .033, .003)], [cx + side * .088, cy + .014, eyeZ(cx + side * .088, cy + .014, .003)], [cx + side * .102, cy + .026, eyeZ(cx + side * .102, cy + .026, .003)]]
  addBlink(curve(head, `LashWing_${side}`, wing, .0026, material(colors.ink, true)))
  const eyebrow: Point[] = []
  for (let i = 0; i <= 12; i++) {
    const x = cx + (i / 12 - .5) * .127, y = .432 + .012 * Math.sin(i / 12 * Math.PI) + side * .08 * (x - cx)
    eyebrow.push([x, y, faceZ(x, y) + .003])
  }
  curve(head, `Eyebrow_${side}`, eyebrow, .0038, material(colors.hair, true))
}

function makeMouth(head: THREE.Object3D) {
  const y0 = .143
  const opening = mesh(head, 'MouthCavity', surface(8, 64, (u, v) => {
    const angle = v * Math.PI * 2, x = .035 * u * Math.cos(angle), y = y0 + .0025 * u * Math.sin(angle)
    return [x, y, faceZ(x, y) + .009]
  }), material(colors.mouth, true))
  morph(opening, 'jawOpen', point => {
    point.y = y0 - .011 + (point.y - y0) * 10
    point.z = faceZ(point.x, point.y) + .009
    return point
  })
  morph(opening, 'mouthSmileLeft', point => { point.y += .008 * Math.max(0, point.x / .035) ** 2; point.z = faceZ(point.x, point.y) + .009; return point })
  morph(opening, 'mouthSmileRight', point => { point.y += .008 * Math.max(0, -point.x / .035) ** 2; point.z = faceZ(point.x, point.y) + .009; return point })
  for (const upper of [true, false]) {
    const points: Point[] = []
    for (let i = 0; i <= 20; i++) {
      const angle = Math.PI * i / 20, x = .035 * Math.cos(angle)
      const y = y0 + (upper ? 1 : -1) * .0025 * Math.sin(angle)
      points.push([x, y, faceZ(x, y) + .010])
    }
    const lip = curve(head, upper ? 'UpperLip' : 'LowerLip', points, upper ? .0015 : .001, material(upper ? '#945a65' : '#d39291', true))
    morph(lip, 'jawOpen', point => {
      const center = y0 + (upper ? 1 : -1) * .0025 * Math.sqrt(Math.max(0, 1 - (point.x / .035) ** 2))
      point.y += -.011 + (center - y0) * 9
      point.z = faceZ(point.x, point.y) + .011
      return point
    })
  }
}

function makeHair(head: THREE.Object3D) {
  const hairRings: Ring[] = [[.09, .185, .105], [.18, .25, .181], [.33, .259, .218], [.46, .248, .208], [.56, .216, .172], [.64, .141, .109], [.69, .001, .001]]
  const cap = surface(60, 112, (u, v) => {
    const angle = Math.PI * 2 * v
    const front = THREE.MathUtils.smoothstep(Math.cos(angle), .12, .8)
    const bottom = .115 + front * .37
    const y = THREE.MathUtils.lerp(bottom, .69, u), [width, depth] = profile(hairRings, y)
    return [width * Math.sin(angle), y, depth * Math.cos(angle) - .014]
  })
  mesh(head, 'BobHairCap', cap, material(colors.hair))
  const lock = (name: string, points: Point[], width: number, highlight = true) => {
    const path = new THREE.CatmullRomCurve3(points.map(point => new THREE.Vector3(...point)))
    const at = (t: number, across: number): Point => {
      const p = path.getPoint(t)
      const taper = Math.sin(Math.PI * (.12 + t * .88)) ** .62
      return [p.x + across * width * taper, p.y, p.z + (1 - across * across) * .011]
    }
    mesh(head, name, surface(32, 12, (u, v) => at(u, v * 2 - 1)), material(colors.hair))
    for (const side of [-1, 1]) {
      const edge: Point[] = Array.from({ length: 20 }, (_, i) => at(i / 19, side))
      curve(head, `${name}_Edge${side}`, edge, .0011, material(colors.ink))
    }
    if (highlight) {
      mesh(head, `${name}_Highlight`, surface(24, 4, (u, v) => {
        const t = .13 + u * .69, p = at(t, -.24 + (v - .5) * .23 * Math.sin(Math.PI * u))
        p[2] += .0015
        return p
      }), material(colors.hairLight))
    }
  }
  lock('FringeOuterRight', [[-.12, .624, .103], [-.195, .54, .166], [-.216, .412, .158], [-.209, .276, .13]], .047)
  lock('FringeRight', [[-.052, .666, .07], [-.12, .58, .172], [-.142, .485, .204], [-.163, .417, .194]], .066)
  lock('FringeCenter', [[.035, .673, .052], [.012, .596, .171], [-.034, .517, .213], [-.077, .445, .216]], .071)
  lock('FringeSweep', [[.066, .658, .089], [.097, .583, .177], [.103, .52, .204], [.04, .463, .222]], .058)
  lock('FringeLeft', [[.12, .615, .119], [.176, .54, .174], [.197, .456, .177], [.207, .34, .144]], .041)
  for (const side of [-1, 1]) {
    lock(`SideBob_${side}`, [[side * .215, .526, .091], [side * .249, .377, .114], [side * .244, .217, .112], [side * .203, .095, .091]], .047)
    lock(`SideBack_${side}`, [[side * .22, .49, .005], [side * .268, .337, -.015], [side * .258, .168, -.025], [side * .227, .094, .008]], .032)
  }
  curve(head, 'GoldHairpin', [[.153, .486, .201], [.182, .465, .193], [.21, .441, .17]], .0035, material(colors.gold))
  curve(head, 'GoldHairpinSmall', [[.17, .50, .19], [.198, .477, .179], [.222, .453, .153]], .0023, material(colors.gold))
}

function makeClothing(parent: THREE.Object3D) {
  const bodyRings: Ring[] = [[0, .285, .137], [.12, .30, .16], [.29, .33, .177], [.43, .36, .157], [.51, .32, .126], [.59, .145, .082], [.635, .092, .069]]
  const bodyPoint = (y: number, angle: number, extra = 0): Point => {
    const [width, depth] = profile(bodyRings, y)
    return [(width + extra) * Math.sin(angle), y, (depth + extra) * Math.cos(angle)]
  }
  mesh(parent, 'InnerTop', surface(50, 80, (u, v) => bodyPoint(u * .635, v * Math.PI * 2)), material(colors.shirt))
  mesh(parent, 'Jacket', surface(50, 96, (u, v) => bodyPoint(u * .59, .36 + v * (Math.PI * 2 - .72), .007)), material(colors.cream))
  for (const side of [-1, 1]) {
    const sleeve = surface(40, 64, (u, v) => {
      const y = .005 + u * .482, angle = v * Math.PI * 2
      const width = .074 + .028 * Math.sin(Math.PI * u), depth = .093 + .014 * Math.sin(Math.PI * u)
      const cx = side * (.314 + .022 * Math.sin(Math.PI * u) - .025 * u)
      return [cx + width * Math.sin(angle), y, depth * Math.cos(angle) - .012]
    })
    mesh(parent, `Sleeve_${side}`, sleeve, material(colors.cream))
    const piping: Point[] = Array.from({ length: 30 }, (_, i) => bodyPoint(i / 29 * .586, side * .362, .009))
    curve(parent, `JacketPiping_${side}`, piping, .0021, material(colors.gold))
    const collarShape = new THREE.Shape()
    collarShape.moveTo(side * .04, .57); collarShape.lineTo(side * .105, .649); collarShape.lineTo(side * .179, .566); collarShape.lineTo(side * .116, .455); collarShape.closePath()
    const collar = mesh(parent, `Collar_${side}`, new THREE.ExtrudeGeometry(collarShape, { depth: .008, bevelEnabled: true, bevelThickness: .002, bevelSize: .002, bevelSegments: 2, steps: 1 }), material(colors.white))
    collar.position.z = .09
    curve(parent, `ShoulderSeam_${side}`, [[side * .17, .572, .112], [side * .257, .531, .12], [side * .328, .47, .108]], .0015, material('#aebbb8'))
  }
  const neck = new THREE.CylinderGeometry(.065, .079, .23, 40)
  neck.translate(0, .70, -.05)
  mesh(parent, 'NeckSkin', neck, material(colors.skin))
  const collarBand = new THREE.CylinderGeometry(.085, .09, .077, 48, 1, true)
  collarBand.translate(0, .611, -.002)
  mesh(parent, 'StandCollar', collarBand, material(colors.shirt))
  const pendant = new THREE.Shape()
  pendant.moveTo(0, .605); pendant.lineTo(.021, .574); pendant.lineTo(0, .54); pendant.lineTo(-.021, .574); pendant.closePath()
  const charm = mesh(parent, 'AmberPendant', new THREE.ExtrudeGeometry(pendant, { depth: .007, bevelEnabled: true, bevelSize: .002, bevelThickness: .002, bevelSegments: 2 }), material(colors.orange))
  charm.position.z = .108
  curve(parent, 'PendantPin', [[0, .604, .119], [0, .575, .119], [0, .546, .119]], .0013, material(colors.gold))
  for (const y of [.20, .25, .30]) ellipsoid(parent, `JacketButton_${y}`, [.152, y, .169], [.0045, .0045, .002], material(colors.gold))
  // The cut at the waist is intentional: this is a bust, not an unfinished full body.
  const hem = new THREE.CylinderGeometry(.285, .285, .025, 80)
  hem.scale(1, 1, .49); hem.translate(0, .003, 0)
  mesh(parent, 'BustHem', hem, material(colors.shirt))
}

export function createChengAvatar() {
  const avatar = new THREE.Group()
  avatar.name = 'Cheng'
  avatar.userData.avatarHarness = CHENG_MARKER
  avatar.userData.displayName = '澄 · 原创代码角色'
  avatar.userData.framing = 'bust'
  makeClothing(avatar)
  const neck = new THREE.Bone(); neck.name = 'Neck'; neck.position.y = .58; avatar.add(neck)
  const head = new THREE.Bone(); head.name = 'Head'; head.position.y = .16; neck.add(head)
  makeFace(head)
  makeEye(head, -1); makeEye(head, 1)
  makeMouth(head); makeHair(head)
  avatar.updateMatrixWorld(true)
  return avatar
}

export function isChengAvatar(model: THREE.Object3D) {
  let found = false
  model.traverse(node => { if (node.userData.avatarHarness === CHENG_MARKER) found = true })
  return found
}

export function setChengExpression(model: THREE.Object3D, name: string, value: number) {
  model.traverse(node => {
    const object = node as THREE.Mesh
    const index = object.morphTargetDictionary?.[name]
    if (index != null && object.morphTargetInfluences) object.morphTargetInfluences[index] = THREE.MathUtils.clamp(value, 0, 1)
  })
}

// Local idle motion is not speech inference and never counts as AV feedback.
export function animateChengIdle(model: THREE.Object3D, seconds: number) {
  const head = model.getObjectByName('Head')
  if (head) head.rotation.set(.025 * Math.sin(seconds * .71), .11 * Math.sin(seconds * .43), .018 * Math.sin(seconds * .57))
  const phase = seconds % 4.8
  const blink = phase > 4.35 && phase < 4.59 ? Math.sin((phase - 4.35) / .24 * Math.PI) ** 2 : 0
  setChengExpression(model, 'eyeBlinkLeft', blink)
  setChengExpression(model, 'eyeBlinkRight', blink)
}
