import { createRouter, createWebHashHistory } from 'vue-router'
import Devices from './views/Devices.vue'
import DeviceDetail from './views/DeviceDetail.vue'
import Changes from './views/Changes.vue'

export default createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/devices' },
    { path: '/devices', component: Devices },
    { path: '/devices/:id', component: DeviceDetail },
    { path: '/changes', component: Changes },
  ],
})
