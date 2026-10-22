import client from './client'

export const INTERACTION = {
  VIEW: 'view',
  CLICK: 'click',
  SCROLL: 'scroll',
  LIKE: 'like',
}

/** view 浏览时长上限（秒）：3 分钟 */
export const VIEW_MAX_SECONDS = 3 * 60
/** scroll 停留时长上限（秒）：5 分钟 */
export const SCROLL_MAX_SECONDS = 5 * 60

/**
 * 上报一次用户互动。
 * 后端结构：{ type, target: Item, detail, long_ts }
 * 互动上报失败不应打扰用户，因此调用方一律「即发即忘」。
 *
 * 各 type 的字段约定：
 * - view  ：target=被浏览的 item；detail 为空；long_ts=浏览时长（秒，上限 3min）
 * - click ：target=被点击的 item；detail=点击目标（card / link）；long_ts 为空
 * - scroll：target 为空；detail=本次推荐的 outline；long_ts=停留时长（秒，上限 5min）
 * - like  ：target=被收藏的 item；detail 为空；long_ts 为空
 *
 * @param {{ type: string, target?: object, detail?: string, long_ts?: number }} payload
 */
export function recordInteraction({ type, target, detail = '', long_ts = 0 } = {}) {
  
  if (type === INTERACTION.VIEW) {
    return client
    .post('/interactions', {
      type,
      target: target ?? {},
      long_ts: Math.min(long_ts, VIEW_MAX_SECONDS),
    })
    .then((res) => res.data)
  }
  if (type === INTERACTION.SCROLL) {
    return client
      .post('/interactions', {
        type,
        detail,
        long_ts: Math.min(long_ts, SCROLL_MAX_SECONDS),
      })
      .then((res) => res.data)
  }
  if (type === INTERACTION.LIKE) {
    return client
      .post('/interactions', {
        type,
        target: target ?? {},
      })
      .then((res) => res.data)
  }
  if (type === INTERACTION.CLICK) {
    return client
      .post('/interactions', {
        type,
        target: target ?? {},
        detail,
      })
      .then((res) => res.data)
  }
}
