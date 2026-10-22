import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { revealDirective } from './directives/reveal'
// KaTeX 先引入，便于后面的 base.css 覆盖公式的字号/间距
import 'katex/dist/katex.min.css'
import './styles/base.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.directive('reveal', revealDirective)
app.mount('#app')
