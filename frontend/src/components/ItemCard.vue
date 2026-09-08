<script setup>
defineProps({
  item: { type: Object, required: true },
})
defineEmits(['open', 'like'])
</script>

<template>
  <article class="card" @click="$emit('open', item)">
    <div class="card-top">
      <span class="source">{{ item.web_source || '未知来源' }}</span>
      <span class="date">{{ item.publish_date || '' }}</span>
    </div>

    <h3 class="title clamp-2">{{ item.title || '（无标题）' }}</h3>
    <p class="authors clamp-1">{{ (item.authors || []).join(', ') || '未知作者' }}</p>
    <p class="abstract clamp-3">{{ item.abstract || item.summary || '暂无摘要' }}</p>

    <div class="tags">
      <span v-for="t in (item.fields || []).slice(0, 3)" :key="'f' + t" class="tag">{{ t }}</span>
      <span v-for="t in (item.keywords || []).slice(0, 3)" :key="'k' + t" class="tag kw">{{ t }}</span>
    </div>

    <div class="card-foot">
      <span v-if="item.is_chinese" class="zh">中</span>
      <span class="spacer"></span>
      <button class="like" @click.stop="$emit('like', item)">♥ 点赞</button>
    </div>
  </article>
</template>

<style scoped>
.card {
  height: 250px;
  display: flex;
  flex-direction: column;
  background: var(--card-bg);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 16px;
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
  overflow: hidden;
}
.card:hover {
  transform: translateY(-3px);
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.1);
}
.card-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  color: var(--text-3);
  margin-bottom: 8px;
}
.source {
  background: #eef2ff;
  color: #4f46e5;
  padding: 2px 8px;
  border-radius: 999px;
}
.title {
  margin: 0 0 6px;
  font-size: 15px;
  font-weight: 700;
  line-height: 1.4;
}
.authors {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--text-2);
}
.abstract {
  margin: 0;
  font-size: 13px;
  color: var(--text-2);
  line-height: 1.55;
  flex: 1;
}
.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}
.tag {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: var(--bg);
  color: var(--text-2);
}
.tag.kw {
  background: #fff7ed;
  color: #c2410c;
}
.card-foot {
  display: flex;
  align-items: center;
  margin-top: 10px;
}
.zh {
  font-size: 11px;
  color: var(--red);
  border: 1px solid var(--red);
  border-radius: 4px;
  padding: 0 4px;
}
.spacer {
  flex: 1;
}
.like {
  border: 1px solid var(--border);
  background: #fff;
  color: var(--text-2);
  border-radius: 999px;
  padding: 5px 12px;
  font-size: 13px;
}
.like:hover {
  border-color: var(--red);
  color: var(--red);
}
</style>
