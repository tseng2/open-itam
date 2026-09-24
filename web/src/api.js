export function getToken() {
  return localStorage.getItem('itagent_admin_token') || ''
}

export function setToken(t) {
  localStorage.setItem('itagent_admin_token', t)
}

export async function api(path, opts = {}) {
  const resp = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getToken()}`,
      ...(opts.headers || {}),
    },
  })
  if (resp.status === 401) {
    window.location.hash = '#/login'
    throw new Error('unauthorized')
  }
  const data = await resp.json()
  if (data.code !== 0) throw new Error(data.message || 'server error')
  return data
}

// 免登录公开面（P0-β 移动扫码页）：不带 JWT，401 不跳登录；
// 鉴权由路径中的盘点码承担，配合服务端 IP 限流
export async function pubApi(path, opts = {}) {
  const resp = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      ...(opts.headers || {}),
    },
  })
  const data = await resp.json()
  if (data.code !== 0) throw new Error(data.message || 'server error')
  return data
}

// 需登录的二进制下载（台账 Excel 导出/导入模板）：带 JWT，返回 blob；
// 非 2xx 时尝试解析 JSON 错误信封取 message
export async function apiBlob(path) {
  const resp = await fetch(path, {
    headers: { Authorization: `Bearer ${getToken()}` },
  })
  if (resp.status === 401) {
    window.location.hash = '#/login'
    throw new Error('unauthorized')
  }
  if (!resp.ok) {
    let msg = '请求失败'
    try {
      const data = await resp.json()
      if (data.message) msg = data.message
    } catch { /* 非 JSON 错误体（二进制/空响应） */ }
    throw new Error(msg)
  }
  return resp.blob()
}

// multipart 上传（台账 Excel 导入）：浏览器自动生成 boundary，勿手设 Content-Type；
// 返回原始响应 JSON（含非零 code 的行级错误明细），由调用方判 code
export async function apiUpload(path, formData) {
  const resp = await fetch(path, {
    method: 'POST',
    headers: { Authorization: `Bearer ${getToken()}` },
    body: formData,
  })
  if (resp.status === 401) {
    window.location.hash = '#/login'
    throw new Error('unauthorized')
  }
  return resp.json()
}

export function timeAgo(ts) {
  if (!ts) return '-'
  const diff = (Date.now() - new Date(ts).getTime()) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}
