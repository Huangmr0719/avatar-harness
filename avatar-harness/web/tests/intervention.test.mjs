import test from 'node:test'
import assert from 'node:assert/strict'
import { PracticeClock, PracticeLaunch, newerSupport, supportDefaults, canFeedback } from '../src/intervention.ts'

test('L01/L07 playback counts advancing visible playback, excludes pauses, seeks and scheduling gaps', () => {
  const clock = new PracticeClock()
  clock.update(0, 0, true, true)
  assert.equal(clock.update(100, .1, true, true), 100)
  assert.equal(clock.update(200, .1, true, true), 100)
  assert.equal(clock.update(300, .2, false, true), 100)
  clock.update(400, .3, true, false)
  clock.update(500, .4, true, true)
  assert.equal(clock.update(600, .5, true, true), 200)
  assert.equal(clock.update(5600, 5, true, true), 200)
  assert.equal(clock.update(5700, 0, true, true), 200)
})
test('L05/L06 intervention parameters exclude avatar expression intensity', () => {
  for (const mode of ['mindfulness', 'humanistic', 'cbt']) {
    const params = supportDefaults(mode)
    assert.equal('intensity' in params, false)
    assert.equal(params.duration_seconds, 60)
  }
})
test('S02 stale states cannot overwrite new state and unpresented content cannot receive feedback', () => {
  assert.equal(newerSupport({version: 7}, {version: 6}).version, 7)
  assert.equal(canFeedback({version: 1, execution: {presented: false}}), false)
  assert.equal(canFeedback({version: 2, execution: {presented: true, feedback_recorded: false}}), true)
  assert.equal(canFeedback({version: 3, execution: {presented: true, feedback_recorded: true}}), false)
})
test('tool video waits for actual speech end, applies once, and cancels with the turn', async () => {
  const launch = new PracticeLaunch()
  let release, played = 0
  const wait = () => new Promise(resolve => { release = resolve })
  launch.schedule('turn-1', 'video-1')
  const done = launch.finish('turn-1', wait, r => r.execution === 'video-1', async () => { played++ })
  assert.equal(played, 0)
  release(); await done
  assert.equal(played, 1)
  await launch.finish('turn-1', wait, () => true, async () => { played++ })
  assert.equal(played, 1)
  launch.schedule('turn-2', 'video-2')
  const cancelled = launch.finish('turn-2', wait, () => true, async () => { played++ })
  launch.cancel(); release(); await cancelled
  assert.equal(played, 1)
  launch.schedule('turn-3', 'video-3')
  const stale = launch.finish('turn-3', wait, () => false, async () => { played++ })
  release(); await stale
  assert.equal(played, 1)
})
