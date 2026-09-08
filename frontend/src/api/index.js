import request from './request'

// 用户
export const login = (data) => request.post('/users/login', data)
export const signup = (data) => request.post('/users/signup', data)
export const getProfile = () => request.get('/users/info')
export const updateInfo = (data) => request.post('/users/info', data)
// 生成画像（PUT） / 更新画像（POST）
export const generateSummary = () => request.put('/users/summary')
export const updateSummary = () => request.post('/users/summary')

// 推荐
export const getRecommend = () => request.get('/items/recommend')

// 交互（type: view/click/scroll/like）
export const addInteraction = (data) => request.post('/interactions', data)
