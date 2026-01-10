// API 统一封装

const API_BASE = import.meta.env.VITE_API_BASE || ''

interface RequestOptions {
  method?: string
  body?: Record<string, unknown>
  headers?: Record<string, string>
}

async function request<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const token = localStorage.getItem('authToken')

  const headers: Record<string, string> = {
    'accept': 'application/json',
    ...options.headers
  }

  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  const response = await fetch(`${API_BASE}${url}`, {
    method: options.method || 'GET',
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined
  })

  if (!response.ok) {
    const error = await response.text()
    throw new Error(error || `HTTP ${response.status}`)
  }

  return response.json()
}

// 认证相关 API
export const authApi = {
  // 获取挑战
  getChallenge(address: string) {
    return request<{ nonce: string; message: string; expires_at: string }>(
      '/api/v1/public/common/auth/challenge',
      { method: 'POST', body: { address } }
    )
  },

  // 验证签名
  verifySignature(address: string, signature: string) {
    return request<{
      token: string
      expires_at: string
      user: { username: string; wallet_address: string; permissions: string[] }
    }>('/api/v1/public/common/auth/verify', {
      method: 'POST',
      body: { address, signature }
    })
  }
}

// 配额 API
export const quotaApi = {
  get() {
    return request<{
      quota: number
      used: number
      available: number
      percentage: number
      unlimited: boolean
    }>('/api/v1/public/webdav/quota')
  }
}