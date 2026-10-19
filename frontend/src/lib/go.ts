// A small Go tokenizer for the code views. Code is data: tokens are only
// ever rendered as text inside spans.

export type TokenClass = '' | 'kw' | 'type' | 'fn' | 'str' | 'num' | 'com' | 'bi' | 'p'
export type Token = [TokenClass, string]

const KW = new Set(
  'break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split(
    ' ',
  ),
)
const TYPES = new Set(
  'bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable'.split(
    ' ',
  ),
)
const BUILTIN = new Set(
  'append cap clear close complex copy delete imag len make max min new panic print println real recover true false iota nil'.split(' '),
)

const RULES: [string, RegExp][] = [
  ['ws', /\s+/y],
  ['com', /\/\/[^\n]*/y],
  ['com', /\/\*[\s\S]*?(?:\*\/|$)/y],
  ['str', /`[^`]*(?:`|$)/y],
  ['str', /"(?:[^"\\\n]|\\.)*(?:"|$)/y],
  ['str', /'(?:[^'\\\n]|\\.)*(?:'|$)/y],
  ['num', /(?:0[xXbBoO][0-9a-fA-F_]+|\d[\d_]*(?:\.[\d_]*)?(?:[eE][+-]?\d+)?|\.\d[\d_]*(?:[eE][+-]?\d+)?)i?/y],
  ['id', /[A-Za-z_À-￿][\wÀ-￿]*/y],
  ['p', /[^\s\w"'`]/y],
]

/** tokenize returns [class, text] pairs covering the whole input. */
export function tokenize(src: string): Token[] {
  const out: Token[] = []
  let pos = 0
  let prevId = ''
  while (pos < src.length) {
    let matched = false
    for (const [cls, re] of RULES) {
      re.lastIndex = pos
      const m = re.exec(src)
      if (!m || !m[0].length) continue
      const text = m[0]
      let c = cls
      if (cls === 'id') {
        if (KW.has(text)) c = 'kw'
        else if (TYPES.has(text)) c = 'type'
        else if (BUILTIN.has(text)) c = 'bi'
        else {
          // a call or a declaration: the next non-space character is "("
          let j = pos + text.length
          while (j < src.length && (src[j] === ' ' || src[j] === '\t')) j++
          if (src[j] === '(') c = 'fn'
          else if (prevId === 'type') c = 'type'
          else c = ''
        }
        prevId = text
      }
      out.push([(c === 'ws' ? '' : c) as TokenClass, text])
      pos += text.length
      matched = true
      break
    }
    if (!matched) {
      out.push(['', src[pos]])
      pos++
    }
  }
  return out
}

/** lines splits tokens at newlines: one array of tokens per line. */
export function tokenLines(tokens: Token[]): Token[][] {
  const rows: Token[][] = [[]]
  for (const [c, text] of tokens) {
    const parts = text.split('\n')
    parts.forEach((part, k) => {
      if (k) rows.push([])
      if (part) rows[rows.length - 1].push([c, part])
    })
  }
  return rows
}

const cache = new Map<string, Token[][]>()

/** highlightFile tokenizes a whole file once per path. */
export function highlightFile(path: string, text: string): Token[][] {
  let rows = cache.get(path)
  if (!rows) {
    rows = tokenLines(tokenize(text))
    cache.set(path, rows)
  }
  return rows
}

export function clearHighlightCache() {
  cache.clear()
}
