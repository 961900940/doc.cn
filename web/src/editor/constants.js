export const EDITOR_ENGINES = {
  bytemd: {
    id: 'bytemd',
    label: '简洁编辑',
    hint: '轻量分屏，适合写 Markdown 源码'
  },
  vditor: {
    id: 'vditor',
    label: '可视化编辑',
    hint: '工具栏更全，支持所见即所得'
  }
}

export const DEFAULT_EDITOR_ENGINE = 'vditor'
export const EDITOR_ENGINE_STORAGE_KEY = 'doc-system-editor-engine'

export function normalizeEditorEngine(value) {
  if (value === 'bytemd') return 'bytemd'
  if (value === 'vditor') return 'vditor'
  return DEFAULT_EDITOR_ENGINE
}

export function loadEditorEngine() {
  try {
    const saved = localStorage.getItem(EDITOR_ENGINE_STORAGE_KEY)
    return saved === null ? DEFAULT_EDITOR_ENGINE : normalizeEditorEngine(saved)
  } catch {
    return DEFAULT_EDITOR_ENGINE
  }
}

export function saveEditorEngine(engine) {
  const next = normalizeEditorEngine(engine)
  try {
    localStorage.setItem(EDITOR_ENGINE_STORAGE_KEY, next)
  } catch {
    // ignore quota / private mode errors
  }
  return next
}
