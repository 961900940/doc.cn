<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
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
const plugins = [breaks(), gfm(), highlight(), mermaid()]

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
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})

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
  overflow: auto;
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
