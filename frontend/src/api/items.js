import client from './client'

/**
 * 拉取一批推荐内容。
 * 响应：{ items: ItemsDetail[], source: 'llm' | 'coarse'，outline: string }
 * 说明：后端为「流式/滚动窗口」式推荐，没有游标分页；重复请求即取下一批，
 * 前端按 id 去重后追加，保证已有卡片顺序不变。
 */
export function fetchRecommendations(params = {}) {
  return client.get('/items/recommend', { params }).then((res) => res.data)
}
