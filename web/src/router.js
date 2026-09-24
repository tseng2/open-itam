import { createRouter, createWebHashHistory } from 'vue-router'
import Dashboard from './views/Dashboard.vue'
import Assets from './views/Assets.vue'
import Dispatches from './views/Dispatches.vue'
import Stocktakes from './views/Stocktakes.vue'
import AssetRequests from './views/AssetRequests.vue'
import Devices from './views/Devices.vue'
import DeviceDetail from './views/DeviceDetail.vue'
import Changes from './views/Changes.vue'
import Organization from './views/Organization.vue'
import Software from './views/Software.vue'
import Settings from './views/Settings.vue'
import Users from './views/Users.vue'
import Login from './views/Login.vue'
import MobileStocktake from './views/MobileStocktake.vue'
import { getToken } from './api'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', component: Login },
    { path: '/dashboard', component: Dashboard },
    { path: '/assets', component: Assets },
    { path: '/dispatches', component: Dispatches },
    { path: '/stocktakes', component: Stocktakes },
    { path: '/asset-requests', component: AssetRequests },
    { path: '/devices', component: Devices },
    { path: '/devices/:id', component: DeviceDetail },
    { path: '/changes', component: Changes },
    { path: '/organization', component: Organization },
    { path: '/users', component: Users },
    { path: '/software', component: Software },
    { path: '/settings', component: Settings },
    // 免登录移动扫码面（P0-β）：盘点码承担鉴权，不走 JWT（详见 MobileStocktake.vue）
    { path: '/a/:number', component: MobileStocktake, meta: { public: true, mobile: true } },
    { path: '/m/scan', component: MobileStocktake, meta: { public: true, mobile: true } },
    { path: '/m/t/:token', component: MobileStocktake, meta: { public: true, mobile: true } },
  ],
})

router.beforeEach((to, from, next) => {
  // 公开路由（移动扫码页）：跳过登录态校验
  if (to.meta.public) {
    next()
    return
  }
  if (to.path !== '/login' && !getToken()) {
    next('/login')
  } else {
    next()
  }
})

export default router
