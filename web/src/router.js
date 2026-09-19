import { createRouter, createWebHashHistory } from 'vue-router'
import Dashboard from './views/Dashboard.vue'
import Assets from './views/Assets.vue'
import Devices from './views/Devices.vue'
import DeviceDetail from './views/DeviceDetail.vue'
import Changes from './views/Changes.vue'
import Organization from './views/Organization.vue'
import Software from './views/Software.vue'
import Settings from './views/Settings.vue'
import Users from './views/Users.vue'
import Login from './views/Login.vue'
import { getToken } from './api'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', component: Login },
    { path: '/dashboard', component: Dashboard },
    { path: '/assets', component: Assets },
    { path: '/devices', component: Devices },
    { path: '/devices/:id', component: DeviceDetail },
    { path: '/changes', component: Changes },
    { path: '/organization', component: Organization },
    { path: '/users', component: Users },
    { path: '/software', component: Software },
    { path: '/settings', component: Settings },
  ],
})

router.beforeEach((to, from, next) => {
  if (to.path !== '/login' && !getToken()) {
    next('/login')
  } else {
    next()
  }
})

export default router
