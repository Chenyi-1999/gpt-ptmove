// Run only against the disposable Nexus canary on 152:18121. Never pass secrets
// on the command line or write them to screenshots/reports.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

const sensitiveValues = new Set();
const OUTPUT = 'D:\\A_GPT\\GPT-BackUp\\GPT-PTMOVE\\2026-10\\staging\\nexus-browser-qa';
const redact = message => [...sensitiveValues].reduce((text, secret) => text.split(secret).join('[redacted]'), String(message)).replace(/ptn_[A-Za-z0-9_-]+/g, '[redacted]');

async function readStdinJson() {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  assert(input.trim(), 'Provide {baseUrl, username, password} as JSON on stdin');
  return JSON.parse(input);
}

function assertCanary(url) {
  const parsed = new URL(url);
  assert(['http:', 'https:'].includes(parsed.protocol), 'QA_BASE_URL must use HTTP(S)');
  assert.equal(parsed.hostname, '152.136.236.238', 'Only the approved remote canary host is allowed');
  assert.equal(parsed.port, '18121', 'Only canary port 18121 is allowed');
  assert(!parsed.username && !parsed.password, 'Do not embed credentials in QA_BASE_URL');
  return parsed.origin;
}

async function main() {
  const config = await readStdinJson();
  if (config.password) sensitiveValues.add(String(config.password));
  const base = assertCanary(config.baseUrl);
  assert(typeof config.password === 'string' && config.password.length, 'stdin JSON must contain password');
  const username = String(config.username || 'admin');
  fs.mkdirSync(OUTPUT, { recursive: true });

  const browser = await chromium.launch({ headless: true, channel: process.env.QA_BROWSER_CHANNEL || 'msedge' });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, acceptDownloads: true });
  // A UI regression must never turn this disposable test into a tracker/NAS request.
  const blockedOrigins = new Set();
  await page.context().route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.origin === base || ['data:', 'blob:'].includes(url.protocol)) return route.continue();
    blockedOrigins.add(url.origin);
    return route.abort('blockedbyclient');
  });
  const errors = [];
  const requestFailures = [];
  const failedResponses = [];
  const publicRequests = [];
  let recordingPublicRequests = true;
  page.on('requestfailed', request => requestFailures.push({ path: new URL(request.url()).pathname, error: request.failure()?.errorText }));
  page.on('response', response => { if (response.status() >= 400) failedResponses.push({ path: new URL(response.url()).pathname, status: response.status() }); });
  page.on('request', request => { if (recordingPublicRequests) publicRequests.push(new URL(request.url()).pathname); });
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  page.on('dialog', dialog => dialog.accept());

  let stage = 'login';
  const screenshots = [];
  const layout = [];
  const shot = async name => {
    await page.screenshot({ path: path.join(OUTPUT, `${name}.png`), fullPage: true, animations: 'disabled',
      mask: [page.locator('input[type="password"], input[autocomplete="new-password"], textarea')] });
    screenshots.push(`${name}.png`);
  };
  const failureEvidence = async error => {
    await shot('failure').catch(() => {});
    const dom = await page.evaluate(() => {
      const visible = node => !!node.getClientRects().length && getComputedStyle(node).visibility !== 'hidden';
      return {
        route: location.pathname,
        headings: [...document.querySelectorAll('h1')].filter(visible).map(node => node.textContent),
        loading: [...document.querySelectorAll('.el-loading-mask')].filter(visible).length,
        selects: [...document.querySelectorAll('.el-select')].map(node => ({
          label: node.closest('.el-form-item')?.querySelector('.el-form-item__label')?.textContent,
          expanded: node.querySelector('[role="combobox"]')?.getAttribute('aria-expanded'),
          rect: { x: node.getBoundingClientRect().x, y: node.getBoundingClientRect().y, width: node.getBoundingClientRect().width, height: node.getBoundingClientRect().height },
        })),
        options: [...document.querySelectorAll('.el-select-dropdown__item')].filter(visible).slice(0, 12).map(node => ({ text: node.textContent, selected: node.getAttribute('aria-selected') })),
        dialogs: [...document.querySelectorAll('[role="dialog"]')].filter(visible).map(node => node.getAttribute('aria-label')),
        images: [...document.images].filter(visible).map(node => ({ src: new URL(node.src).pathname, complete: node.complete, width: node.naturalWidth })),
      };
    }).catch(() => ({}));
    fs.writeFileSync(path.join(OUTPUT, 'qa-failure.json'), redact(JSON.stringify({ passed: false, stage, error: error.message, dom, layout, screenshots, consoleErrors: errors, blockedOrigins: [...blockedOrigins], requestFailures, failedResponses }, null, 2)));
  };
  let adminToken = '';
  const adminGet = async route => {
    const target = new URL(route, base);
    assert.equal(target.origin, base, 'Admin API requests must stay on the canary origin');
    return page.request.get(target.href, { headers: { Authorization: `Bearer ${adminToken}` } });
  };
  const navigateSettings = async route => {
    await page.goto(`${base}/settings/${route}`, { waitUntil: 'networkidle' });
    await page.locator('.settings-content').waitFor({ state: 'visible' });
    await page.locator('.settings-content h1').first().waitFor({ state: 'visible' });
    await page.locator('.settings-content .el-loading-mask:visible').waitFor({ state: 'hidden' });
  };
  const selectSite = async (label, site) => {
    const field = page.locator('.settings-content .el-form-item').filter({ hasText: label });
    const input = field.getByRole('combobox');
    await input.click();
    const listboxId = await input.getAttribute('aria-controls');
    assert(listboxId, `${label} should identify its own listbox`);
    const listbox = page.locator(`[id="${listboxId}"]`);
    await listbox.waitFor({ state: 'visible' });
    await listbox.getByRole('option').filter({ hasText: `(${site.id})` }).click();
    await listbox.waitFor({ state: 'hidden' });
    assert.equal(await input.getAttribute('aria-expanded'), 'false', `${label} should close after selection`);
    assert((await field.innerText()).includes(site.id), `${label} should show the selected site`);
  };
  const checkLayout = async (route, width, height) => {
    await page.setViewportSize({ width, height });
    await navigateSettings(route);
    const result = await page.evaluate(() => {
      const root = document.documentElement;
      const content = document.querySelector('.settings-content');
      const missingLabels = [...(content || document).querySelectorAll('input,textarea,select')]
        .filter(node => {
          if (!node.getClientRects().length || node.type === 'hidden') return false;
          return !node.getAttribute('aria-label') && !node.labels?.length &&
            !node.closest('.el-form-item')?.querySelector('.el-form-item__label');
        })
        .map(node => ({ id: node.id, type: node.type, role: node.getAttribute('role'), context: node.closest('.card-head, .el-form-item, .head')?.textContent?.trim().slice(0, 80) || '' }));
      const clippedPanels = [...(content?.children || [])]
        .filter(node => node.matches('.el-card, .el-alert') && node.scrollHeight > node.clientHeight + 2 && ['hidden', 'clip'].includes(getComputedStyle(node).overflowY))
        .map(node => ({ tag: node.classList.contains('el-card') ? 'card' : 'alert', clientHeight: node.clientHeight, scrollHeight: node.scrollHeight }));
      return { width: innerWidth, scrollWidth: root.scrollWidth, missingLabels, clippedPanels };
    });
    return result;
  };

  try {
    await page.goto(base, { waitUntil: 'networkidle' });
    assert(page.url().includes('/login'), 'Unauthenticated home should show the login page');
    assert(!publicRequests.some(route => ['/api/settings', '/api/migrate/task_monitor'].includes(route)), 'Login must not request protected settings/task monitor');
    assert((await page.locator('#app').evaluate(node => getComputedStyle(node).backgroundImage)).includes('/backgrounds/wallpaper_1744788002_8050.jpg'), 'Login background should use the bundled local image');
    const loginInputs = page.locator('.login-card input');
    await loginInputs.nth(0).fill(username);
    await loginInputs.nth(1).fill(config.password);
    recordingPublicRequests = false;
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.waitForURL(url => !url.pathname.endsWith('/login'));
    await page.locator('.main-nav').waitFor({ state: 'visible' });
    adminToken = await page.evaluate(() => localStorage.getItem('token') || '');
    if (adminToken) sensitiveValues.add(adminToken);
    assert(adminToken, 'Login should store an administrator JWT for authenticated API checks');
    // Drop the submitted password reference as soon as login completes.
    config.password = '';

    // Existing manual cross-seed entry points remain available.
    for (const [href, label] of [['/torrents', '一种多站'], ['/data', '一站多种']]) {
      const link = page.locator(`.main-nav a[href="${href}"]`);
      if (await link.count()) assert((await link.innerText()).includes(label), `Missing existing navigation ${label}`);
      else assert((await page.locator('.main-nav').innerText()).includes(label), `Missing existing navigation ${label}`);
    }
    await page.goto(`${base}/torrents`, { waitUntil: 'networkidle' });
    await page.locator('.main-nav').waitFor({ state: 'visible' });
    await shot('manual-cross-seed-desktop');

    const profilesResponse = await adminGet('/api/site-profiles');
    assert.equal(profilesResponse.status(), 200, 'Read configured site profiles');
    const profileData = await profilesResponse.json();
    const profiles = profileData.sites || [];
    assert(profiles.length >= 2, 'Canary needs two configured sites to save a disabled automation rule');
    const source = profiles.find(profile => (profile.roles || []).includes('source')) || profiles[0];
    const target = profiles.find(profile => profile.id !== source.id && (profile.roles || []).includes('target')) || profiles.find(profile => profile.id !== source.id);
    assert(target, 'Canary needs distinct source and target site profiles');

    stage = 'automation-source-selection';
    await navigateSettings('automation');
    const automationState = await adminGet('/api/automation/settings');
    assert.equal(automationState.status(), 200);
    assert.equal((await automationState.json()).settings.enabled, false, 'Automation module should default to disabled');
    const moduleSwitch = page.locator('.settings-content .el-switch').first();
    assert((await moduleSwitch.getAttribute('aria-checked')) !== 'true', 'Automation UI should initially show disabled');
    await selectSite('源站标识', source);
    stage = 'automation-target-selection';
    await selectSite('目标站标识', target);
    await page.getByRole('button', { name: '保存规则', exact: true }).click();
    await page.getByText('规则已保存', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
    const savedRule = await adminGet('/api/automation/settings');
    assert.equal((await savedRule.json()).settings.enabled, false, 'Saved automation rule must remain disabled');
    await shot('automation-desktop');

    stage = 'site-profile-import';
    await navigateSettings('site-profiles');
    await page.getByRole('button', { name: '导入配置', exact: true }).click();
    const editor = page.getByRole('dialog', { name: '导入配置' });
    const jsonEditor = editor.getByRole('textbox', { name: '站点配置 JSON' });

    // Exercise the visible client-side validation path without an API mutation.
    await jsonEditor.fill('{invalid');
    await editor.getByRole('button', { name: '校验并预览', exact: true }).click();
    const validationToast = page.locator('.el-message').filter({ hasText: 'JSON' }).first();
    await validationToast.waitFor({ state: 'visible', timeout: 5000 });
    assert((await validationToast.innerText()).length > 0, 'Invalid JSON error should be visible');
    await jsonEditor.fill('');

    const templateResponse = await adminGet('/api/site-profiles/template');
    assert.equal(templateResponse.status(), 200);
    const fixture = await templateResponse.json();
    const suffix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
    fixture.id = `qa-${suffix}`;
    fixture.name = `Nexus QA ${suffix}`;
    fixture.base_url = `https://${fixture.id}.example.com`;
    fixture.roles = ['source', 'target'];
    fixture.announce_hosts = [`${fixture.id}.example.com`];
    fixture.cookie = undefined;
    fixture.rss = undefined;
    fixture.rss_url = undefined;
    await editor.locator('input[type=file]').setInputFiles({
      name: 'qa-site.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(fixture, null, 2)),
    });
    await editor.getByRole('button', { name: '校验并预览', exact: true }).click();
    await editor.locator('.preview').waitFor({ state: 'visible' });
    await editor.getByRole('button', { name: '导入配置', exact: true }).click();
    await page.getByText(fixture.name, { exact: true }).waitFor({ state: 'visible', timeout: 10000 });

    const profileRow = page.locator('.el-table__row').filter({ hasText: fixture.name });
    const download = page.waitForEvent('download');
    await profileRow.getByRole('button', { name: '导出', exact: true }).click();
    const profileDownload = await download;
    const exportedPath = path.join(OUTPUT, 'qa-site-export.json');
    await profileDownload.saveAs(exportedPath);
    const exported = JSON.parse(fs.readFileSync(exportedPath, 'utf8'));
    assert.equal(exported.id, fixture.id);
    assert(!Object.keys(exported).some(key => /cookie|rss(_url)?|passkey|secret|token/i.test(key)), 'Export must not contain credentials');

    // Exercise the optimistic-concurrency edit flow using the table revision,
    // including the explicit existing-profile update acknowledgement.
    await profileRow.getByRole('button', { name: '编辑', exact: true }).click();
    stage = 'site-profile-update';
    const editDialog = page.getByRole('dialog', { name: '检查配置' });
    const editJson = editDialog.getByRole('textbox', { name: '站点配置 JSON' });
    const editedProfile = JSON.parse(await editJson.inputValue());
    const updatedName = `${fixture.name} updated`;
    editedProfile.name = updatedName;
    await editJson.fill(JSON.stringify(editedProfile, null, 2));
    // Element Plus visually hides its native input; exercise the rendered label.
    const updateCheckbox = editDialog.getByRole('checkbox', { name: '明确更新现有配置' });
    await editDialog.locator('label.el-checkbox').filter({ hasText: '明确更新现有配置' }).click();
    assert.equal(await updateCheckbox.isChecked(), true, 'Existing-profile update needs explicit checked acknowledgement');
    await editDialog.getByRole('button', { name: '校验并预览', exact: true }).click();
    await editDialog.locator('.preview').waitFor({ state: 'visible' });
    await editDialog.getByRole('button', { name: '确认更新', exact: true }).click();
    await page.getByText(updatedName, { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
    assert.equal(await page.getByText(fixture.name, { exact: true }).count(), 0, 'Updated site name should replace the prior row value');
    await shot('site-profiles-desktop');

    stage = 'machine-key-create';
    await navigateSettings('ai');
    await page.getByRole('button', { name: '创建密钥', exact: true }).click();
    const createDialog = page.getByRole('dialog', { name: '创建机器密钥' });
    await createDialog.getByRole('textbox').first().fill(`Nexus QA ${suffix}`);
    const createdResponse = page.waitForResponse(response => response.url().endsWith('/api/machine-keys') && response.request().method() === 'POST');
    await createDialog.getByRole('button', { name: '创建密钥', exact: true }).click();
    const createResult = await (await createdResponse).json();
    const token = createResult.token;
    if (token) sensitiveValues.add(token);
    assert(typeof token === 'string' && token.startsWith('ptn_'), 'Canary should issue a machine key');
    const keyDialog = page.getByRole('dialog', { name: '复制机器密钥' });
    await keyDialog.getByRole('textbox', { name: '新创建的机器密钥，仅显示一次' }).waitFor({ state: 'visible' });
    assert.equal(await keyDialog.getByRole('textbox', { name: '新创建的机器密钥，仅显示一次' }).inputValue(), token);
    const keyId = createResult.key?.id || createResult.id;
    assert(keyId, 'Create response must identify the key for revocation');
    // Close clears the one-time token from the rendered UI before screenshots.
    await keyDialog.getByRole('button', { name: '已妥善保存并关闭', exact: true }).click();
    await keyDialog.waitFor({ state: 'hidden' });
    assert.equal(await page.locator('textarea').evaluateAll(nodes => nodes.some(node => node.value && node.value.startsWith('ptn_'))), false, 'Token must be hidden before screenshots');

    stage = 'openapi-download';
    const openApiDownload = page.waitForEvent('download');
    await page.getByRole('button', { name: '下载带 JWT 认证的 OpenAPI 文档', exact: true }).click();
    const openApiFile = await openApiDownload;
    const openApiPath = path.join(OUTPUT, 'ptnexus-machine-openapi.json');
    await openApiFile.saveAs(openApiPath);
    const openApi = JSON.parse(fs.readFileSync(openApiPath, 'utf8'));
    assert(Object.keys(openApi.paths || {}).length > 0, 'OpenAPI download should contain paths');
    assert((await page.getByRole('textbox', { name: '可复制的中文 AI API 调用说明' }).inputValue()).includes('/api/v1'), 'AI guide should render machine API instructions');

    stage = 'machine-key-revoke';
    const keyRow = page.locator('.el-table__row').filter({ hasText: `Nexus QA ${suffix}` });
    await keyRow.getByRole('button', { name: '撤销', exact: true }).click();
    await page.getByRole('button', { name: '撤销密钥', exact: true }).click();
    await keyRow.getByText('已撤销', { exact: true }).waitFor({ state: 'visible', timeout: 10000 });
    const revoked = await page.request.get(`${base}/api/v1/dashboard`, { headers: { Authorization: `Bearer ${token}` } });
    assert.equal(revoked.status(), 401, 'Revoked machine key should no longer authenticate');
    assert(!JSON.stringify(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).includes(token), 'Machine key must not be stored in browser storage');
    await shot('ai-desktop');

    for (const [route, name] of [['automation', 'automation'], ['site-profiles', 'site-profiles'], ['ai', 'ai']]) {
      stage = `layout-${route}`;
      layout.push({ route, desktop: await checkLayout(route, 1440, 1000) });
      await shot(`${name}-desktop-layout`);
      layout.push({ route, mobile: await checkLayout(route, 390, 844) });
      await shot(`${name}-mobile`);
      if (route === 'automation') {
        await page.getByRole('button', { name: '保存规则', exact: true }).scrollIntoViewIfNeeded();
        await shot('automation-mobile-actions');
      }
      if (route === 'site-profiles') {
        await page.getByRole('button', { name: '导入配置', exact: true }).click();
        const mobileImport = page.getByRole('dialog', { name: '导入配置' });
        await mobileImport.getByRole('textbox', { name: '站点配置 JSON' }).waitFor({ state: 'visible' });
        await mobileImport.getByRole('button', { name: '校验并预览', exact: true }).scrollIntoViewIfNeeded();
        await shot('site-profiles-mobile-import');
        await mobileImport.getByRole('button', { name: '取消', exact: true }).click();
        await mobileImport.waitFor({ state: 'hidden' });
      }
      if (route === 'ai') {
        await page.getByRole('button', { name: '下载带 JWT 认证的 OpenAPI 文档', exact: true }).scrollIntoViewIfNeeded();
        await shot('ai-mobile-docs');
        await page.getByRole('button', { name: '创建密钥', exact: true }).click();
        const mobileKey = page.getByRole('dialog', { name: '创建机器密钥' });
        await mobileKey.waitFor({ state: 'visible' });
        await shot('ai-mobile-key-dialog');
        await mobileKey.getByRole('button', { name: '取消', exact: true }).click();
        await mobileKey.waitFor({ state: 'hidden' });
      }
    }
    stage = 'mobile-navigation';
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(`${base}/torrents`, { waitUntil: 'networkidle' });
    await page.getByRole('button', { name: '菜单', exact: true }).waitFor({ state: 'visible' });
    await page.getByRole('button', { name: '菜单', exact: true }).click();
    const mobileMenu = page.locator('.mobile-nav-menu');
    await mobileMenu.waitFor({ state: 'visible' });
    assert((await mobileMenu.innerText()).includes('一种多站'), 'Mobile navigation retains manual cross-seed route');
    assert((await mobileMenu.innerText()).includes('一站多种'), 'Mobile navigation retains manual one-site-many-torrents route');
    await shot('manual-cross-seed-mobile');

    const images = await page.locator('img').evaluateAll(nodes => nodes.map(node => ({ src: new URL(node.src).pathname, complete: node.complete, width: node.naturalWidth })));
    fs.writeFileSync(path.join(OUTPUT, 'qa-network.json'), redact(JSON.stringify({ requestFailures, failedResponses, images, publicRequests }, null, 2)));
    stage = 'final-layout-validation';
    const layoutFindings = layout.flatMap(entry => {
      const viewport = entry.desktop || entry.mobile;
      return [
        ...(viewport.scrollWidth > viewport.width ? [{ route: entry.route, width: viewport.width, scrollWidth: viewport.scrollWidth }] : []),
        ...viewport.missingLabels.map(control => ({ route: entry.route, width: viewport.width, unlabeled: control })),
        ...viewport.clippedPanels.map(panel => ({ route: entry.route, width: viewport.width, clipped: panel })),
      ];
    });
    assert.deepEqual(layoutFindings, [], 'PC/mobile pages must fit the viewport and label their visible controls');
    assert(images.every(image => image.complete && image.width > 0), 'Rendered brand images must decode successfully');
    assert.deepEqual(errors, [], 'Browser console/page errors must be empty');
    assert.deepEqual([...blockedOrigins], [], 'The UI must only request the approved canary origin');
    fs.writeFileSync(path.join(OUTPUT, 'qa-report.json'), JSON.stringify({ passed: true, target: '152:18121', screenshots, layout, images, consoleErrors: errors.length, fixtureId: fixture.id, keyRevoked: true }, null, 2));
    console.log(JSON.stringify({ passed: true, screenshots: screenshots.length, consoleErrors: errors.length, keyRevoked: true, images }));
  } catch (error) {
    await failureEvidence(error);
    throw error;
  } finally {
    config.password = '';
    await browser.close();
  }
}

main().catch(error => {
  // Error messages may include response details, so emit only a sanitized summary.
  process.stderr.write(`Nexus browser QA failed: ${error.name}: ${redact(error.message)}\n`);
  process.exitCode = 1;
});
