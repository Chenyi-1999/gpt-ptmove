import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const read = (path) =>
  readFile(new URL(path, import.meta.url), 'utf8').then((text) => text.replaceAll('\r\n', '\n'))

test('site profile guide uses the same prompt and example as the authoring docs', async () => {
  const [guide, prompt, docsExample, webExample] = await Promise.all([
    read('../../../docs/sites/AI_PROMPT.md'),
    read('../src/assets/site-profile-ai-prompt.txt'),
    read('../../../docs/sites/example.json'),
    read('../public/site-profile.example.json'),
  ])
  const [, documentedPrompt] = guide.match(/^```text\n([\s\S]*?)\n```$/m) ?? []
  assert.ok(documentedPrompt)
  assert.equal(prompt.trim(), documentedPrompt)
  assert.equal(webExample, docsExample)
  assert.equal(JSON.parse(webExample).schema_version, 1)
})
