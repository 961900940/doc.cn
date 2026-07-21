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
const shellRef = ref(null)
let editor = null
let syncing = false

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
  if (editor) {
    editor.destroy()
    editor = null
  }
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
})

onBeforeUnmount(() => {
  if (host.value) {
    host.value.removeEventListener('keydown', handleKeydown)
  }
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
      class="vditor-preview-wrap"
      :class="mode === 'preview' ? `preview-${previewWidth}` : ''"
    >
      <div ref="previewHost" class="vditor-reset vditor-preview-body" />
    </div>
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
  overflow: auto;
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
