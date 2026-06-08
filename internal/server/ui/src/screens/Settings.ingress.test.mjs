import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const settingsSource = readFileSync(resolve(here, 'Settings.tsx'), 'utf8')
const querySource = readFileSync(resolve(here, '../lib/query.ts'), 'utf8')

test('settings screen renders live ingress status from the runtime endpoint', () => {
  assert.match(querySource, /function useIngressStatus\(/)
  assert.match(settingsSource, /useIngressStatus/)
  assert.match(settingsSource, /IngressStatusPanel/)
  assert.match(settingsSource, /base_url/)
  assert.match(settingsSource, /last_error/)
})
