import test from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

test('model tunnel includes perception and reconnects after SSH drops', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'harness-tunnel-')), trace = join(dir, 'trace')
  await writeFile(join(dir, 'ssh'), '#!/bin/sh\nprintf "ATTEMPT %s\\n" "$*" >> "$AVATAR_TUNNEL_TRACE"\nif [ -f "$AVATAR_TUNNEL_TRACE.once" ]; then exec /bin/sleep 30; fi\n: > "$AVATAR_TUNNEL_TRACE.once"\nexit 255\n', { mode: 0o755 })
  const child = spawn('bash', [fileURLToPath(new URL('../../scripts/connect-data2-models', import.meta.url))], {
    detached: true, stdio: 'ignore', env: { ...process.env, PATH: `${dir}:${process.env.PATH}`, AVATAR_TUNNEL_TRACE: trace },
  })
  try {
    const deadline = Date.now() + 6000
    let calls = []
    while (Date.now() < deadline) {
      calls = (await readFile(trace, 'utf8').catch(() => '')).trim().split('\n').filter(Boolean)
      if (calls.length >= 2) break
      await new Promise(resolve => setTimeout(resolve, 50))
    }
    assert.ok(calls.length >= 2, 'SSH exited without restoring the model tunnel')
    for (const call of calls) assert.match(call, /127\.0\.0\.1:18769:127\.0\.0\.1:18769/)
  } finally {
    try { process.kill(-child.pid, 'SIGTERM') } catch (error) { if (error.code !== 'ESRCH') throw error }
    await rm(dir, { recursive: true, force: true })
  }
})
