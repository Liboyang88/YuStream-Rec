<script setup>
import SettingSwitch from './SettingSwitch.vue'
import { usePreferencesStore } from '@/stores/preferences'

/**
 * 「设置」子页面。
 * 目前只开放第一项展示偏好（卡片摘要取 summary 还是原文 abstract），
 * 其余设置项继续预留占位。
 */
const prefs = usePreferencesStore()
</script>

<template>
  <div class="settings">
    <section class="setting-card">
      <article class="setting-row">
        <div class="setting-text">
          <h3 class="setting-label">
            卡片优先展示
            <span class="setting-value">{{ prefs.preferSummary ? '二次概括 summary' : '原文 abstract' }}</span>
          </h3>
          <p class="setting-hint">
            开启时卡片摘要展示模型生成的二次概括 summary；关闭时展示论文原文 abstract。
            该偏好只影响推荐卡片的摘要文案，不影响详情页。
          </p>
        </div>
        <SettingSwitch
          v-model="prefs.preferSummary"
          size="sm"
          aria-label="卡片优先展示二次概括 summary"
        />
      </article>
    </section>

    <p class="setting-note">该模块已预留，后续将在此继续补充推荐偏好、通知与账号安全等选项。</p>
  </div>
</template>

<style scoped>
.settings {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 200px;
}

.setting-card {
  border-radius: var(--r-md);
  background: var(--surface-2);
  padding: 4px 14px;
}

.setting-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px 0;
}
.setting-row + .setting-row {
  border-top: 1px solid var(--border);
}

.setting-text {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}
.setting-label {
  display: flex;
  align-items: center;
  gap: 7px;
  flex-wrap: wrap;
  font-size: 13.5px;
  font-weight: 700;
}
.setting-value {
  padding: 2px 9px;
  border-radius: var(--r-pill);
  background: var(--brand-soft);
  color: var(--brand-dark);
  font-size: 11.5px;
  font-weight: 600;
}
.setting-hint {
  font-size: 12.5px;
  color: var(--text-3);
  line-height: 1.6;
}

.setting-note {
  font-size: 12px;
  color: var(--text-3);
  line-height: 1.6;
  padding: 0 2px;
}
</style>
