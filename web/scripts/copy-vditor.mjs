import { cpSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const source = join(root, 'node_modules', 'vditor', 'dist')
const target = join(root, 'public', 'vditor', 'dist')

if (!existsSync(source)) {
  console.warn('[copy-vditor] vditor dist not found, skip')
  process.exit(0)
}

rmSync(join(root, 'public', 'vditor'), { recursive: true, force: true })
mkdirSync(dirname(target), { recursive: true })
cpSync(source, target, { recursive: true })
console.log('[copy-vditor] copied to public/vditor/dist')
