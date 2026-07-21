/** 从 Markdown 源码解析文档大纲（跳过代码块内的 #） */
export function parseOutline(markdown) {
  const lines = String(markdown || '').split(/\r?\n/)
  const items = []
  let inFence = false
  let fenceChar = ''

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i]
    const fenceMatch = line.match(/^([`~]{3,})/)
    if (fenceMatch) {
      const marker = fenceMatch[1][0]
      if (!inFence) {
        inFence = true
        fenceChar = marker
      } else if (marker === fenceChar && /^[`~]{3,}\s*$/.test(line)) {
        inFence = false
        fenceChar = ''
      }
      continue
    }
    if (inFence) continue

    const match = line.match(/^(#{1,6})\s+(.+?)\s*#*\s*$/)
    if (!match) continue

    const text = cleanHeadingText(match[2])
    if (!text) continue

    items.push({
      id: `outline-${items.length}`,
      level: match[1].length,
      text,
      line: i + 1,
      lineIndex: i,
      index: items.length
    })
  }

  return items
}

function cleanHeadingText(raw) {
  return String(raw || '')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[*_`~]+/g, '')
    .replace(/^\s*\\?#+\s*/, '')
    .trim()
}

export const OUTLINE_VISIBLE_STORAGE_KEY = 'doc-system-outline-visible'

export function loadOutlineVisible() {
  try {
    const value = localStorage.getItem(OUTLINE_VISIBLE_STORAGE_KEY)
    if (value === null) return true
    return value !== '0'
  } catch {
    return true
  }
}

export function saveOutlineVisible(visible) {
  try {
    localStorage.setItem(OUTLINE_VISIBLE_STORAGE_KEY, visible ? '1' : '0')
  } catch {
    // ignore
  }
}
