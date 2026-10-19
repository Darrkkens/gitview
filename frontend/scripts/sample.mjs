// Extracts the data of a page exported by GitView (gitview graph export,
// audit export, study, runtime show -o…) into .samples/<name>.json, which
// `npm run dev` opens with ?sample=<name>.
//
//   npm run sample -- path/to/page.html name
import fs from 'node:fs'
import path from 'node:path'

const [file, name] = process.argv.slice(2)
if (!file || !name) {
  console.error('usage: npm run sample -- <page.html> <name>')
  process.exit(2)
}
const page = fs.readFileSync(file, 'utf8')
const block = (id) => {
  const m = page.match(new RegExp(`<script type="[^"]+" id="${id}">([\\s\\S]*?)</script>`))
  return m ? m[1] : ''
}
const data = block('gitview-data')
if (!data) {
  console.error(file + ': no gitview-data block (exported by an older GitView?)')
  process.exit(1)
}
const out = path.join(import.meta.dirname, '..', '.samples', name + '.json')
fs.mkdirSync(path.dirname(out), { recursive: true })
fs.writeFileSync(out, JSON.stringify({ envelope: JSON.parse(data), sources: block('gitview-sources') || undefined }))
console.log(`${out} (${(fs.statSync(out).size / 1024).toFixed(0)} KiB) · open http://localhost:5173/?sample=${name}`)
