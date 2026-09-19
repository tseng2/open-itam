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

export function timeAgo(ts) {
  if (!ts) return '-'
  const diff = (Date.now() - new Date(ts).getTime()) / 1000
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}
