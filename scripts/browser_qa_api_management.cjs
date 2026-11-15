// Remote-only API management acceptance. Credentials arrive on stdin; no local app server.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const secrets = new Set();

async function main() {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const config = JSON.parse(input);
  input = '';
  const base = new URL(config.baseUrl);
  const readonly = config.mode === 'readonly';
  assert.equal(base.origin, readonly ? 'http://152.136.236.238:18120' : 'http://152.136.236.238:18121');
  assert(config.password);
  secrets.add(config.password);
  const output = path.resolve('D:/A_GPT/GPT-BackUp/GPT-PTMOVE/2026-11/staging/api-management-qa', readonly ? 'production' : 'canary');
  fs.mkdirSync(output, { recursive: true });
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, acceptDownloads: true });
  const page = await context.newPage();
  const errors = [], blocked = [], probes = [], shots = [], fixtures = [];
  const prefix = `qa-api-${Date.now()}`;
  let stage = 'login';
  let expectedAuthFailure = false;
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => {
    if (message.type() === 'error' && !(expectedAuthFailure && /401/.test(message.text()))) errors.push(message.text());
  });
  await context.route('**/*', route => {
    const request = route.request(), url = new URL(request.url());
    if (url.protocol === 'blob:' || url.protocol === 'data:') return route.continue();
    const method = request.method();
    const keyMutation = !readonly && ((method === 'POST' && url.pathname === '/api/machine-keys') || (method === 'DELETE' && /^\/api\/machine-keys\/[a-f0-9]+$/.test(url.pathname)));
    if (url.origin !== base.origin || !(method === 'GET' || (method === 'POST' && url.pathname === '/api/auth/login') || keyMutation)) {
      blocked.push({ origin: url.origin, path: url.pathname, method });
      return route.abort();
    }
    if (url.pathname === '/api/v1/connection') {
      assert.equal(method, 'GET');
      assert(!request.headers().cookie, 'Connection probe sent cookies');
      probes.push({ origin: url.origin, path: url.pathname, bearer: request.headers().authorization });
    }
    return route.continue();
  });
  let jwt = '';
  const admin = async (method, endpoint, data, status = 200) => {
    const response = await context.request.fetch(base.origin + endpoint, { method, data, headers: { Authorization: `Bearer ${jwt}` }, maxRedirects: 0 });
    assert.equal(response.status(), status, `${method} ${endpoint}`);
    return response.json();
  };
  const snapshot = async () => ({ rules: await admin('GET', '/api/automation/settings'), tasks: await admin('GET', '/api/automation/tasks'), runs: await admin('GET', '/api/automation/runs') });
  const screenshot = async name => {
    const file = `${name}.png`;
    await page.screenshot({ path: path.join(output, file), fullPage: true, animations: 'disabled', mask: [page.locator('input[type=password], textarea')] });
    shots.push(file);
  };
  try {
    await page.goto(base.origin + '/settings/ai', { waitUntil: 'networkidle' });
    const fields = page.locator('.login-card input');
    await fields.nth(0).fill(config.username || 'admin');
    await fields.nth(1).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.waitForURL(url => url.pathname !== '/login');
    config.password = '';
    jwt = await page.evaluate(() => localStorage.getItem('token'));
    secrets.add(jwt);
    await page.goto(base.origin + '/settings/ai', { waitUntil: 'networkidle' });
    await page.waitForURL(url => url.pathname === '/settings/api');
    await page.getByRole('heading', { name: 'API 管理', exact: true }).waitFor();
    await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
    const before = await snapshot();
    const originalKeys = await admin('GET', '/api/machine-keys');

    const download = async label => {
      const promise = page.waitForEvent('download');
      await page.getByRole('button', { name: label, exact: true }).click();
      const result = await promise;
      const stream = await result.createReadStream();
      let body = '';
      for await (const chunk of stream) body += chunk;
      for (const secret of secrets) if (secret) assert(!body.includes(secret), 'Credential appeared in download');
      return JSON.parse(body);
    };
    const pack = await download('下载 AI 接入包');
    assert(JSON.stringify(pack).includes('PTMOVE_API_KEY'));
    const contract = await download('下载机器 API OpenAPI JSON');
    assert(contract.paths['/api/v1/connection']);
    const operations = Object.values(contract.paths).flatMap(item => Object.values(item).map(op => op.operationId));
    assert(operations.every(Boolean) && operations.length === new Set(operations).size, 'Missing/duplicate operation IDs');

    if (!readonly) {
      await page.getByRole('button', { name: '创建密钥', exact: true }).first().click();
      const dialog = page.getByRole('dialog', { name: '创建机器密钥', exact: true });
      await dialog.waitFor();
      stage = 'create-key';
      await dialog.locator('input[type=text]').first().fill(prefix + '-read');
      await dialog.getByRole('button', { name: '创建密钥', exact: true }).click();
      const issued = page.getByRole('dialog', { name: '新机器密钥', exact: true });
      await issued.waitFor();
      const token = await issued.getByLabel('新创建的机器密钥，仅显示一次').inputValue();
      assert(/^ptn_[a-f0-9]{64}$/.test(token));
      secrets.add(token);
      const keys = await admin('GET', '/api/machine-keys');
      const created = keys.keys.find(key => key.name === prefix + '-read');
      assert.deepEqual(created.scopes, ['read']);
      fixtures.push(created.id);
      await issued.getByRole('button', { name: '只读测试此密钥', exact: true }).click();
      await issued.getByText('只读连接验证成功', { exact: true }).waitFor();
      await screenshot('issued-readonly-key');
      await issued.getByRole('button', { name: '保存并关闭', exact: true }).click();
      await issued.waitFor({ state: 'hidden' });
      await page.waitForFunction(() => [...document.querySelectorAll('input[aria-label="新创建的机器密钥，仅显示一次"]')].every(input => input.value === ''));
      assert.equal(await page.evaluate(value => JSON.stringify(localStorage).includes(value) || JSON.stringify(sessionStorage).includes(value), token), false);

      const guideBase = page.locator('.api-base-form input');
      await guideBase.fill('https://example.invalid/api/v1');
      const probeInput = page.getByLabel('用于只读连接测试的临时机器密钥');
      await probeInput.fill(token);
      await page.getByRole('button', { name: '验证只读连接', exact: true }).click();
      await page.locator('.probe-result').getByText('连接成功', { exact: true }).waitFor();
      assert.equal(probes.at(-1).origin, base.origin);
      assert.equal(probes.at(-1).bearer, `Bearer ${token}`);
      await page.getByRole('button', { name: '恢复当前地址', exact: true }).click();
      await screenshot('connected-readonly');

      stage = 'expiry-and-revoke';
      const expired = await admin('POST', '/api/machine-keys', { name: prefix + '-expiry', scopes: ['read'], expires_at: new Date(Date.now() + 1600).toISOString() }, 201);
      fixtures.push(expired.key.id);
      secrets.add(expired.token);
      await new Promise(resolve => setTimeout(resolve, 2200));
      await page.reload({ waitUntil: 'networkidle' });
      await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
      const expiredRow = page.getByRole('row').filter({ hasText: prefix + '-expiry' });
      await expiredRow.getByText('已到期', { exact: true }).waitFor();
      await admin('DELETE', '/api/machine-keys/' + created.id);
      expectedAuthFailure = true;
      await probeInput.fill(token);
      await page.getByRole('button', { name: '验证只读连接', exact: true }).click();
      await page.getByText('密钥无效、已到期或已撤销。', { exact: true }).waitFor();
      expectedAuthFailure = false;
      stage = 'clear-probe';
      await page.getByRole('button', { name: '清除', exact: true }).click();
      assert.equal(await probeInput.inputValue(), '');
      await page.reload({ waitUntil: 'networkidle' });
      await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
      await screenshot('expired-and-revoked');
    }

    stage = 'independent-auth-failure';
    expectedAuthFailure = true;
    await page.getByLabel('用于只读连接测试的临时机器密钥').fill('invalid-machine-key');
    await page.getByRole('button', { name: '验证只读连接', exact: true }).click();
    await page.getByText('密钥无效、已到期或已撤销。', { exact: true }).waitFor();
    assert.equal(new URL(page.url()).pathname, '/settings/api', 'Machine key rejection logged out the administrator');
    await page.getByRole('button', { name: '清除', exact: true }).click();
    expectedAuthFailure = false;

    for (const [width, height, label] of [[1440, 1100, 'desktop'], [390, 844, 'mobile']]) {
      stage = 'layout-' + label;
      await page.setViewportSize({ width, height });
      await page.goto(base.origin + '/settings/api', { waitUntil: 'networkidle' });
      await page.getByRole('heading', { name: 'API 管理', exact: true }).waitFor();
      await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
      const layout = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth }));
      assert(layout.scroll <= layout.width, 'Horizontal page overflow');
      await screenshot('api-' + label);
      await page.goto(base.origin + '/settings/downloader', { waitUntil: 'networkidle' });
      if (!readonly) {
        await page.getByRole('button', { name: '添加下载器', exact: true }).click();
        const card = page.locator('.downloader-card').last();
        await card.locator('.el-switch').filter({ hasText: '本地' }).click();
        const mediaOnly = card.getByRole('checkbox', { name: '仅媒体代理（NAS Docker 程序）', exact: true });
        assert(await mediaOnly.isChecked());
        await card.getByPlaceholder('留空保留已配置 Token；NAS 程序至少 32 字符').fill('synthetic-media-token-for-ui-only');
        const mediaLabel = card.getByText('仅媒体代理（NAS Docker 程序）', { exact: true });
        await mediaLabel.click();
        assert.equal(await mediaOnly.isChecked(), false);
        await mediaLabel.click();
        assert.equal(await mediaOnly.isChecked(), true);
        await card.scrollIntoViewIfNeeded();
        const proxyLayout = await page.evaluate(() => ({ width: innerWidth, scroll: document.documentElement.scrollWidth }));
        assert(proxyLayout.scroll <= proxyLayout.width, 'Downloader form horizontal overflow');
      }
      await screenshot('downloader-' + label);
    }
    assert.deepEqual(await snapshot(), before, 'API setup/probe changed business records');
    if (readonly) assert.deepEqual(await admin('GET', '/api/machine-keys'), originalKeys);
    assert.deepEqual(blocked, [], 'Unexpected network or mutation');
    assert.deepEqual(errors, [], 'Browser errors');
    stage = 'browser-admin-expiry';
    expectedAuthFailure = true;
    await page.evaluate(async () => { await fetch('/api/machine-keys', { headers: { Authorization: 'Bearer invalid-browser-session' } }); });
    await page.waitForURL(url => url.pathname === '/login');
    expectedAuthFailure = false;
    const report = { passed: true, mode: config.mode, screenshots: shots, probes: probes.length, externalRequests: blocked.length, consoleErrors: errors.length, businessStateUnchanged: true };
    fs.writeFileSync(path.join(output, 'qa-report.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report));
  } catch (error) {
    await screenshot('failure').catch(() => {});
    const diagnostic = await page.evaluate(() => ({ route: location.pathname, headings: [...document.querySelectorAll('h1')].map(node => node.textContent), dialogs: [...document.querySelectorAll('[role=dialog]')].filter(node => node.getClientRects().length).map(node => node.getAttribute('aria-label')), loading: [...document.querySelectorAll('.el-loading-mask')].filter(node => node.getClientRects().length).length }));
    fs.writeFileSync(path.join(output, 'failure.json'), JSON.stringify({ stage, ...diagnostic }, null, 2));
    throw error;
  } finally {
    for (const id of fixtures) await admin('DELETE', '/api/machine-keys/' + id).catch(() => {});
    config.password = '';
    probes.forEach(probe => { delete probe.bearer; });
    await browser.close();
  }
}

main().catch(error => {
  const message = [...secrets].filter(Boolean).reduce((text, secret) => text.split(secret).join('[redacted]'), error.message);
  process.stderr.write(`API management QA failed: ${message}\n`);
  process.exitCode = 1;
});
