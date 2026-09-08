<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { getRecommend, addInteraction } from '../api'
import { useItemsStore } from '../stores/items'
import Sidebar from '../components/Sidebar.vue'
import ItemCard from '../components/ItemCard.vue'
import ProfileDrawer from '../components/ProfileDrawer.vue'

const router = useRouter()
const itemsStore = useItemsStore()

const items = ref([])
const loading = ref(false)
const error = ref('')
const showProfile = ref(false)

async function loadRecommend() {
  loading.value = true
  error.value = ''
  try {
    const res = await getRecommend()
    items.value = res.items || []
    itemsStore.setItems(items.value)
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function openItem(item) {
  // 记录 click 交互（失败不阻塞跳转）
  addInteraction({ type: 'click', target: item, detail: '点击卡片', long_ts: 0 }).catch(() => {})
  router.push(`/item/${item.id}`)
}

function likeItem(item) {
  addInteraction({ type: 'like', target: item, detail: '主页点赞', long_ts: 0 }).catch(() => {})
}

onMounted(loadRecommend)
</script>

<template>
  <div class="home">
    <Sidebar @refresh="loadRecommend" @profile="showProfile = true" />

    <main class="feed">
      <header class="feed-head">
        <div>
          <h2>为你推荐</h2>
          <p class="sub">基于你的学术画像，为你精选文献</p>
        </div>
        <button class="primary refresh" :disabled="loading" @click="loadRecommend">
          {{ loading ? '加载中…' : '换一批' }}
        </button>
      </header>

      <p v-if="error" class="error">{{ error }}</p>

      <div v-if="items.length" class="grid">
        <ItemCard
          v-for="item in items"
          :key="item.id"
          :item="item"
          @open="openItem"
          @like="likeItem"
        />
      </div>

      <div v-else-if="!loading" class="empty">
        <p>暂无推荐内容</p>
        <p class="hint">先在「我的信息」里完善资料并生成画像，再来获取推荐</p>
      </div>
    </main>

    <ProfileDrawer v-if="showProfile" @close="showProfile = false" />
  </div>
</template>

<style scoped>
.home {
  display: flex;
  min-height: 100vh;
}
.feed {
  flex: 1;
  padding: 28px 32px;
  max-width: 1200px;
}
.feed-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 20px;
}
.feed-head h2 {
  margin: 0;
  font-size: 24px;
}
.feed-head .sub {
  margin: 4px 0 0;
  color: var(--text-2);
  font-size: 13px;
}
.refresh {
  width: auto;
  padding: 9px 20px;
  font-size: 14px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}
.empty {
  text-align: center;
  color: var(--text-2);
  padding: 80px 0;
}
.empty .hint {
  font-size: 13px;
  color: var(--text-3);
}
</style>
