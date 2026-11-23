import assert from 'node:assert/strict'
import test from 'node:test'
import {
  chooseAutomaticScreenshots,
  countDistinctScreenshotURLs,
} from '../src/components/cross-seed/panel/mediaCompletion.ts'

const frame = (timeSeconds, recommended = false, id = String(timeSeconds)) => ({
  id,
  timeSeconds,
  recommended,
})

test('preselects five distinct frames, prefers recommended choices, sorts timestamps', () => {
  const selected = chooseAutomaticScreenshots([
    frame(60, true), frame(10), frame(30, true), frame(40), frame(50, true),
    frame(20), frame(30.2, true), frame(0, true), frame(Infinity, true), frame(80, true, '60'),
  ])
  assert.deepEqual(selected.map(({ timeSeconds }) => timeSeconds), [10, 20, 30, 50, 60])
})

test('uses fallback spacing when a preferred frame prevents choosing five', () => {
  const selected = chooseAutomaticScreenshots([
    frame(0.5), frame(1, true), frame(1.6), frame(2.7), frame(3.8), frame(4.9),
  ])
  assert.deepEqual(selected.map(({ timeSeconds }) => timeSeconds), [0.5, 1.6, 2.7, 3.8, 4.9])
  assert.deepEqual(chooseAutomaticScreenshots([frame(1), frame(2), frame(3), frame(4)]), [])
})

test('rejects missing, credential-bearing, duplicate, and non-HTTP screenshot results', () => {
  assert.equal(countDistinctScreenshotURLs([
    'https://img.example/1.jpg', 'https://IMG.EXAMPLE/1.jpg#fragment', '',
    'https://u:p@img.example/2.jpg', 'ftp://img.example/3.jpg', 'https://img.example/4.jpg',
  ]), 2)
})
