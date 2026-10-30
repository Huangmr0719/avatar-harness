// Development-only local proxy for the browser probe. No credentials are read.
import { createServer, request } from 'node:http'
import { connect } from 'node:net'
import { readFile } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'

const built = spawnSync('node_modules/.bin/esbuild', ['tests/browser-voice-probe.ts', '--bundle', '--format=esm', '--outfile=../.cache/browser-voice-check/probe.js'], { cwd: new URL('../web/', import.meta.url), stdio: 'inherit' })
if (built.status) process.exit(built.status)
const server = createServer(async (incoming, outgoing) => {
  if (incoming.url.startsWith('/api/') || incoming.url.startsWith('/v1/')) {
    const proxy = request({ hostname: '127.0.0.1', port: 8765, method: incoming.method, path: incoming.url, headers: incoming.headers }, response => { outgoing.writeHead(response.statusCode, response.headers); response.pipe(outgoing) })
    proxy.on('error', () => { outgoing.writeHead(502); outgoing.end() }); incoming.pipe(proxy)
    outgoing.on('close', () => proxy.destroy())
    return
  }
  try {
    const script = incoming.url === '/probe.js'
    const path = new URL(script ? '../.cache/browser-voice-check/probe.js' : '../web/tests/browser-voice-probe.html', import.meta.url)
    outgoing.setHeader('Content-Type', script ? 'text/javascript' : 'text/html; charset=utf-8'); outgoing.end(await readFile(path))
  } catch { outgoing.writeHead(404); outgoing.end() }
})
server.on('upgrade', (req, client, head) => {
  const upstream = connect(8765, '127.0.0.1', () => {
    const headers = Object.entries(req.headers).map(([key, value]) => `${key}: ${value}`).join('\r\n')
    upstream.write(`${req.method} ${req.url} HTTP/1.1\r\n${headers}\r\n\r\n`)
    if (head.length) upstream.write(head)
    client.pipe(upstream); upstream.pipe(client)
  })
  upstream.on('error', () => client.destroy()); client.on('error', () => upstream.destroy())
  client.on('close', () => upstream.destroy()); upstream.on('close', () => client.destroy())
})
server.listen(8770, '127.0.0.1', () => console.log('Browser probe: http://127.0.0.1:8770/'))
