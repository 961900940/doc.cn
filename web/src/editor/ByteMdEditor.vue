<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Editor, Viewer } from '@bytemd/vue-next'
import breaks from '@bytemd/plugin-breaks'
import gfm from '@bytemd/plugin-gfm'
import highlight from '@bytemd/plugin-highlight'
import mermaid from '@bytemd/plugin-mermaid'
import zhHans from 'bytemd/locales/zh_Hans.json'
import 'bytemd/dist/index.css'
import 'highlight.js/styles/github.css'
import { uploadFile } from '../api'

const props = defineProps({
  modelValue: { type: String, default: '' },
  mode: { type: String, default: 'split' },
  previewWidth: { type: String, default: 'default' },
  readonly: { type: Boolean, default: false }
})

const emit = defineEmits(['update:modelValue', 'save'])

const shellRef = ref(null)
const showBackTop = ref(false)
const plugins = [breaks(), gfm(), highlight(), mermaid()]
let scrollTarget = null
let resizeObserver = null

const showEditor = computed(() => !props.readonly && props.mode !== 'preview')
const showViewer = computed(() => props.readonly || props.mode === 'preview')

function handleChange(value) {
  emit('update:modelValue', value)
}

async function uploadImages(files) {
  const results = []
  for (const file of files) {
    const uploaded = await uploadFile(file)
    results.push({
      url: uploaded.url,
      alt: uploaded.name,
      title: uploaded.name
    })
  }
  return results
}

function handleKeydown(event) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
    event.preventDefault()
    emit('save')
  }
}

function getScrollableCandidates() {
  const root = shellRef.value
  if (!root) return []
  if (showViewer.value) {
    return [root, root.querySelector('.byte-md-viewer')].filter(Boolean)
  }
  return [
    root.querySelector('.CodeMirror-scroll'),
    root.querySelector('.bytemd-preview'),
    root.querySelector('.bytemd-body')
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

function scrollPreviewHeading(item) {
  const root = shellRef.value
  if (!root) return false
  const containers = [...root.querySelectorAll('.bytemd-preview, .byte-md-viewer')].filter((el) => {
    const style = window.getComputedStyle(el)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
  for (const container of containers) {
    const headings = container.querySelectorAll('h1, h2, h3, h4, h5, h6')
    const target = headings[item.index]
    if (target) {
      target.scrollIntoView({ behavior: 'smooth', block: 'start' })
      return true
    }
  }
  return false
}

function scrollSourceHeading(item) {
  const root = shellRef.value
  if (!root) return false
  const cmHost = root.querySelector('.CodeMirror')
  const cm = cmHost?.CodeMirror
  if (!cm) return false
  cm.focus()
  cm.setCursor({ line: item.lineIndex, ch: 0 })
  cm.scrollIntoView({ line: item.lineIndex, ch: 0 }, 100)
  return true
}

function scrollToHeading(item) {
  if (!item) return
  if (props.mode !== 'edit' && scrollPreviewHeading(item)) return
  scrollSourceHeading(item)
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
  refreshBackTop()
  resizeObserver = new ResizeObserver(updateBackTopState)
  if (shellRef.value) resizeObserver.observe(shellRef.value)
  window.addEventListener('resize', updateBackTopState)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
  window.removeEventListener('resize', updateBackTopState)
  resizeObserver?.disconnect()
  detachScrollTarget()
})

watch(
  () => [props.modelValue, props.mode, props.readonly],
  () => {
    refreshBackTop()
  }
)

defineExpose({
  insertMarkdown(snippet) {
    emit('update:modelValue', `${props.modelValue || ''}\n\n${snippet}\n`)
  },
  scrollToHeading
})
</script>

<template>
  <div ref="shellRef" class="byte-md-shell" :class="[`mode-${mode}`, { 'is-readonly': readonly }]">
    <Editor
      v-if="showEditor"
      :value="modelValue"
      :plugins="plugins"
      :locale="zhHans"
      mode="split"
      :upload-images="uploadImages"
      @change="handleChange"
    />
    <Viewer
      v-if="showViewer"
      class="byte-md-viewer"
      :class="`preview-${previewWidth}`"
      :value="modelValue"
      :plugins="plugins"
    />
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
.byte-md-shell {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  background: #fff;
}

.byte-md-shell.mode-preview {
  overflow: hidden;
  padding: 28px;
  background: #f5f7fb;
}

.byte-md-viewer {
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: 24px 32px 80px;
  background: #fff;
  line-height: 1.75;
}

.byte-md-shell.mode-preview .byte-md-viewer {
  width: 100%;
  height: 100%;
  min-height: 100%;
  margin: 0 auto;
  border: 1px solid #dde3ee;
  border-radius: 8px;
  box-shadow: 0 12px 34px rgb(29 36 51 / 8%);
}

.byte-md-viewer.preview-narrow {
  max-width: clamp(760px, 52%, 1120px);
}

.byte-md-viewer.preview-default {
  max-width: clamp(960px, 68%, 1480px);
}

.byte-md-viewer.preview-wide {
  max-width: clamp(1280px, 86%, 1880px);
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
.byte-md-shell .bytemd {
  position: absolute !important;
  inset: 0 !important;
  width: 100% !important;
  height: 100% !important;
  max-height: none !important;
  border: 0 !important;
  border-radius: 0 !important;
}

.byte-md-shell .bytemd-body {
  height: calc(100% - 58px) !important;
}

.byte-md-shell .CodeMirror,
.byte-md-shell .CodeMirror-scroll {
  height: 100% !important;
  min-height: 100% !important;
}

.byte-md-shell.mode-edit .bytemd-preview {
  display: none;
}

.byte-md-shell.mode-edit .bytemd-editor {
  width: 100%;
  border-right: 0;
}

.byte-md-viewer img {
  max-width: 100%;
}
</style>
