import { defineStore } from 'pinia'

// 内存中缓存最近一次推荐结果，供详情页按 id 取完整 item。
// 后端目前没有「按 id 查单条」的接口，故用前端 store 传递。
export const useItemsStore = defineStore('items', {
  state: () => ({
    items: [],
  }),
  getters: {
    byId: (s) => (id) => s.items.find((i) => String(i.id) === String(id)),
  },
  actions: {
    setItems(list) {
      this.items = list || []
    },
  },
})
