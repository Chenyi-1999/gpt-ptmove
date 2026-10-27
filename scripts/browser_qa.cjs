// Standalone browser QA; uses an existing Playwright runtime, no test framework.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const base = process.env.QA_BASE_URL || 'http://127.0.0.1:18121';
  // Deployment auth stays in memory; never write the server password to disk.
  const password = process.env.QA_SSH_AUTH === '1'
    ? require('node:child_process').execFileSync('ssh', ['itemkeep-server','python3 /opt/gpt-ptmove/scripts/show_admin_password.py'], {encoding:'utf8'}).trim()
    : JSON.parse(fs.readFileSync(process.env.QA_ACCESS_FILE || '.local/test-access.json', 'utf8')).password;
  const output = path.resolve('output/playwright');
  fs.mkdirSync(output, {recursive:true});
  const browser = await chromium.launch({headless:true, ...(process.env.QA_BROWSER_CHANNEL ? {channel:process.env.QA_BROWSER_CHANNEL} : {})});
  const page = await browser.newPage({viewport:{width:1440,height:1000}, deviceScaleFactor:1});
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => {if (m.type()==='error') errors.push(m.text());});
  const prefix = process.env.QA_PREFIX || 'local';
  const shot = name => page.screenshot({path:path.join(output, `${prefix}-${name}.png`),fullPage:true});
  await page.goto(base, {waitUntil:'networkidle'});
  await page.locator('#login-password').waitFor({state:'visible'});
  await shot('login-desktop');
  await page.locator('#login-password').fill(password);
  await page.locator('#login-form button[type=submit]').click();
  await page.locator('#app-view').waitFor({state:'visible'});
  await page.waitForFunction(() => document.querySelector('#quota-value').textContent.includes('/'));
  await shot('overview-desktop');
  const initial = await page.evaluate(() => fetch('/api/dashboard').then(r=>r.json()));
  assert.equal(initial.rule.enabled, false);

  await page.locator('.nav-item[data-page=rules]').click();
  await page.waitForFunction(() => document.querySelector('[name=monthly_limit]').value === '6');
  await shot('rules-desktop');
  if(process.env.QA_WRITE_FORMS === '1') {
    await page.locator('[name=monthly_limit]').fill('15');
    let saved = page.waitForResponse(r => r.url().endsWith('/api/rules') && r.request().method()==='PUT');
    await page.locator('#rules-form button[type=submit]').click();
    assert.equal((await saved).status(),200);
    assert.equal((await page.evaluate(()=>fetch('/api/rules').then(r=>r.json()))).monthly_limit,15);
    await page.locator('[name=monthly_limit]').fill('6');
    saved = page.waitForResponse(r => r.url().endsWith('/api/rules') && r.request().method()==='PUT');
    await page.locator('#rules-form button[type=submit]').click();
    assert.equal((await saved).status(),200);
  }
  await page.locator('.nav-item[data-page=overview]').click();
  await page.locator('#demo-button').click();
  await page.locator('#demo-label').waitFor({state:'visible'});
  await page.waitForTimeout(600);
  assert.equal(await page.locator('.candidate-check').count(),5);
  assert.equal(await page.locator('.candidate-check:not(:disabled)').count(),0);
  const afterDemo = await page.evaluate(()=>fetch('/api/dashboard').then(r=>r.json()));
  assert.deepEqual(afterDemo.quota, initial.quota);
  assert.equal(afterDemo.counts.tasks,initial.counts.tasks);
  await shot('demo-desktop');
  await page.locator('.nav-item[data-page=connections]').click();
  await page.locator('#settings-form').waitFor({state:'visible'});
  const downloadEvent = page.waitForEvent('download');
  await page.locator('#compose-button').click();
  const download = await downloadEvent;
  const composePath = path.join(output, `${prefix}-worker-compose.yaml`);
  await download.saveAs(composePath);
  const compose = fs.readFileSync(composePath,'utf8');
  assert(compose.includes('network_mode: host'));
  assert(compose.includes('/state'));
  await shot('connections-desktop');

  if (process.env.QA_WRITE_FORMS === '1') {
    await page.locator('[name=source_rss_url]').fill('https://zmpt.cc/rss.php?passkey=synthetic-qa-only');
    await page.locator('[name=qb_api_key]').fill('qbt_' + 'A'.repeat(28));
    let saved=page.waitForResponse(r=>r.url().endsWith('/api/settings')&&r.request().method()==='PUT');
    await page.locator('#settings-form button[type=submit]').click();
    assert.equal((await saved).status(),200);
    await page.waitForFunction(()=>document.querySelector('[name=source_rss_url]').value==='');
    assert.equal(await page.locator('[name=qb_api_key]').inputValue(),'');
    saved=page.waitForResponse(r=>r.url().endsWith('/api/settings')&&r.request().method()==='PUT');
    await page.locator('#settings-form button[type=submit]').click();
    assert.equal((await saved).status(),200);
    let settings=await page.evaluate(()=>fetch('/api/settings').then(r=>r.json()));
    assert.equal(settings.configured_secrets.source_rss_url,true);
    assert.equal(settings.configured_secrets.qb_api_key,true);
    assert(!JSON.stringify(settings).includes('synthetic-qa-only'));
    assert(!JSON.stringify(settings).includes('qbt_'+'A'.repeat(28)));
    await page.locator('[name=clear_source_rss_url]').check();
    await page.locator('[name=clear_qb_api_key]').check();
    saved=page.waitForResponse(r=>r.url().endsWith('/api/settings')&&r.request().method()==='PUT');
    await page.locator('#settings-form button[type=submit]').click();
    assert.equal((await saved).status(),200);
  }

  const lint = [];
  for (const width of [1440,768,390]) {
    await page.setViewportSize({width,height:width===390?844:1000});
    for(const tab of ['overview','rules','connections']) {
      await page.locator(`.nav-item[data-page=${tab}]`).click();
      await page.waitForTimeout(150);
      const result=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,
        tiny:[...document.querySelectorAll('label,p,small,.metric-label,.metric-foot')].filter(n=>n.getBoundingClientRect().width&&n.getBoundingClientRect().height&&parseFloat(getComputedStyle(n).fontSize)<12).map(n=>({text:n.textContent.trim().slice(0,35),size:getComputedStyle(n).fontSize})),
        missingLabels:[...document.querySelectorAll('input,select')].filter(n=>n.getBoundingClientRect().width&&!n.labels?.length&&!n.getAttribute('aria-label')).map(n=>n.name||n.id)}));
      lint.push({tab,...result});
      assert(result.scroll<=result.width, `horizontal page overflow ${tab} at ${width}`);
      assert.equal(result.missingLabels.length,0,`missing form labels ${tab}`);
      if(width===390) await shot(`${tab}-mobile`);
    }
  }
  assert.equal(errors.length,0,JSON.stringify(errors));
  fs.writeFileSync(path.join(output,`${prefix}-qa.json`),JSON.stringify({base,passed:true,checks:['login','dashboard','rules','demo-isolation','compose-download','responsive','form-labels'],errors,lint},null,2));
  await browser.close();
  console.log(JSON.stringify({passed:true,prefix,screenshots:fs.readdirSync(output).filter(n=>n.startsWith(prefix)&&n.endsWith('.png')).length,tinyText:lint.reduce((n,r)=>n+r.tiny.length,0)}));
})().catch(error=>{console.error(error.message);process.exit(1);});
