import { describe, expect, it } from 'vitest'
import { utf8Length } from './format'

// go/token columns count UTF-8 bytes; the page measures JavaScript strings.
describe('utf8Length', () => {
  it('counts bytes as Go does', () => {
    const enc = new TextEncoder()
    for (const s of ['', 'return x', '\tif ok {', 'rescisão := 1', 'ü€', 'a😀b', 'é\t"ç"'])
      expect(utf8Length(s)).toBe(enc.encode(s).length)
  })
})
