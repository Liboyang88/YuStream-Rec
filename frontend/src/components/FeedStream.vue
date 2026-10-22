<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import ItemCard from './ItemCard.vue'
import FeedLoader from './FeedLoader.vue'
import AppIcon from './AppIcon.vue'
import { useFeedStore } from '@/stores/feed'
import { useUiStore } from '@/stores/ui'
import { INTERACTION, SCROLL_MAX_SECONDS, recordInteraction } from '@/api/interactions'

/**
 * 中间内容区的卡片流：
 * - 卡片宽度恒定（--card-w），列数随窗口宽度在 1~3 之间自适应
 * - 触底自动请求下一批并「顺序追加」，不重排已有卡片
 * - 卡片进入视口时浮现（v-reveal），新追加的卡片同样享受该动画
 */
const feed = useFeedStore()
const ui = useUiStore()

const gridRef = ref(null)
const sentinelRef = ref(null)
const columns = ref(3)

const CARD_WIDTH = 320
const GAP = 24
const MAX_COLUMNS = 3
/** 单次「填满视口」的连锁加载上限，避免后端持续返回新内容时无限请求 */
const MAX_CHAIN = 6

let resizeObserver = null
let sentinelObserver = null
let chainCount = 0
let scrollRoot = null
let scrollRaf = null
/** 到达底部后是否还「武装」着（离开底部会重新武装），用于一次下滑只触发一次 */
let bottomArmed = true
let lastRequestAt = 0

/** 距底部多少像素内视为「已经触碰到底部」 */
const BOTTOM_THRESHOLD = 320
/** 两次触底请求的最小间隔，避免一次连续滚动里重复发送 */
const MIN_REQUEST_INTERVAL = 800

