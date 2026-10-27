// Read-only acceptance of the deployed native service; credentials only through stdin.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const secrets = new Set();

async function main() {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const config = JSON.parse(input);
  const base = new URL(config.baseUrl);
  assert.equal(base.origin, 'http://152.136.236.238:18120');
  secrets.add(config.password);
  const output = 'D:\\A_GPT\\GPT-BackUp\\GPT-PTMOVE\\2026-10\\staging\\nexus-production-qa';
  fs.mkdirSync(output, { recursive: true });
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const issues = [];
  const screenshots = [];
  const layouts = [];
  page.on('console', message => { if (message.type() === 'error') issues.push(message.text()); });
  page.on('pageerror', error => issues.push(error.message));
  await page.route('**/*', route => {
    const request = route.request();
    const target = new URL(request.url());
    if (target.origin !== base.origin || (request.method() !== 'GET' && !(request.method() === 'POST' && target.pathname === '/api/auth/login'))) {
      issues.push('Blocked unexpected origin or mutation');
      return route.abort();
    }
    return route.continue();
  });
  try {
    await page.goto(base.origin, { waitUntil: 'networkidle' });
    const fields = page.locator('.login-card input');
    await fields.nth(0).fill(config.username || 'admin');
    await fields.nth(1).fill(config.password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.waitForURL(url => url.pathname !== '/login');
    config.password = '';
    const jwt = await page.evaluate(() => localStorage.getItem('token'));
    secrets.add(jwt);
    const get = async endpoint => {
      const response = await page.request.get(base.origin + endpoint, { headers: { Authorization: `Bearer ${jwt}` }, maxRedirects: 0 });
      assert.equal(response.status(), 200, `Read ${endpoint}`);
      return response.json();
    };
    const before = { settings: await get('/api/automation/settings'), tasks: await get('/api/automation/tasks'), runs: await get('/api/automation/runs'), keys: await get('/api/machine-keys') };
    assert.equal(before.settings.settings.enabled, false);
    assert.equal(before.settings.settings.auto_publish, false);
    assert.equal(before.settings.settings.schedule_enabled, false);
    assert.equal(before.settings.settings.source_site, 'zmpt');
    assert.equal(before.settings.settings.target_site, 'novahd');
    for (const [width, height, label] of [[1440, 1000, 'desktop'], [390, 844, 'mobile']]) {
      await page.setViewportSize({ width, height });
      for (const route of ['automation', 'site-profiles', 'ai']) {
        await page.goto(`${base.origin}/settings/${route}`, { waitUntil: 'networkidle' });
        await page.locator('.settings-content h1').waitFor();
        await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
        const layout = await page.evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth, brokenImages: [...document.images].filter(img => img.getClientRects().length && img.complete && !img.naturalWidth).map(img => new URL(img.src).pathname) }));
        assert(layout.scrollWidth <= layout.width, 'Page overflow');
        assert.deepEqual(layout.brokenImages, [], 'Broken visible images');
        layouts.push({ route, label, ...layout });
        const name = `${route}-${label}.png`;
        await page.screenshot({ path: path.join(output, name), fullPage: true, mask: [page.locator('input[type=password], textarea')] });
        screenshots.push(name);
      }
    }
    const after = { settings: await get('/api/automation/settings'), tasks: await get('/api/automation/tasks'), runs: await get('/api/automation/runs'), keys: await get('/api/machine-keys') };
    assert.deepEqual(after, before, 'Read-only browser verification mutated persistent records');
    assert.deepEqual(issues, [], 'Browser errors or unexpected traffic');
    const report = { passed: true, mode: 'production-read-only', screenshots, layouts, consoleErrors: 0, persistentStateUnchanged: true };
    fs.writeFileSync(path.join(output, 'qa-report.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({ passed: true, screenshots: screenshots.length, consoleErrors: 0, persistentStateUnchanged: true }));
  } finally {
    config.password = '';
    await browser.close();
  }
}

main().catch(error => {
  const message = [...secrets].filter(Boolean).reduce((text, value) => text.split(value).join('[redacted]'), error.message);
  process.stderr.write(`Production browser verification failed: ${message}\n`);
  process.exitCode = 1;
});
