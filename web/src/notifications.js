// 站内信共享模块：顶栏铃铛收件箱（App.vue）与独立消息中心页
// （/notifications）共用——resource → 路由映射与类型中文映射禁止两处复制

// resource（通知的关联对象面）→ 前端路由；无映射的通知点击后只标已读不跳转
export const NOTIFICATION_ROUTES = {
  'asset-requests': '/asset-requests',
  assets: '/assets',
  consumables: '/consumables',
  licenses: '/software',
}

// 四类事件源中文映射（asset_request 设备申请 / asset_alert 资产告警 /
// consumable_low_stock 耗材预警 / license_expiring 许可到期）；未知类型原样展示
export const NOTIFICATION_TYPE_TEXTS = {
  asset_request: '设备申请',
  asset_alert: '资产告警',
  consumable_low_stock: '耗材预警',
  license_expiring: '许可到期',
}

// 类型标签色（消息中心页过滤与列表渲染用）
export const NOTIFICATION_TYPE_TAGS = {
  asset_request: 'primary',
  asset_alert: 'danger',
  consumable_low_stock: 'warning',
  license_expiring: 'warning',
}

export function notificationRoute(resource) {
  return NOTIFICATION_ROUTES[resource] || ''
}

export function notificationTypeText(type) {
  return NOTIFICATION_TYPE_TEXTS[type] || type
}

export function notificationTypeTag(type) {
  return NOTIFICATION_TYPE_TAGS[type] || 'info'
}
