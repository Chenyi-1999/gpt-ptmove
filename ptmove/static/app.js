(() => {
  'use strict';
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
  const state = { csrf: '', dashboard: null, candidates: [], tasks: [], demo: false, timer: null, busy: false };
  const apiRoot = '/api';
  const labels = { queued:'排队中', leased:'已领取', downloading:'下载中', preparing:'准备中', publishing:'发布中', published:'已发布', seeding:'做种中', succeeded:'已完成', failed:'失败', needs_review:'需要核查', cancelled:'已取消', movie:'电影', tv:'剧集', other:'其他' };

  function text(node, value) { if (node) node.textContent = value == null || value === '' ? '—' : String(value); }
  function el(tag, cls, value) { const n = document.createElement(tag); if (cls) n.className = cls; if (value !== undefined) n.textContent = value; return n; }
  function formatTime(value) { if (!value) return '—'; const d = new Date(value); return Number.isNaN(+d) ? String(value) : new Intl.DateTimeFormat('zh-CN', { dateStyle:'medium', timeStyle:'short', hour12:false }).format(d); }
  function formatBytes(bytes) { const n = Number(bytes); if (!Number.isFinite(n) || n <= 0) return '0 GiB'; return `${(n / 1073741824).toFixed(n / 1073741824 >= 10 ? 0 : 1)} GiB`; }
  function statusLabel(value) { return labels[value] || value || '未知'; }
  function showNotice(message, error = false) { const n = $('#notice'); n.textContent = message; n.classList.toggle('error', error); n.hidden = false; clearTimeout(showNotice.timer); showNotice.timer = setTimeout(() => { n.hidden = true; }, 6500); }
  function safeError(err) { let message = (err && err.message) || '请求未完成，请稍后重试。'; message = message.replace(/(cookie|passkey|token|password|secret)\s*[:=]\s*[^\s,;]+/ig, '$1=[已隐藏]').slice(0, 240); return message; }

  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (options.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    if (options.method && !['GET','HEAD'].includes(options.method.toUpperCase()) && state.csrf) headers.set('X-CSRF-Token', state.csrf);
    const response = await fetch(path, { ...options, headers, credentials:'same-origin', cache:'no-store' });
    if (response.status === 401) { expireSession(); throw new Error('登录已过期，请重新登录。'); }
    if (!response.ok) {
      let msg = `请求失败（${response.status}）`;
      try { const data = await response.json(); if (typeof data.detail === 'string') msg = data.detail; } catch (_) { /* Keep generic message. */ }
      throw new Error(msg);
    }
    if (response.status === 204) return null;
    const type = response.headers.get('content-type') || '';
    return type.includes('application/json') ? response.json() : response;
  }
  function expireSession() { state.csrf = ''; clearInterval(state.timer); $('#app-view').hidden = true; $('#login-view').hidden = false; $('#login-password').value = ''; }
  function enterApp(csrf) { state.csrf = csrf || ''; $('#login-view').hidden = true; $('#app-view').hidden = false; refreshAll(); clearInterval(state.timer); state.timer = setInterval(() => { if (!document.hidden && !state.busy) { refreshDashboard(); refreshTasks(); } }, 30000); }

  async function init() {
    $('#login-view').hidden = false;
    try { const session = await request(`${apiRoot}/auth/session`); if (session.authenticated) enterApp(session.csrf_token); }
    catch (err) { if (!/401|登录已过期/.test(err.message)) showLoginError('无法连接控制器，请检查服务后重试。'); }
  }
  function showLoginError(message) { const e = $('#login-error'); if (e) e.textContent = message; }
  $('#login-form').addEventListener('submit', async event => {
    event.preventDefault(); showLoginError(''); const button = $('button[type="submit"]', event.currentTarget); button.disabled = true; button.textContent = '正在登录…';
    try { const data = await request(`${apiRoot}/auth/login`, { method:'POST', body:JSON.stringify({ password:$('#login-password').value }) }); $('#login-password').value = ''; enterApp(data.csrf_token); }
    catch (err) { showLoginError(safeError(err)); }
    finally { button.disabled = false; button.textContent = '登录管理台'; }
  });
  $('#logout-button').addEventListener('click', async () => { try { await request(`${apiRoot}/auth/logout`, { method:'POST' }); } catch (_) { /* Session expiry is also a logout. */ } expireSession(); });

  function goTo(page, options = {}) {
    const titles = { overview:'总览', candidates:'候选种子', tasks:'转种任务', rules:'自动规则', connections:'连接与执行端' };
    $$('.page').forEach(p => { const active = p.id === `page-${page}`; p.classList.toggle('active', active); p.hidden = !active; });
    $$('.nav-item[data-page]').forEach(b => { const active = b.dataset.page === page; b.classList.toggle('active', active); if (active) b.setAttribute('aria-current','page'); else b.removeAttribute('aria-current'); });
    text($('#page-title'), titles[page] || '总览');
    if (options.load !== false) { if (page === 'candidates') refreshCandidates(); if (page === 'tasks') refreshTasks(); if (page === 'rules') loadRules(); if (page === 'connections') loadSettings(); }
  }
  $$('.nav-item[data-page]').forEach(b => b.addEventListener('click', () => goTo(b.dataset.page)));
  $$('[data-goto]').forEach(b => b.addEventListener('click', () => goTo(b.dataset.goto)));
  $('#refresh-button').addEventListener('click', refreshAll);

  async function refreshAll() { await Promise.allSettled([refreshDashboard(), refreshTasks()]); }
  async function refreshDashboard() {
    try { const [data, eventData] = await Promise.all([request(`${apiRoot}/dashboard`), request(`${apiRoot}/events`)]); state.dashboard = data; renderDashboard(data); renderEvents(eventData.items || []); text($('#app-version'), `v${data.version || '0.1.0'}`); }
    catch (err) { showNotice(safeError(err), true); }
  }
  async function refreshTasks() {
    try { const data = await request(`${apiRoot}/tasks`); state.tasks = data.items || []; renderTasks(state.tasks); renderRecentTasks(state.tasks.slice(0,5)); }
    catch (err) { showNotice(safeError(err), true); }
  }
  function renderDashboard(data) {
    const quota = data.quota || {}, counts = data.counts || {}, rule = data.rule || {};
    const used = Number(quota.used ?? quota.published ?? quota.succeeded ?? 0); const limit = Number(quota.limit ?? rule.monthly_limit ?? 0);
    text($('#quota-value'), `${used} / ${limit || '—'}`); text($('#quota-foot'), `${Number(quota.succeeded || 0)} 已完成 · ${Number(quota.published || 0)} 已发布`);
    $('#quota-progress').style.width = `${limit ? Math.min(100, used / limit * 100) : 0}%`;
    const reserved = Number(quota.reserved_bytes || 0); const budget = Number(quota.budget_bytes ?? (Number(rule.monthly_budget_gib || 0) * 1073741824));
    text($('#budget-value'), formatBytes(reserved)); text($('#budget-foot'), `上限 ${formatBytes(budget)}`); $('#budget-progress').style.width = `${budget ? Math.min(100, reserved / budget * 100) : 0}%`;
    text($('#pending-value'), Number(counts.pending || 0)); text($('#review-value'), Number(counts.needs_review || 0));
    text($('#next-run'), rule.enabled === false ? '计划已关闭' : formatTime(data.next_run_at));
    const workers = Array.isArray(data.workers) ? data.workers : []; const list = $('#workers-list'); list.replaceChildren();
    const live = workers.filter(w => { const seen = Date.parse(w.last_seen || ''); const age = Date.now() - seen; return Number.isFinite(seen) && age >= -5000 && age <= 90000; }).length;
    const chip = $('#worker-status'); chip.className = `status-chip ${live ? 'ok' : 'warn'}`; text(chip, live ? `${live} 个已连接` : '暂无心跳');
    if (!workers.length) list.append(el('div','empty-inline','未发现 NAS 执行端。可在「连接与执行端」中查看接入步骤。'));
    workers.forEach(worker => { const row = el('div','worker-row'); const info = el('div'); const seen = Date.parse(worker.last_seen || ''); const age = Date.now() - seen; const online = Number.isFinite(seen) && age >= -5000 && age <= 90000; info.append(el('div','worker-name',worker.worker_id || 'NAS 执行端'),el('div','worker-meta',`版本 ${worker.version || '未知'} · 最近心跳 ${formatTime(worker.last_seen)}`)); row.append(info,el('span',`status-chip ${online ? 'ok' : 'neutral'}`,online ? '在线' : '离线')); list.append(row); });
  }
  function renderEvents(items) {
    const list = $('#events-list'); list.replaceChildren();
    if (!items.length) { list.append(el('div','empty-inline','暂无运行事件')); return; }
    items.slice(0,5).forEach(item => { const row = el('div','event-row'); const sev = String(item.severity || 'info').toLowerCase(); row.append(el('span',`event-mark ${sev === 'error' ? 'error' : sev === 'warning' || sev === 'warn' ? 'warn' : ''}`)); const body = el('div'); body.append(el('div','event-message',item.message || '运行事件')); body.append(el('div','event-time',formatTime(item.created_at))); row.append(body); list.append(row); });
  }
  function table(headers, rows, emptyTitle, emptyText) {
    const wrap = el('div','table-scroll');
    if (!rows.length) { const blank = el('div','table-empty'); blank.append(el('strong','',emptyTitle),el('span','',emptyText)); wrap.append(blank); return wrap; }
    const t = el('table'); const thead = el('thead'); const trh = el('tr'); headers.forEach(h => trh.append(el('th','',h))); thead.append(trh); t.append(thead);
    const tbody = el('tbody'); rows.forEach(row => { const tr = el('tr'); row.forEach(cell => { const td = el('td'); if (cell instanceof Node) td.append(cell); else td.textContent = cell == null ? '—' : String(cell); tr.append(td); }); tbody.append(tr); }); t.append(tbody); wrap.append(t); return wrap;
  }
  function statusNode(status) { const s = el('span',`status-text ${status || ''}`,statusLabel(status)); return s; }
  function taskRows(items, actions = false) { return items.map(task => {
    const candidate = task.candidate || {}; const title = el('div','title-cell',candidate.title || `任务 ${task.id || ''}`); title.append(el('div','secondary-line',`${task.month || '—'} · ${formatTime(task.created_at)}`));
    const cells = [title,statusNode(task.status),task.message || task.published_url || '—',formatBytes(task.downloaded_bytes || candidate.size_bytes)];
    if (actions) { const group = el('div'); const canCancel = task.status === 'queued' && task.ever_claimed === false && !task.publish_attempted && !Number(task.downloaded_bytes); const activeStatuses = ['leased','downloading','preparing','publishing','seeding','succeeded']; const canRepair = Boolean(task.published_url) && !activeStatuses.includes(task.status) && task.message !== '等待补种修复'; const canRetry = task.status === 'failed' && !task.published_url && !task.publish_attempted; if (canCancel) group.append(actionButton('取消',async () => { if (confirm('确认取消此任务？只允许取消尚未领取的排队任务。')) await taskAction(task.id,'cancel'); },'button ghost small')); if (canRepair) group.append(actionButton('修复做种',async () => { if (confirm('确认安排已发布内容的补种修复？此操作不会重新发布。')) await taskAction(task.id,'retry'); },'button secondary small')); if (canRetry) group.append(actionButton('安全重试',async () => { if (confirm('确认安全重试此任务？')) await taskAction(task.id,'retry'); },'button secondary small')); if (task.status === 'needs_review' && !task.published_url && task.publish_attempted) group.append(actionButton('确认已发布链接',async () => await reconcileTask(task),'button secondary small')); if (task.status === 'needs_review' || task.publish_attempted && !task.published_url) group.append(el('span','subtle-label','请核对 NovaHD 发布结果')); cells.push(group); }
    return cells;
  }); }
  function actionButton(label, fn, cls) { const b = el('button',cls,label); b.type='button'; b.addEventListener('click',fn); return b; }
  function renderTasks(items) {
    const filter = $('#task-filter').value; const visible = items.filter(t => filter === 'all' || (filter === 'active' && ['queued','leased','downloading','preparing','publishing'].includes(t.status)) || t.status === filter || (filter === 'succeeded' && ['succeeded','published','seeding'].includes(t.status)));
    text($('#task-summary'),`${items.length} 条任务记录`); $('#tasks-table').replaceChildren(table(['候选项目','状态','进度与结果','已下载','操作'],taskRows(visible,true),'暂无转种任务','手动审核候选项后，提交的任务会显示在这里。'));
  }
  function renderRecentTasks(items) { $('#recent-tasks').replaceChildren(table(['候选项目','状态','进度与结果','已下载'],taskRows(items),'暂无任务','任务创建后会显示在这里。')); }
  $('#tasks-refresh').addEventListener('click',refreshTasks); $('#task-filter').addEventListener('change',() => renderTasks(state.tasks));
  async function taskAction(id, action) { try { await request(`${apiRoot}/tasks/${encodeURIComponent(id)}/${action}`,{method:'POST',body:'{}'}); showNotice(action === 'cancel' ? '已提交取消请求。' : action === 'retry' ? '已提交安全任务处理请求。' : '任务已处理。'); await refreshTasks(); } catch (err) { showNotice(safeError(err),true); } }
  async function reconcileTask(task) { const url = prompt('请粘贴你已在 NovaHD 核实的详情页链接。确认后只会安排补种修复，不会重新发布。'); if (!url) return; if (!confirm('确认此链接对应 NovaHD 上已发布的同一资源？')) return; try { await request(`${apiRoot}/tasks/${encodeURIComponent(task.id)}/reconcile`,{method:'POST',body:JSON.stringify({published_url:url.trim()})}); showNotice('已确认发布链接，并安排补种修复；不会重复发布。'); await refreshTasks(); } catch (_) { showNotice('链接无效或任务状态已变化。请核对 NovaHD 链接和任务状态后重试。',true); } }

  async function refreshCandidates() {
    try { const data = await request(`${apiRoot}/candidates`); state.candidates = data.items || []; state.demo = false; $('#demo-label').hidden = true; text($('#candidate-summary'),`${data.total ?? state.candidates.length} 个候选项 · 仅手动选择后提交`); renderCandidates(); }
    catch (err) { showNotice(safeError(err),true); }
  }
  function canSelect(c) { return c.selected === true && c.restricted === false && c.permission_verified === true && c.target_exists === false && typeof c.target_work_exists === 'boolean'; }
  function renderCandidates() {
    const host = $('#candidates-table'); host.replaceChildren();
    if (!state.candidates.length) { host.append(table(['选择','标题','体积','热度','分类','审核结果'],[],'没有候选项','先手动扫描；演示预览不会写入列表或占用名额。')); }
    else {
      const rows = state.candidates.map(c => {
        const check = document.createElement('input'); check.type='checkbox'; check.className='candidate-check'; check.setAttribute('aria-label',`选择 ${c.title || '未命名候选项'}`); check.disabled = state.demo || !canSelect(c); check.checked = false; check.dataset.id = c.id; check.addEventListener('change',updateSelectedCount);
        const title = el('div','title-cell',c.title || '未命名'); title.append(el('div','secondary-line',c.release_group || c.source_id || '织梦候选'));
        const reason = c.reason || (c.restricted ? c.restriction_reason || '受限内容' : c.permission_verified !== true ? '转种权限未知' : c.target_exists !== false || c.target_work_exists !== false ? '目标重复检查未通过' : '符合初步规则');
        const verdict = el('div'); verdict.append(el('span',`status-text ${canSelect(c) ? 'succeeded' : 'needs_review'}`,canSelect(c) ? '可选择' : '已阻止'),el('div','secondary-line',reason));
        return [check,title,formatBytes(c.size_bytes),`↑ ${c.seeders ?? 0} · ↓ ${c.leechers ?? 0}`,statusLabel(c.category),verdict];
      }); host.append(table(['选择','候选项目','体积','做种 · 下载','分类','审核结果'],rows,'',''));
    }
    updateSelectedCount();
  }
  function updateSelectedCount() { const n = $$('.candidate-check:checked').length; text($('#selected-count'),n); $('#transfer-selected').disabled = n === 0 || state.demo; }
  $('#candidate-refresh').addEventListener('click',refreshCandidates);
  async function startScan() { try { await request(`${apiRoot}/runs`,{method:'POST',body:JSON.stringify({mode:'manual'})}); showNotice('扫描已排队。扫描结果只会进入候选审核，不会自动转种。'); setTimeout(refreshCandidates,1800); } catch (err) { showNotice(safeError(err),true); } }
  $('#scan-button').addEventListener('click',startScan); $('#candidate-scan').addEventListener('click',startScan);
  $('#demo-button').addEventListener('click',async () => { try { const data = await request(`${apiRoot}/preview`,{method:'POST',body:JSON.stringify({demo:true})}); state.demo = true; state.candidates = (data.items || []).map(item => ({...item,selected:false})); goTo('candidates',{load:false}); $('#demo-label').hidden = false; text($('#candidate-summary'),'模拟预览 · 不会保存、下载、发布或占用名额'); renderCandidates(); showNotice('以下是模拟策略预览，不是真实候选数据。'); } catch (err) { showNotice(safeError(err),true); } });
  $('#transfer-selected').addEventListener('click',async () => { const selected = $$('.candidate-check:checked').map(c => c.dataset.id).filter(Boolean); const ids = selected.map(id => { const candidate = state.candidates.find(item => String(item.id) === id); return candidate ? candidate.id : id; }); if (!ids.length || state.demo) return; if (!confirm(`确认提交所选 ${ids.length} 项？控制器将再次检查权限、重复项与月度额度。`)) return; state.busy = true; $('#transfer-selected').disabled = true; try { const result = await request(`${apiRoot}/tasks`,{method:'POST',body:JSON.stringify({candidate_ids:ids})}); showNotice(`已提交 ${Number(result.queued || (result.items || []).length)} 项，跳过 ${Array.isArray(result.skipped) ? result.skipped.length : 0} 项。`); await Promise.all([refreshCandidates(),refreshTasks(),refreshDashboard()]); } catch (err) { showNotice(safeError(err),true); } finally { state.busy = false; updateSelectedCount(); } });

  async function loadRules() {
    try { const rule = await request(`${apiRoot}/rules`); const form = $('#rules-form'); for (const [key,value] of Object.entries(rule)) { const field = form.elements.namedItem(key); if (!field) continue; if (key === 'categories') $$('input[name="categories"]',form).forEach(c => c.checked = (value || []).includes(c.value)); else if (field.type === 'checkbox') field.checked = Boolean(value); else if (field.type !== 'fieldset') field.value = value; }
      const chip = $('#rule-state'); chip.className = `status-chip ${rule.enabled ? 'ok' : 'neutral'}`; text(chip,rule.enabled ? '计划已启用' : '计划已关闭');
    } catch (err) { showNotice(safeError(err),true); }
  }
  $('#rules-form').addEventListener('submit',async event => {
    event.preventDefault(); const form = event.currentTarget; const fd = new FormData(form); const body = {};
    for (const key of ['enabled','day','hour','minute','monthly_limit','monthly_budget_gib','candidate_limit','min_seeders','min_leechers','source_max_age_days','min_size_gib','max_size_gib','publish_interval_seconds','max_download_hours','max_run_hours','official_policy']) { const field = form.elements.namedItem(key); if (!field) continue; body[key] = field.type === 'checkbox' ? field.checked : field.type === 'number' ? Number(fd.get(key)) : fd.get(key); }
    body.categories = fd.getAll('categories');
    if (!body.categories.length) { text($('#rules-message'),'至少选择一个候选分类。'); return; }
    if (body.min_size_gib > body.max_size_gib) { text($('#rules-message'),'最大种子大小必须大于或等于最小值。'); form.elements.namedItem('max_size_gib').focus(); return; }
    if (body.max_download_hours > body.max_run_hours) { text($('#rules-message'),'下载最长时间不能超过单次运行最长时间。'); form.elements.namedItem('max_run_hours').focus(); return; }
    const submit = $('button[type="submit"]',form); submit.disabled = true;
    try { const saved = await request(`${apiRoot}/rules`,{method:'PUT',body:JSON.stringify(body)}); text($('#rules-message'),'规则已保存。计划状态以控制器返回为准。'); const chip = $('#rule-state'); chip.className=`status-chip ${saved.enabled ? 'ok' : 'neutral'}`; text(chip,saved.enabled?'计划已启用':'计划已关闭'); showNotice('自动规则已保存。'); }
    catch (err) { text($('#rules-message'),safeError(err)); showNotice(safeError(err),true); } finally { submit.disabled = false; }
  });

  async function loadSettings() {
    try { const data = await request(`${apiRoot}/settings`); const form = $('#settings-form'); for (const [key,value] of Object.entries(data)) { const field = form.elements.namedItem(key); if (field && field.type !== 'password' && field.type !== 'checkbox') field.value = value || ''; }
      $$('[data-secret]').forEach(node => text(node,(data.configured_secrets || {})[node.dataset.secret] ? (node.dataset.secret === 'source_rss_url' ? '已配置 · 留空保持不变' : '已配置 · 保留现有密钥') : '尚未配置'));
      for (const name of ['source_cookie','target_cookie','qb_password','ptnexus_password']) form.elements.namedItem(name).value = '';
    } catch (err) { showNotice(safeError(err),true); }
  }
  $('#settings-form').addEventListener('submit',async event => {
    event.preventDefault(); const form = event.currentTarget; const body = {};
    for (const field of $$('input:not([type=checkbox])',form)) { const clear = form.elements.namedItem(`clear_${field.name}`); if (field.type === 'password' || field.name === 'source_rss_url') { if (clear && clear.checked) body[field.name] = ''; else if (field.value) body[field.name] = field.value; } else body[field.name] = field.value; }
    for (const name of ['source_cookie','target_cookie','source_rss_url','qb_password','ptnexus_password']) if (form.elements.namedItem(`clear_${name}`)?.checked) body[name] = '';
    const submit = $('button[type="submit"]',form); submit.disabled=true;
    try { await request(`${apiRoot}/settings`,{method:'PUT',body:JSON.stringify(body)}); text($('#settings-message'),'连接信息已保存。密钥不会再次显示。'); form.reset(); await loadSettings(); showNotice('连接设置已更新。'); }
    catch (err) { text($('#settings-message'),safeError(err)); showNotice(safeError(err),true); }
    finally { submit.disabled=false; }
  });
  $('#compose-button').addEventListener('click',async () => { try { const data = await request(`${apiRoot}/worker-compose`); const blob = new Blob([data.compose || ''],{type:'text/yaml;charset=utf-8'}); const url = URL.createObjectURL(blob); const a = el('a'); a.href=url; a.download='ptmove-worker-compose.yml'; document.body.append(a); a.click(); a.remove(); URL.revokeObjectURL(url); showNotice('Compose 文件已下载；它不包含执行端令牌。'); } catch (err) { showNotice(safeError(err),true); } });
  $('#token-button').addEventListener('click',async () => { if (!confirm('显示当前已配置的执行端令牌？屏幕上将暂时显示令牌，请避免共享或截图。')) return; try { const data = await request(`${apiRoot}/worker-token`,{method:'POST',body:'{}'}); const box=$('#token-output'); box.replaceChildren(el('strong','','当前执行端令牌：'),el('div','',data.token || '控制器未返回令牌')); const copy=actionButton('复制令牌',async () => { try { await navigator.clipboard.writeText(data.token); copy.textContent='已复制'; } catch (_) { showNotice('无法访问剪贴板，请手动复制令牌。',true); } },'button secondary small'); box.append(copy); box.hidden=false; } catch (err) { showNotice(safeError(err),true); } });

  init();
})();
