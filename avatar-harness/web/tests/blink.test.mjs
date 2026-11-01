import test from 'node:test'
import assert from 'node:assert/strict'
import { Blink } from '../src/blink.ts'

const advance = (blink, from, to) => { for (let t = from; t < to; t += 100) blink.sample(t) }

test('blinks use avatar-relative varied intervals, with fast closure and slower reopening', () => {
  const values = [0, 1, .5], blink = new Blink(() => values.shift() ?? .5)
  assert.equal(blink.sample(90000), 0)
  advance(blink, 90100, 92600)
  assert.equal(blink.sample(92600), 0)
  assert.ok(blink.sample(92632) > .4)
  assert.equal(blink.sample(92665), 1)
  assert.equal(blink.sample(92700), 1)
  assert.ok(blink.sample(92765) > .5)
  assert.equal(blink.sample(92840), 0)
  // The next interval uses the next draw (6600 ms), not the first 2600 ms.
  advance(blink, 92900, 99440)
  assert.equal(blink.sample(99440), 0)
  assert.equal(blink.sample(99510), 1)
})

test('returning from a background tab schedules afresh instead of replaying a missed blink', () => {
  const blink = new Blink(() => 0)
  blink.sample(0)
  assert.equal(blink.sample(10000), 0)
  advance(blink, 10100, 12600)
  blink.sample(12600)
  assert.equal(blink.sample(12670), 1)
})

test('an observed model blink delays the procedural blink without adding another closure', () => {
  const blink = new Blink(() => 0)
  advance(blink, 0, 2500)
  assert.equal(blink.sample(2500, 1), 0)
  assert.equal(blink.sample(2550, 1), 0)
  advance(blink, 2600, 5100)
  blink.sample(5100)
  assert.equal(blink.sample(5170), 1)
})
