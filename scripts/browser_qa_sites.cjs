// Existing Playwright runtime; target an isolated 152 canary for mutation checks.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const {execFileSync} = require('node:child_process');
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const base = process.env.QA_BASE_URL;
  assert(base && !/127\.0\.0\.1|localhost/.test(base), 'QA requires the remote152 deployment');
  const password = execFileSync('ssh', ['itemkeep-server', process.env.QA_AUTH_COMMAND || 'python3 /opt/gpt-ptmove/scripts/show_admin_password.py'], {encoding:'utf8'}).trim();
  const output = path.resolve('output/playwright'); fs.mkdirSync(output, {recursive:true});
  const prefix = process.env.QA_PREFIX || 'sites-020';
  const browser = await chromium.launch({headless:true, channel:process.env.QA_BROWSER_CHANNEL || 'msedge'});
  const page = await browser.newPage({viewport:{width:1440,height:1000}});
  const errors=[]; page.on('pageerror',e=>errors.push(e.message)); page.on('console',m=>{if(m.type()==='error')errors.push(m.text());});
  page.on('dialog',d=>d.accept());
  const shot=name=>page.screenshot({path:path.join(output,`${prefix}-${name}.png`),fullPage:true});
  const read=async route=>{
    const response=await page.request.get(base+route);
    const value=await response.text();
    assert.equal(response.status(),200,`read ${route}: HTTP${response.status()}, ${value.length} bytes`);
    assert(value.length,`read ${route}: empty body`);
    return JSON.parse(value);
  };
  await page.goto(base,{waitUntil:'networkidle'});
  await page.locator('#login-password').fill(password);
  await page.locator('#login-form button').click();
  await page.locator('#app-view').waitFor({state:'visible'});
  await page.waitForFunction(()=>document.querySelector('#app-version').textContent.includes('0.2.0'));
  await page.locator('.nav-item[data-page=sites]').click();
  await page.waitForFunction(()=>document.querySelectorAll('.site-card').length>=2);
  assert((await page.locator('#sites-list').innerText()).includes('PTNexus：zmpt'));
  const before=await read('/api/dashboard');
  if(process.env.QA_MUTATE_SITES==='1') {
    const beforeSites=await page.locator('.site-card').count();
    const profile={schema_version:1,id:'qa-tracker',name:'QA JSON站点',base_url:'https://qa.example.com',roles:['source','target'],ptnexus_site:'qa-tracker',announce_hosts:['qa.example.com'],official_groups:['qa']};
    await page.locator('#site-file').setInputFiles({name:'site.json',mimeType:'application/json',buffer:Buffer.from(JSON.stringify(profile))});
    await page.locator('#site-preview-button').click();
    await page.locator('#site-preview').waitFor({state:'visible'});
    assert.equal(await page.locator('.site-card').count(),beforeSites);
    if((await page.locator('#site-preview').innerText()).includes('"exists": true')) await page.locator('#site-update-existing').check();
    let saved=page.waitForResponse(r=>r.url().endsWith('/api/sites/import')&&r.request().method()==='POST');
    await page.locator('#site-import-button').click(); assert.equal((await saved).status(),200);
    const card=page.locator('.site-card').filter({has:page.getByRole('heading',{name:'QA JSON站点',exact:true})});
    await card.waitFor(); await card.getByRole('button',{name:'配置凭据'}).click();
    await card.locator('[name=cookie]').fill('QA-SYNTHETIC-COOKIE');
    await card.locator('[name=rss_url]').fill('https://qa.example.com/rss?passkey=QA-SYNTHETIC');
    saved=page.waitForResponse(r=>r.url().endsWith('/api/sites/qa-tracker/credentials')&&r.request().method()==='PUT');
    await card.getByRole('button',{name:'保存凭据'}).click(); assert.equal((await saved).status(),200);
    await card.locator('.site-credential-form').waitFor({state:'detached'});
    await page.waitForFunction(()=>[...document.querySelectorAll('.site-card')].some(n=>n.textContent.includes('QA JSON站点')&&n.textContent.includes('Cookie 已配置')));
    await card.getByRole('button',{name:'配置凭据'}).click();
    assert.equal(await card.locator('[name=cookie]').inputValue(),'');
    saved=page.waitForResponse(r=>r.url().endsWith('/api/sites/qa-tracker/credentials')&&r.request().method()==='PUT');
    await card.getByRole('button',{name:'保存凭据'}).click(); assert.equal((await saved).status(),200);
    await card.locator('.site-credential-form').waitFor({state:'detached'});
    await card.getByRole('button',{name:'配置凭据'}).click();
    await card.locator('[name=clear_credentials]').check();
    saved=page.waitForResponse(r=>r.url().endsWith('/api/sites/qa-tracker/credentials')&&r.request().method()==='PUT');
    await card.getByRole('button',{name:'保存凭据'}).click(); assert.equal((await saved).status(),200);
    await card.locator('.site-credential-form').waitFor({state:'detached'});
    const data=await read('/api/sites');
    assert.deepEqual(data.items.find(i=>i.id==='qa-tracker').configured_secrets,{cookie:false,rss_url:false});
    await page.locator('.nav-item[data-page=rules]').click();
    await page.locator('[name=source_site] option[value=qa-tracker]').waitFor({state:'attached'});
    await page.locator('[name=source_site]').selectOption('qa-tracker');
    saved=page.waitForResponse(r=>r.url().endsWith('/api/rules')&&r.request().method()==='PUT');
    await page.locator('#rules-form button[type=submit]').click(); assert.equal((await saved).status(),200);
    await page.locator('[name=source_site]').selectOption(before.rule.source_site);
    saved=page.waitForResponse(r=>r.url().endsWith('/api/rules')&&r.request().method()==='PUT');
    await page.locator('#rules-form button[type=submit]').click(); assert.equal((await saved).status(),200);
  }
  await page.locator('.nav-item[data-page=ai]').click();
  await page.locator('#ai-guide-text').waitFor({state:'visible'});
  await page.waitForFunction(()=>document.querySelector('#ai-guide-text').value.includes('/api/v1'));
  let download=page.waitForEvent('download'); await page.locator('#ai-openapi-download').click();
  const specDownload=await download; const specPath=path.join(output,`${prefix}-openapi.json`); await specDownload.saveAs(specPath);
  const spec=JSON.parse(fs.readFileSync(specPath)); assert(Object.keys(spec.paths).length>10);
  if(process.env.QA_MUTATE_SITES==='1') {
    await page.locator('#api-key-form [name=name]').fill('QA read-only verification');
    await page.locator('#api-key-form button[type=submit]').click();
    await page.locator('#new-api-key textarea').waitFor({state:'visible'});
    const token=await page.locator('#new-api-key textarea').inputValue();
    assert(token.startsWith('ptm_'));
    const status=await page.evaluate(async t=>(await fetch('/api/v1/dashboard',{headers:{Authorization:'Bearer '+t}})).status,token);
    assert.equal(status,200);
    await page.locator('#new-api-key').getByRole('button',{name:'隐藏并清除'}).click();
    assert.equal(await page.locator('#new-api-key').isVisible(),false);
    await page.locator('#api-keys-list').getByRole('button',{name:'撤销'}).click();
    await page.waitForFunction(()=>document.querySelector('#api-keys-list').textContent.includes('已撤销'));
    const revoked=await page.request.get(base+'/api/v1/dashboard',{headers:{Authorization:'Bearer '+token}});
    assert.equal(revoked.status(),401);
    // The expected revoked response is made via APIRequestContext, not a console error.
    assert(!JSON.stringify(await page.evaluate(()=>({...localStorage}))).includes(token));
  }
  const lint=[];
  for(const width of [1440,768,390]) {
    await page.setViewportSize({width,height:width===390?844:1000});
    for(const tab of ['overview','rules','connections','sites','ai']) {
      await page.locator(`.nav-item[data-page=${tab}]`).click(); await page.waitForTimeout(200);
      const result=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,
        missingLabels:[...document.querySelectorAll('input,select,textarea')].filter(n=>n.getBoundingClientRect().width&&!n.labels?.length&&!n.getAttribute('aria-label')).map(n=>n.id||n.name),
        tiny:[...document.querySelectorAll('p,label,small')].filter(n=>n.getBoundingClientRect().width&&parseFloat(getComputedStyle(n).fontSize)<12).map(n=>n.tagName)}));
      assert(result.scroll<=result.width,`${tab} overflow at${width}`); assert.equal(result.missingLabels.length,0); assert.equal(result.tiny.length,0);
      lint.push({tab,...result}); if(width!==768)await shot(`${tab}-${width}`);
    }
  }
  const after=await read('/api/dashboard');
  assert.deepEqual(after.quota,before.quota); assert.deepEqual(after.counts,before.counts); assert.equal(after.rule.enabled,false);
  assert.deepEqual(errors,[]);
  await page.locator('#mobile-logout-button').click(); await page.locator('#login-view').waitFor({state:'visible'}); assert.equal(await page.locator('#new-api-key textarea').count(),0);
  fs.writeFileSync(path.join(output,`${prefix}-qa.json`),JSON.stringify({passed:true,base,mutated:process.env.QA_MUTATE_SITES==='1',errors,lint,countsUnchanged:true,quotaUnchanged:true},null,2));
  await browser.close(); console.log(JSON.stringify({passed:true,prefix,screenshots:10,mutationsIsolated:process.env.QA_MUTATE_SITES==='1'}));
})().catch(e=>{console.error(e.message);process.exit(1);});
