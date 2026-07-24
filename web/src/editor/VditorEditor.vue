<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Vditor from 'vditor'
import 'vditor/dist/index.css'
import { uploadFile } from '../api'

const props = defineProps({
  modelValue: { type: String, default: '' },
  mode: { type: String, default: 'split' },
  previewWidth: { type: String, default: 'default' },
  readonly: { type: Boolean, default: false }
})

const emit = defineEmits(['update:modelValue', 'save'])

const host = ref(null)
const previewHost = ref(null)
const previewWrapRef = ref(null)
const shellRef = ref(null)
const showBackTop = ref(false)
let editor = null
let syncing = false
let scrollTarget = null
let resizeObserver = null

const vditorCdn = `${import.meta.env.BASE_URL}vditor`.replace(/\/$/, '')

function mapMode(mode) {
  if (mode === 'edit') {
    return { mode: 'wysiwyg', previewMode: 'editor' }
  }
  if (mode === 'preview') {
    return { mode: 'sv', previewMode: 'both' }
  }
  return { mode: 'sv', previewMode: 'both' }
}

async function customUpload(files) {
  const snippets = []
  for (const file of files) {
    const uploaded = await uploadFile(file)
    const image = file.type.startsWith('image/')
    snippets.push(image ? `![${uploaded.name}](${uploaded.url})` : `[${uploaded.name}](${uploaded.url})`)
  }
  return snippets.join('\n\n')
}

function destroyEditor() {
  detachScrollTarget()
  if (editor) {
    editor.destroy()
    editor = null
  }
}

function getScrollableCandidates() {
  const root = shellRef.value
  if (!root) return []
  if (props.readonly || props.mode === 'preview') {
    return [root, previewWrapRef.value].filter(Boolean)
  }
  return [
    root.querySelector('.vditor-sv'),
    root.querySelector('.vditor-wysiwyg'),
    root.querySelector('.vditor-ir'),
    root.querySelector('.vditor-preview'),
    root.querySelector('.vditor-content')
  ].filter(Boolean)
}

function isScrollable(element) {
  return element && element.scrollHeight - element.clientHeight > 8
}

function pickScrollTarget() {
  const candidates = getScrollableCandidates()
  return candidates.find(isScrollable) || candidates[0] || null
}

function updateBackTopState() {
  const target = pickScrollTarget()
  if (target !== scrollTarget) {
    attachScrollTarget(target)
    return
  }
  showBackTop.value = Boolean(target && isScrollable(target) && target.scrollTop > 180)
}

function detachScrollTarget() {
  if (scrollTarget) {
    scrollTarget.removeEventListener('scroll', updateBackTopState)
    scrollTarget = null
  }
}

function attachScrollTarget(target = pickScrollTarget()) {
  detachScrollTarget()
  scrollTarget = target
  if (scrollTarget) {
    scrollTarget.addEventListener('scroll', updateBackTopState, { passive: true })
  }
  showBackTop.value = Boolean(scrollTarget && isScrollable(scrollTarget) && scrollTarget.scrollTop > 180)
}

async function refreshBackTop() {
  await nextTick()
  requestAnimationFrame(() => {
    attachScrollTarget()
  })
}

function scrollBackToTop() {
  const target = scrollTarget || pickScrollTarget()
  if (!target) return
  target.scrollTo({ top: 0, behavior: 'smooth' })
}

async function createEditor() {
  destroyEditor()
  if (!host.value || props.readonly || props.mode === 'preview') return

  const mapped = mapMode(props.mode)
  await nextTick()

  editor = new Vditor(host.value, {
    cdn: vditorCdn,
    value: props.modelValue || '',
    height: '100%',
    mode: mapped.mode,
    lang: 'zh_CN',
    cache: { enable: false },
    toolbarConfig: {
      pin: true
    },
    preview: {
      mode: mapped.previewMode,
      hljs: {
        lineNumber: true,
        style: 'github'
      }
    },
    upload: {
      accept: 'image/*,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.zip,.txt,.md,.csv',
      multiple: false,
      handler(files) {
        return customUpload(Array.from(files)).then((markdown) => {
          editor?.insertValue(markdown)
          return null
        })
      }
    },
    input(value) {
      if (syncing) return
      emit('update:modelValue', value)
    },
    after() {
      if (props.mode === 'edit') {
        editor?.setPreviewMode('editor')
      } else if (props.mode === 'split') {
        editor?.setPreviewMode('both')
      }
      if (props.readonly) {
        editor?.disabled()
      }
      refreshBackTop()
    },
    ctrlEnter: () => {},
    hint: {
      emoji: {}
    }
  })

  // Ctrl/Cmd + S
  host.value.addEventListener('keydown', handleKeydown)
}

function handleKeydown(event) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    emit('save')
  }
}

async function renderPreviewOnly() {
  destroyEditor()
  if (!previewHost.value) return
  previewHost.value.innerHTML = ''
  await Vditor.preview(previewHost.value, props.modelValue || '', {
    cdn: vditorCdn,
    mode: 'light',
    hljs: {
      lineNumber: true,
      style: 'github'
    }
  })
  await refreshBackTop()
}

