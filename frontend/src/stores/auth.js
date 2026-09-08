import { defineStore } from 'pinia'
import { login, signup, getProfile } from '../api'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    accessToken: localStorage.getItem('accessToken') || '',
    refreshToken: localStorage.getItem('refreshToken') || '',
    profile: null,
  }),
  getters: {
    isAuthenticated: (s) => !!s.accessToken,
  },
  actions: {
    setTokens({ accessToken, refreshToken }) {
      this.accessToken = accessToken
      this.refreshToken = refreshToken
      localStorage.setItem('accessToken', accessToken)
      localStorage.setItem('refreshToken', refreshToken)
    },
    logout() {
      this.accessToken = ''
      this.refreshToken = ''
      this.profile = null
      localStorage.removeItem('accessToken')
      localStorage.removeItem('refreshToken')
    },
    async login(username, password) {
      const data = await login({ username, password })
      this.setTokens(data)
    },
    async signup(username, password) {
      const data = await signup({ username, password, repassword: password })
      this.setTokens(data)
    },
    async fetchProfile() {
      this.profile = await getProfile()
      return this.profile
    },
  },
})