const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${columns.value}, var(--card-w))`,
}))

function measureColumns() {
  const el = gridRef.value
  if (!el) return
  const width = el.clientWidth
  if (!width) return
  const fit = Math.floor((width + GAP) / (CARD_WIDTH + GAP))
  columns.value = Math.max(1, Math.min(MAX_COLUMNS, fit))
}

/** 同一排内的卡片错峰浮现，形成波浪感 */
function revealDelay(index) {
  return (index % columns.value) * 70
}

function openDetail(item) {
  ui.openDetail(item)
  // click：点击卡片进入详情页
  recordInteraction({ type: INTERACTION.CLICK, target: item, detail: 'card' }).catch(() => {})
  // 进入详情页即离开推荐卡片流，先结算本次 scroll 停留
  endScrollSession()
}

/** 卡片流所在的滚动容器（HomeView 上的 [data-feed-scroll]） */
function resolveScrollRoot() {
  if (scrollRoot && scrollRoot.isConnected) return scrollRoot
  scrollRoot = sentinelRef.value?.closest('[data-feed-scroll]') || null
  return scrollRoot
}

/* ---------- scroll 互动：浏览本次推荐集合的停留时长 ---------- */
/** 本次停留的开始时间戳（毫秒） */
let scrollStartedAt = null
/** 本次停留对应的推荐 outline（结算时作为 scroll 的 detail） */
let scrollOutline = ''
/** 本次停留对应的推荐来源（仅 source=llm 时才记录 scroll） */
let scrollSource = ''

/** 推荐卡片网格是否与滚动容器可视区相交（即用户正在浏览卡片群） */
function isGridVisible() {
  const grid = gridRef.value
  const root = resolveScrollRoot()
  if (!grid || !root) return false
  const gridRect = grid.getBoundingClientRect()
  const rootRect = root.getBoundingClientRect()
  return gridRect.bottom > rootRect.top && gridRect.top < rootRect.bottom
}

function startScrollSession() {
  if (scrollStartedAt != null) return
  scrollStartedAt = Date.now()
  scrollOutline = feed.outline || ''
  scrollSource = feed.source || ''
}

function endScrollSession() {
  if (scrollStartedAt == null) return
  const seconds = Math.min(
    SCROLL_MAX_SECONDS,
    Math.max(0, Math.round((Date.now() - scrollStartedAt) / 1000)),
  )
  // 仅 source=llm 时上报 scroll：target 为空，detail = outline，long_ts = 停留时长（秒，上限 5min）
  if (scrollSource === 'llm') {
    recordInteraction({ type: INTERACTION.SCROLL, target: null, detail: scrollOutline, long_ts: seconds }).catch(
      () => {},
    )
  }
  scrollStartedAt = null
  scrollOutline = ''
  scrollSource = ''
}

/**
 * 依据「是否正在浏览卡片群」「当前 source」「当前 outline」同步 scroll 会话：
 * - 离开卡片群（滚走 / 无内容 / 非 llm 来源）→ 结算本次停留
 * - 开始浏览卡片群且 source=llm → 开启新停留
 * - outline 变化（进入新一轮推荐）→ 先结算上一轮，再开启新一轮
 */
function syncScrollSession() {
  if (feed.count === 0 || !isGridVisible() || feed.source !== 'llm') {
    endScrollSession()
    return
  }
  const outline = feed.outline || ''
  if (scrollStartedAt == null) {
    startScrollSession()
  } else if (outline !== scrollOutline) {
    endScrollSession()
    startScrollSession()
  }
}

function isAtBottom(root) {
  return root.scrollHeight - root.scrollTop - root.clientHeight <= BOTTOM_THRESHOLD
}

/**
 * 到达底部 → 再发一次推荐请求。
 *
 * 触发来源有两个，都是「下滑到底部」这一动作：
 *   1) scroll 事件：滚动（滚轮/拖拽滚动条/程序化）到达底部
 *   2) wheel 事件：已经贴底时继续向下滚（此时不会产生 scroll 事件）
 *
 * 用固定冷却时间防止一次连续滚动里重复触发；请求进行中不重复发。
 * 注意：不会因为「上一轮没有新内容」而停止，每次下滑到底都会真实再请求一次。
 *
 * @returns {boolean} 是否真的发出了请求（只有真发出才消耗「已武装」状态）
 */
function requestMoreFromBottom() {
  if (feed.loading || feed.blocked) return false
  const now = Date.now()
  if (now - lastRequestAt < MIN_REQUEST_INTERVAL) return false
  lastRequestAt = now
  loadMoreAndFill()
  return true
}

/** 滚动事件在 rAF 中合并处理 */
function scheduleBottomCheck() {
  if (scrollRaf !== null) return
  scrollRaf = requestAnimationFrame(() => {
    scrollRaf = null
    syncScrollSession()
    const root = resolveScrollRoot()
    if (!root) return

    if (!isAtBottom(root)) {
      // 离开底部 → 重新武装，下次到达底部时再请求
      bottomArmed = true
      return
    }
    if (!bottomArmed) return
    // 关键：只有真的发出请求才消耗武装状态。
    // 否则挂载初期「哨兵短暂可见但请求正在飞行」会把武装状态白白吃掉，
    // 导致用户第一次下滑到底部时反而不发请求。
    if (requestMoreFromBottom()) bottomArmed = false
  })
}

/** 已贴底时继续向下滚：同样视为「一次下滑到底部」的动作 */
function onWheelAtBottom(event) {
  if (event.deltaY <= 0) return
  const root = resolveScrollRoot()
  if (!root) return
  const target = event.target
  if (target instanceof Node && !root.contains(target)) return
  if (!isAtBottom(root)) return
  if (requestMoreFromBottom()) bottomArmed = false
}

/**
 * 请求一批；只有在「确实拿到了新内容且仍处于底部」时才继续补齐，
 * 避免后端返回重复内容时形成空转请求。
 */
async function loadMoreAndFill() {
  const result = await feed.loadMore()
  await nextTick()

  if (!result || result.error || result.blocked) {
    chainCount = 0
    return
  }
  if (chainCount >= MAX_CHAIN) {
    chainCount = 0
    return
  }

  // 仅当本轮有新内容（列表变长、底部被推开）时才连锁补齐一屏
  const root = resolveScrollRoot()
  if (result.added > 0 && root && isAtBottom(root)) {
    chainCount += 1
    loadMoreAndFill()
  } else {
    chainCount = 0
  }
}

function handleRetry() {
  chainCount = 0
  bottomArmed = true
  lastRequestAt = 0
  feed.retry()
}

// 新一批内容 / 新 outline / 来源变化后，重新同步 scroll 会话（DOM 就绪后再算可见性）
watch(
  () => [feed.count, feed.outline, feed.source],
  () => {
    nextTick(() => syncScrollSession())
  },
)

// 打开详情 / 打开侧边浮窗 → 离开推荐卡片流，结算 scroll；返回时重新同步
watch(
  () => ui.activePanel || Boolean(ui.detailItem),
  (busy) => {
    if (busy) endScrollSession()
    else nextTick(() => syncScrollSession())
  },
)

onMounted(() => {
  measureColumns()

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(measureColumns)
    if (gridRef.value) resizeObserver.observe(gridRef.value)
  } else {
    window.addEventListener('resize', measureColumns)
  }

  if (typeof IntersectionObserver !== 'undefined' && sentinelRef.value) {
    sentinelObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) scheduleBottomCheck()
        }
      },
      { root: null, rootMargin: '260px 0px', threshold: 0 },
    )
    sentinelObserver.observe(sentinelRef.value)
  }

  // 滚动（含程序化跳转/拖拽滚动条）到达底部
  window.addEventListener('scroll', scheduleBottomCheck, { capture: true, passive: true })
  // 已贴底时继续向下滚（不产生 scroll 事件，需要单独监听）
  window.addEventListener('wheel', onWheelAtBottom, { capture: true, passive: true })

  feed.ensureLoaded()
  nextTick(() => syncScrollSession())
})

onBeforeUnmount(() => {
  if (resizeObserver) resizeObserver.disconnect()
  else window.removeEventListener('resize', measureColumns)
  if (sentinelObserver) sentinelObserver.disconnect()
  window.removeEventListener('scroll', scheduleBottomCheck, { capture: true })
  window.removeEventListener('wheel', onWheelAtBottom, { capture: true })
  if (scrollRaf !== null) cancelAnimationFrame(scrollRaf)
  // 离开页面：结算未完成的 scroll 停留
  endScrollSession()
})
</script>

<template>
  <div class="stream">
    <!-- 空状态：后端无候选/检索服务不可用 -->
    <div v-if="feed.isEmpty && !feed.loading" class="empty-state rise-in">
      <span class="empty-icon"><AppIcon name="compass" :size="26" /></span>
      <h3 class="empty-title">暂时没有可推荐的内容</h3>
      <p class="empty-text">
        {{ feed.error || '推荐服务暂时没有返回内容，可以稍后再试，或先去「我的」完善资料以提升推荐效果。' }}
      </p>
      <div class="empty-actions">
        <button type="button" class="btn btn-primary" @click="handleRetry">
          <AppIcon name="refresh" :size="16" />
          重新加载
        </button>
        <button type="button" class="btn btn-ghost" @click="ui.openPanel('profile')">
          <AppIcon name="user" :size="16" />
          完善我的资料
        </button>
      </div>
    </div>

    <!-- 卡片网格 -->
    <div v-else ref="gridRef" class="grid" :style="gridStyle">
      <ItemCard
        v-for="(item, index) in feed.items"
        :key="item.id != null ? `id-${item.id}` : `idx-${index}`"
        :item="item"
        :reveal-delay="revealDelay(index)"
        @open="openDetail"
      />
    </div>

    <!-- 首屏骨架 -->
    <div v-if="feed.loading && feed.count === 0" class="grid" :style="gridStyle">
      <div v-for="n in columns" :key="`sk-${n}`" class="skeleton-card">
        <span class="skeleton sk-badge" />
        <span class="skeleton sk-title" />
        <span class="skeleton sk-line" />
        <span class="skeleton sk-line short" />
      </div>
    </div>

    <!-- 触底哨兵 + 底部加载动画 -->
    <div ref="sentinelRef" class="sentinel" aria-hidden="true" />
    <FeedLoader
      :loading="feed.loading && feed.count > 0"
      :no-new-content="feed.noNewContent"
      :error="feed.error"
      :count="feed.count"
      @retry="handleRetry"
    />
  </div>
</template>

<style scoped>
.stream {
  width: 100%;
}

.grid {
  display: grid;
  justify-content: center;
  gap: var(--gap);
  width: 100%;
}

/* 骨架屏 */
.skeleton-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: var(--card-w);
  min-height: var(--card-min-h);
  padding: 18px;
  border-radius: var(--r-lg);
  background: var(--surface);
  border: 1px solid var(--border);
}
.sk-badge {
  width: 62px;
  height: 22px;
  border-radius: var(--r-pill);
}
.sk-title {
  width: 88%;
  height: 19px;
}
.sk-line {
  width: 100%;
  height: 12px;
}
.sk-line.short {
  width: 62%;
}

.sentinel {
  height: 1px;
  width: 100%;
}

/* 空状态 */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 12px;
  padding: 64px 32px;
  border-radius: var(--r-xl);
  background: var(--surface);
  border: 1px dashed var(--border-strong);
  max-width: 560px;
  margin: 0 auto;
}
.empty-icon {
  display: grid;
  place-items: center;
  width: 58px;
  height: 58px;
  border-radius: 50%;
  background: var(--brand-soft);
  color: var(--brand);
}
.empty-title {
  font-size: 17px;
  font-weight: 700;
}
.empty-text {
  font-size: 13.5px;
  color: var(--text-2);
  line-height: 1.65;
  max-width: 400px;
}
.empty-actions {
  display: flex;
  gap: 10px;
  margin-top: 6px;
  flex-wrap: wrap;
  justify-content: center;
}

@media (max-width: 720px) {
  .grid {
    gap: 16px;
  }
}
</style>