async function mountCurrent() {
  if (props.readonly || props.mode === 'preview') {
    await nextTick()
    await renderPreviewOnly()
    return
  }
  await createEditor()
}

watch(
  () => props.modelValue,
  (value) => {
    if (!editor) {
      if (props.readonly || props.mode === 'preview') {
        renderPreviewOnly()
      }
      return
    }
    const current = editor.getValue()
    if (value !== current) {
      syncing = true
      editor.setValue(value || '', true)
      syncing = false
      refreshBackTop()
    }
  }
)

watch(
  () => [props.mode, props.readonly],
  async () => {
    await mountCurrent()
  }
)

onMounted(async () => {
  await mountCurrent()
  resizeObserver = new ResizeObserver(updateBackTopState)
  if (shellRef.value) resizeObserver.observe(shellRef.value)
  if (previewWrapRef.value) resizeObserver.observe(previewWrapRef.value)
  window.addEventListener('resize', updateBackTopState)
})

onBeforeUnmount(() => {
  if (host.value) {
    host.value.removeEventListener('keydown', handleKeydown)
  }
  window.removeEventListener('resize', updateBackTopState)
  resizeObserver?.disconnect()
  detachScrollTarget()
  destroyEditor()
})

defineExpose({
  insertMarkdown(snippet) {
    if (editor) {
      editor.insertValue(snippet)
      return
    }
    emit('update:modelValue', `${props.modelValue || ''}\n\n${snippet}\n`)
  },
  scrollToHeading(item) {
    if (!item) return
    const root = shellRef.value
    if (!root) return

    const containers = root.querySelectorAll(
      '.vditor-preview, .vditor-preview-body, .vditor-wysiwyg, .vditor-ir, .vditor-reset'
    )
    for (const container of containers) {
      const headings = container.querySelectorAll('h1, h2, h3, h4, h5, h6')
      const target = headings[item.index]
      if (target) {
        target.scrollIntoView({ behavior: 'smooth', block: 'start' })
        return
      }
    }

    const sv = root.querySelector('.vditor-sv')
    if (sv) {
      const lineHeight = 22
      sv.scrollTop = Math.max(0, item.lineIndex * lineHeight - 80)
      editor?.focus()
    }
  }
})
</script>

<template>
  <div ref="shellRef" class="vditor-shell" :class="[`mode-${mode}`, { 'is-readonly': readonly }]">
    <div
      v-show="!readonly && mode !== 'preview'"
      ref="host"
      class="vditor-host"
    />
    <div
      v-show="readonly || mode === 'preview'"
      ref="previewWrapRef"
      class="vditor-preview-wrap"
      :class="mode === 'preview' ? `preview-${previewWidth}` : ''"
    >
      <div ref="previewHost" class="vditor-reset vditor-preview-body" />
    </div>
    <button
      v-show="showBackTop"
      type="button"
      class="doc-back-top"
      aria-label="回到顶部"
      title="回到顶部"
      @click="scrollBackToTop"
    >
      ↑
    </button>
  </div>
</template>

<style scoped>
.vditor-shell {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: #fff;
}

.vditor-host {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  min-height: 0;
}

.vditor-shell.mode-preview {
  overflow: hidden;
  padding: 28px;
  background: #f5f7fb;
}

.vditor-preview-wrap {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  background: #fff;
}

.vditor-shell.mode-preview .vditor-preview-wrap {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 100%;
  margin: 0 auto;
  border: 1px solid #dde3ee;
  border-radius: 8px;
  box-shadow: 0 12px 34px rgb(29 36 51 / 8%);
}

.vditor-preview-wrap.preview-narrow {
  max-width: clamp(760px, 52%, 1120px);
}

.vditor-preview-wrap.preview-default {
  max-width: clamp(960px, 68%, 1480px);
}

.vditor-preview-wrap.preview-wide {
  max-width: clamp(1280px, 86%, 1880px);
}

.vditor-preview-body {
  padding: 24px 32px 80px;
  line-height: 1.75;
}

.doc-back-top {
  position: absolute;
  right: 24px;
  bottom: 24px;
  z-index: 20;
  width: 42px;
  height: 42px;
  border: 1px solid #d8e0ec;
  border-radius: 50%;
  background: #fff;
  color: #1f6feb;
  box-shadow: 0 10px 28px rgb(15 23 42 / 16%);
  cursor: pointer;
  font-size: 22px;
  line-height: 1;
}

.doc-back-top:hover {
  border-color: #1f6feb;
  background: #eff6ff;
}
</style>

<style>
/* 第三方编辑器 DOM 不带 scoped attribute，用组件根类限定，避免 :deep */
.vditor-shell .vditor {
  width: 100% !important;
  height: 100% !important;
  border: 0;
  border-radius: 0;
}

.vditor-shell .vditor-content {
  min-height: 0;
}

.vditor-preview-body img {
  max-width: 100%;
}
</style>
