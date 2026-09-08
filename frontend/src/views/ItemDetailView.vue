<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { addInteraction } from '../api'
import { useItemsStore } from '../stores/items'

const route = useRoute()
const router = useRouter()
const itemsStore = useItemsStore()

const item = ref(null)
const liked = ref(false)

onMounted(() => {
  // 后端暂无「按 id 查单条」接口，从内存 store 取完整 item
  item.value = itemsStore.byId(route.params.id) || null
  if (!item.value) {
    router.replace('/')
  }
})

function openOriginal() {
  // 后端 Item 目前没有原文 URL 字段，按标题构造 arXiv 检索链接作为兜底
  const title = encodeURIComponent(item.value?.title || '')
  const url = `https://arxiv.org/search/?searchtype=all&query=${title}`
  window.open(url, '_blank', 'noopener')
}

function like() {
  if (liked.value) return
  liked.value = true
  addInteraction({ type: 'like', target: item.value, detail: '详情页点赞', long_ts: 0 }).catch(() => {})
}
</script>

<template>
  <div v-if="item" class="detail-page">
    <header class="detail-head">
      <button class="ghost" @click="router.back()">← 返回</button>
      <div class="spacer"></div>
      <button class="primary link-btn" @click="openOriginal">🔗 跳转原文</button>
      <button class="primary like-btn" :class="{ liked }" :disabled="liked" @click="like">
        {{ liked ? '♥ 已点赞' : '♡ 点赞' }}
      </button>
    </header>

    <article class="detail">
      <div class="meta">
        <span class="source">{{ item.web_source || '未知来源' }}</span>
        <span class="date">{{ item.publish_date || '' }}</span>
      </div>

      <h1 class="title">{{ item.title }}</h1>
      <h2 v-if="item.title_chinese" class="title-zh">{{ item.title_chinese }}</h2>

      <p class="authors">{{ (item.authors || []).join('、') || '未知作者' }}</p>

      <div class="tags">
        <span v-for="t in item.fields || []" :key="'f' + t" class="tag">{{ t }}</span>
        <span v-for="t in item.keywords || []" :key="'k' + t" class="tag kw">{{ t }}</span>
      </div>

      <section>
        <h3>摘要</h3>
        <p class="abstract">{{ item.abstract || '暂无摘要' }}</p>
      </section>

      <section v-if="item.abstract_chinese">
        <h3>中文摘要</h3>
        <p class="abstract">{{ item.abstract_chinese }}</p>
      </section>

      <section v-if="item.summary && item.summary !== item.abstract">
        <h3>内容摘要</h3>
        <p class="abstract">{{ item.summary }}</p>
      </section>
    </article>
  </div>
</template>

<style scoped>
.detail-page {
  max-width: 820px;
  margin: 0 auto;
  padding: 24px 32px 60px;
}
.detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
  position: sticky;
  top: 0;
  background: var(--bg);
  padding: 12px 0;
}
.detail-head .spacer {
  flex: 1;
}
.link-btn,
.like-btn {
  width: auto;
  padding: 9px 18px;
  font-size: 14px;
}
.like-btn {
  background: #fff;
  color: var(--red);
  border: 1px solid var(--red);
}
.like-btn.liked,
.like-btn:disabled {
  background: #ffeef1;
  cursor: default;
}
.detail {
  background: #fff;
  border-radius: 16px;
  box-shadow: var(--shadow);
  padding: 28px 32px;
}
.meta {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 14px;
  font-size: 13px;
}
.source {
  background: #eef2ff;
  color: #4f46e5;
  padding: 3px 10px;
  border-radius: 999px;
}
.date {
  color: var(--text-3);
}
.title {
  margin: 0 0 8px;
  font-size: 24px;
  line-height: 1.4;
}
.title-zh {
  margin: 0 0 8px;
  font-size: 18px;
  color: var(--text-2);
  font-weight: 600;
}
.authors {
  color: var(--text-2);
  font-size: 14px;
  margin: 0 0 16px;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 24px;
}
.tag {
  font-size: 12px;
  padding: 3px 10px;
  border-radius: 999px;
  background: var(--bg);
  color: var(--text-2);
}
.tag.kw {
  background: #fff7ed;
  color: #c2410c;
}
section {
  margin-top: 24px;
}
section h3 {
  font-size: 16px;
  margin: 0 0 8px;
}
.abstract {
  margin: 0;
  line-height: 1.7;
  color: var(--text);
  white-space: pre-line;
}
</style>
