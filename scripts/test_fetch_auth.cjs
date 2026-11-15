const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const { stripTypeScriptTypes } = require('node:module')
const test = require('node:test')

// Execute the production wrapper and its actual helpers without booting Vue or a server.
// Keep the boundary markers explicit so moving/removing the wrapper fails this harness.
const source = fs.readFileSync(path.join(__dirname, '../nexus/webui/src/main.ts'), 'utf8')
const start = source.indexOf('const uint8ArrayToBase64 =')
const marker = source.indexOf('// fetch authentication wrapper (also exercised by scripts/test_fetch_auth.cjs).')
const endMarker = '// End fetch authentication wrapper.'
const end = source.indexOf(endMarker)
assert.ok(start >= 0 && marker > start && end > marker, 'production fetch wrapper markers exist')
const productionCode = stripTypeScriptTypes(source.slice(start, end + endMarker.length))
const origin = 'https://ptmove.example.test'
const token = 'synthetic-admin-jwt'

function harness({ desktop = false, status = 401 } = {}) {
  const nativeCalls = []
  const desktopCalls = []
  const redirects = []
  let tokenReads = 0
  class BrowserRequest extends Request {
    constructor(input, init) {
      super(typeof input === 'string' ? new URL(input, origin) : input, init)
    }
  }
  const window = {
    location: { origin, href: `${origin}/settings/api` },
    fetch: async (input, init) => {
      nativeCalls.push({ input, init, request: new BrowserRequest(input, init) })
      return new Response('', { status })
    },
  }
  const bridge = {
    DesktopRequest: async (request) => {
      desktopCalls.push(request)
      return { status, headers: {}, bodyBase64: '' }
    },
  }
  vm.runInNewContext(productionCode, {
    window,
    localStorage: {
      getItem: (key) => {
        assert.equal(key, 'token')
        tokenReads += 1
        return token
      },
    },
    router: {
      currentRoute: { value: { fullPath: '/settings/api' } },
      replace: (url) => redirects.push(url),
    },
    getDesktopBridge: () => (desktop ? bridge : null),
    URL,
    Headers,
    Request: BrowserRequest,
    Response,
    Uint8Array,
    ArrayBuffer,
    atob,
    btoa,
  })
  return {
    fetch: window.fetch,
    nativeCalls,
    desktopCalls,
    redirects,
    tokenReads: () => tokenReads,
  }
}

for (const desktop of [false, true]) {
  for (const input of [
    '/api/v1/connection',
    `${origin}/api/v1/connection?probe=1`,
    new URL(`${origin}/api/v1/connection`),
    '/api/v1',
    '/api/v1/',
  ]) {
    test(`machine fetch preserves native options (${desktop ? 'desktop' : 'browser'}, ${input})`, async () => {
      const h = harness({ desktop })
      const init = {
        headers: { Authorization: 'Bearer synthetic-machine-key' },
        credentials: 'omit',
        redirect: 'error',
        cache: 'no-store',
      }
      const response = await h.fetch(input, init)
      assert.equal(response.status, 401)
      assert.equal(h.nativeCalls.length, 1)
      assert.equal(h.nativeCalls[0].input, input)
      assert.equal(h.nativeCalls[0].init, init)
      assert.equal(h.nativeCalls[0].request.headers.get('Authorization'), 'Bearer synthetic-machine-key')
      assert.equal(h.nativeCalls[0].request.credentials, 'omit')
      assert.equal(h.nativeCalls[0].request.redirect, 'error')
      assert.equal(h.tokenReads(), 0)
      assert.equal(h.desktopCalls.length, 0)
      assert.deepEqual(h.redirects, [])
    })
  }

  test(`machine Request preserves headers and options (${desktop ? 'desktop' : 'browser'})`, async () => {
    const h = harness({ desktop })
    const input = new Request(`${origin}/api/v1/connection`, {
      headers: { Authorization: 'Bearer synthetic-machine-key' },
      credentials: 'omit',
      redirect: 'error',
    })
    await h.fetch(input)
    assert.equal(h.nativeCalls[0].input, input)
    assert.equal(h.nativeCalls[0].init, undefined)
    assert.equal(h.nativeCalls[0].request.headers.get('Authorization'), 'Bearer synthetic-machine-key')
    assert.equal(h.nativeCalls[0].request.credentials, 'omit')
    assert.equal(h.nativeCalls[0].request.redirect, 'error')
    assert.equal(h.tokenReads(), 0)
    assert.equal(h.desktopCalls.length, 0)
    assert.deepEqual(h.redirects, [])
  })

  test(`machine fetch without Bearer never borrows JWT (${desktop ? 'desktop' : 'browser'})`, async () => {
    const h = harness({ desktop })
    await h.fetch('/api/v1/connection', { credentials: 'omit', redirect: 'error' })
    assert.equal(h.nativeCalls[0].request.headers.has('Authorization'), false)
    assert.equal(h.tokenReads(), 0)
    assert.equal(h.desktopCalls.length, 0)
    assert.deepEqual(h.redirects, [])
  })
}

for (const desktop of [false, true]) {
  for (const input of ['/api/machine-keys', `${origin}/api/machine-keys`, new Request(`${origin}/api/machine-keys`)]) {
    test(`admin fetch retains JWT and 401 redirect (${desktop ? 'desktop' : 'browser'}, ${input})`, async () => {
      const h = harness({ desktop })
      await h.fetch(input)
      const headers = h.desktopCalls.length
        ? new Headers(h.desktopCalls[0].headers)
        : h.nativeCalls[0].request.headers
      assert.equal(headers.get('Authorization'), `Bearer ${token}`)
      assert.deepEqual(h.redirects, ['/login?redirect=%2Fsettings%2Fapi'])
      assert.equal(h.desktopCalls.length, desktop && typeof input === 'string' && input.startsWith('/') ? 1 : 0)
    })
  }
}

for (const input of [
  'https://foreign.example.test/api/machine-keys',
  '//foreign.example.test/api/v1/connection',
  '/api-other/connection',
  '/health',
]) {
  test(`unrelated fetch neither borrows JWT nor redirects (${input})`, async () => {
    const h = harness()
    await h.fetch(input)
    assert.equal(h.nativeCalls[0].request.headers.has('Authorization'), false)
    assert.deepEqual(h.redirects, [])
  })
}

for (const desktop of [false, true]) {
  for (const input of ['/api/auth/login', `${origin}/api/auth/login`, '/api/auth']) {
    test(`authentication endpoint 401 does not redirect (${desktop ? 'desktop' : 'browser'}, ${input})`, async () => {
      const h = harness({ desktop })
      await h.fetch(input)
      assert.deepEqual(h.redirects, [])
    })
  }
}

test('successful admin response does not redirect and explicit authorization is retained', async () => {
  const h = harness({ status: 200 })
  await h.fetch('/api/machine-keys', { headers: { Authorization: 'Bearer explicit-admin-key' } })
  assert.equal(h.nativeCalls[0].request.headers.get('Authorization'), 'Bearer explicit-admin-key')
  assert.deepEqual(h.redirects, [])
})
