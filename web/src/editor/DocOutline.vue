<script setup>
defineProps({
  items: { type: Array, default: () => [] },
  activeId: { type: String, default: '' }
})

const emit = defineEmits(['select'])
</script>

<template>
  <aside class="doc-outline">
    <div class="doc-outline-header">文档大纲</div>
    <div v-if="!items.length" class="doc-outline-empty">暂无标题</div>
    <nav v-else class="doc-outline-nav">
      <button
        v-for="item in items"
        :key="item.id"
        type="button"
        class="outline-item"
        :class="[`level-${item.level}`, { active: item.id === activeId }]"
        :title="item.text"
        @click="emit('select', item)"
      >
        {{ item.text }}
      </button>
    </nav>
  </aside>
</template>

<style scoped>
.doc-outline {
  display: flex;
  flex-direction: column;
  width: 220px;
  min-width: 220px;
  height: 100%;
  border-left: 1px solid #dde3ee;
  background: #fbfcff;
}

.doc-outline-header {
  flex: 0 0 auto;
  padding: 12px 14px;
  font-size: 13px;
  font-weight: 700;
  color: #475569;
  border-bottom: 1px solid #eef2f8;
}

.doc-outline-empty {
  padding: 20px 14px;
  color: #94a3b8;
  font-size: 13px;
}

.doc-outline-nav {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  padding: 8px 0 16px;
}

.outline-item {
  display: block;
  width: 100%;
  border: 0;
  background: transparent;
  padding: 7px 14px;
  color: #334155;
  font-size: 13px;
  line-height: 1.45;
  text-align: left;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.outline-item:hover {
  background: #eef2f8;
  color: #0f172a;
}

.outline-item.active {
  background: #e8eefc;
  color: #1d4ed8;
  font-weight: 600;
}

.outline-item.level-1 {
  padding-left: 14px;
  font-weight: 600;
}

.outline-item.level-2 {
  padding-left: 24px;
}

.outline-item.level-3 {
  padding-left: 34px;
}

.outline-item.level-4 {
  padding-left: 44px;
}

.outline-item.level-5 {
  padding-left: 54px;
}

.outline-item.level-6 {
  padding-left: 64px;
}

@media (max-width: 900px) {
  .doc-outline {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 5;
    width: min(240px, 78vw);
    min-width: 0;
    box-shadow: -8px 0 24px rgb(29 36 51 / 10%);
  }
}
</style>
