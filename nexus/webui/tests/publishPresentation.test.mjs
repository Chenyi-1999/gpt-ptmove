import assert from 'node:assert/strict'
import test from 'node:test'
import {
  getAutoAddPresentation,
  getMissingSeedFields,
  getPublishResponseStatus,
  getTaskDisplayStatus,
  redactPrivateText,
  safeTrackerResultUrl,
} from '../src/utils/publishPresentation.ts'

test('queue completion does not imply new publication', () => {
  const queue = { kind: 'publish_queue', status: 'success', progressText: '' }
  assert.equal(getTaskDisplayStatus(queue).label, '队列处理完成')
  assert.equal(getTaskDisplayStatus({ ...queue, outcome: 'incomplete' }).label, '未完成')
  assert.equal(getTaskDisplayStatus({ ...queue, outcome: 'published' }).label, '发布成功')
  assert.equal(getTaskDisplayStatus({ ...queue, outcome: 'exists' }).label, '已存在')
  assert.equal(getTaskDisplayStatus({ ...queue, outcome: 'failed' }).label, '发布失败')
  assert.equal(
    getTaskDisplayStatus({ ...queue, kind: 'publish_log', progressText: 'edited' }).label,
    '已编辑',
  )
})

test('publish response requires explicit success and distinguishes existing and edited torrents', () => {
  assert.equal(getPublishResponseStatus({}), 'incomplete')
  assert.equal(getPublishResponseStatus({ success: true, is_existing_torrent: true }), 'exists')
  assert.equal(
    getPublishResponseStatus({ success: true, auto_edit_result: { success: true } }),
    'edited',
  )
  assert.equal(
    getPublishResponseStatus({ success: true, source_restriction_status: 'denied' }),
    'failed',
  )
  assert.equal(getPublishResponseStatus({ success: false, is_existing_torrent: true }), 'failed')
  assert.equal(getPublishResponseStatus({ success: true }), 'success')
})

test('downloader status uses actual state and keeps failure details independent', () => {
  assert.equal(getAutoAddPresentation('success', '').label, '无添加记录')
  assert.equal(getAutoAddPresentation('failed', '{').label, '未执行')
  assert.equal(getAutoAddPresentation('queued', '').label, '待执行')
  assert.equal(
    getAutoAddPresentation('exists', {
      success: false,
      message: '检测到目标站点种子已存在，按设置跳过自动添加',
    }).label,
    '未执行',
  )
  assert.equal(
    getAutoAddPresentation('success', { success: false, message: '自动添加已跳过：缺少 savePath' })
      .label,
    '未执行',
  )
  const failure = getAutoAddPresentation(
    'success',
    '{"success":false,"message":"请求超时 token=test-secret"}',
  )
  assert.equal(failure.label, '添加失败')
  assert.equal(failure.detail, '请求超时 token=[已隐藏]')
})

test('lists only known missing materials and projected empty required fields', () => {
  assert.deepEqual(getMissingSeedFields({}), [])
  assert.deepEqual(getMissingSeedFields({ missing_required_fields: [] }), [])
  assert.deepEqual(
    getMissingSeedFields({
      missing_required_fields: ['poster', 'mediainfo', 'unknown'],
      type: ' ',
      team: 'team.ok',
    }),
    ['海报', 'MediaInfo / BDInfo', '类型'],
  )
})

test('result links preserve canonical detail ids without private parameters', () => {
  assert.equal(
    safeTrackerResultUrl('https://tracker.test/details.php?id=12&passkey=secret#secret'),
    'https://tracker.test/details.php?id=12',
  )
  assert.equal(
    safeTrackerResultUrl(
      'https://tracker.test/torrent/info/12|DIRECT_DOWNLOAD:https://tracker.test/download/secret',
    ),
    'https://tracker.test/torrent/info/12',
  )
  assert.equal(
    safeTrackerResultUrl('https://rousi.pro/api/v1/torrents/99999999-9999-9999-9999-999999999999'),
    'https://rousi.pro/torrent/99999999-9999-9999-9999-999999999999',
  )
  for (const url of [
    'javascript:alert(1)',
    '//tracker.test/details.php?id=12',
    'https://a:b@tracker.test/details.php?id=12',
    'https://tracker.test/download.php?id=12',
    'https://tracker.test/details.php?id=not-a-number',
  ]) {
    assert.equal(safeTrackerResultUrl(url), '')
  }
})

test('logs do not expose raw tracker markup, private URLs or request credentials', () => {
  const logs = [
    'Cookie: uid=hidden1; auth=hidden2',
    'Authorization: Bearer hidden3',
    '请求 https://tracker.test/details.php?id=12&key=hidden4',
    '直链 https://tracker.test/api/torrent/download/12/hidden5',
    '{"passkey":"hidden6"}',
    '<input type="hidden" name="csrf" value="hidden7">',
    '<script>let password = "hidden8"</script>',
  ].join('\n')
  const redacted = redactPrivateText(logs)
  assert.doesNotMatch(redacted, /hidden\d/)
  assert.match(redacted, /请求 https:\/\/tracker\.test\/details\.php/)
  assert.equal(redactPrivateText(redacted), redacted)
})
