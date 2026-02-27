// API 统一封装
import { authFetch, getAccessToken } from '@yeying-community/web3-bs'

const API_BASE = import.meta.env.VITE_API_BASE || ''
const AUTH_BASE = API_BASE ? `${API_BASE.replace(/\/+$/, '')}/api/v1/public/auth` : '/api/v1/public/auth'

interface RequestOptions {
  method?: string
  body?: Record<string, unknown> | FormData
  headers?: Record<string, string>
}

async function request<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    'accept': 'application/json',
    ...options.headers
  }

  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  const response = await authFetch(`${API_BASE}${url}`, {
    method: options.method || 'GET',
    headers,
    body: options.body instanceof FormData ? options.body : (options.body ? JSON.stringify(options.body) : undefined)
  }, {
    baseUrl: AUTH_BASE,
    accessToken: getAccessToken()
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
    return request<{
      code: number
      message: string
      data: {
        address: string
        challenge: string
        nonce: string
        issuedAt: number
        expiresAt: number
      }
      timestamp: number
    }>(
      '/api/v1/public/auth/challenge',
      { method: 'POST', body: { address } }
    )
  },

  // 验证签名
  verifySignature(address: string, signature: string) {
    return request<{
      code: number
      message: string
      data: {
        address: string
        token: string
        expiresAt: number
        refreshExpiresAt: number
      }
      timestamp: number
    }>('/api/v1/public/auth/verify', {
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

// 用户信息 API
export const userApi = {
  getInfo() {
    return request<{
      username: string
      wallet_address?: string
      email?: string
      permissions: string[]
      created_at?: string
      updated_at?: string
      has_password?: boolean
    }>('/api/v1/public/webdav/user/info')
  },

  updateUsername(username: string) {
    return request<{
      username: string
    }>('/api/v1/public/webdav/user/update', {
      method: 'POST',
      body: { username }
    })
  },

  updatePassword(oldPassword: string | null, newPassword: string) {
    return request('/api/v1/public/webdav/user/password', {
      method: 'POST',
      body: { oldPassword, newPassword }
    })
  }
}

export interface AssetSpaceInfo {
  key: string
  name: string
  path: string
}

export const assetsApi = {
  async getSpaces() {
    const response = await request<{
      code: number
      message: string
      data?: {
        defaultSpace?: string
        spaces?: AssetSpaceInfo[]
      }
      timestamp: number
    }>('/api/v1/public/assets/spaces')

    const data = response?.data || {}
    return {
      defaultSpace: data.defaultSpace || 'personal',
      spaces: Array.isArray(data.spaces) ? data.spaces : []
    }
  }
}

// 回收站项目类型
export interface RecycleItem {
  hash: string
  name: string
  path: string        // 删除前的完整路径（相对于目录根）
  size: number
  deletedAt: string   // 删除时间
  directory: string   // 所在目录
  isDir?: boolean
}

// 回收站 API
export const recycleApi = {
  // 获取回收站列表（全局）
  list() {
    return request<{
      items: RecycleItem[]
    }>('/api/v1/public/webdav/recycle/list')
  },

  // 恢复文件到原始目录
  recover(hash: string) {
    return request('/api/v1/public/webdav/recycle/recover', {
      method: 'POST',
      body: { hash }
    })
  },

  // 永久删除
  remove(hash: string) {
    return request('/api/v1/public/webdav/recycle/permanent', {
      method: 'DELETE',
      body: { hash }
    })
  },

  clear() {
    return request<{ deleted: number }>('/api/v1/public/webdav/recycle/clear', {
      method: 'DELETE'
    })
  }
}

// 分享项目类型
export interface ShareItem {
  token: string
  name: string
  path: string
  url: string
  viewCount: number
  downloadCount: number
  expiresAt?: string
  createdAt?: string
}

// 定向分享项目类型
export interface DirectShareItem {
  id: string
  name: string
  path: string
  isDir: boolean
  permissions: string[]
  targetWallet?: string
  ownerWallet?: string
  ownerName?: string
  expiresAt?: string
  createdAt: string
}

export interface ShareEntryItem {
  name: string
  path: string
  isDir: boolean
  size: number
  modified: string
}

export interface AddressGroup {
  id: string
  name: string
  createdAt?: string
}

export interface AddressContact {
  id: string
  name: string
  walletAddress: string
  groupId?: string
  tags?: string[]
  createdAt?: string
}

// 分享 API
export const shareApi = {
  create(path: string, expiresIn?: number) {
    return request<{
      token: string
      name: string
      path: string
      url: string
      viewCount: number
      downloadCount: number
      expiresAt?: string
    }>('/api/v1/public/share/create', {
      method: 'POST',
      body: { path, expiresIn }
    })
  },

  list() {
    return request<{
      items: ShareItem[]
    }>('/api/v1/public/share/list')
  },

  revoke(token: string) {
    return request('/api/v1/public/share/revoke', {
      method: 'POST',
      body: { token }
    })
  }
}

// 定向分享 API
export const directShareApi = {
  create(payload: {
    path: string
    targetAddress: string
    permissions: string[]
    expiresIn?: number
  }) {
    return request<DirectShareItem>('/api/v1/public/share/user/create', {
      method: 'POST',
      body: payload
    })
  },

  listMine() {
    return request<{
      items: DirectShareItem[]
    }>('/api/v1/public/share/user/list')
  },

  listReceived() {
    return request<{
      items: DirectShareItem[]
    }>('/api/v1/public/share/user/received')
  },

  revoke(id: string) {
    return request('/api/v1/public/share/user/revoke', {
      method: 'POST',
      body: { id }
    })
  },

  listEntries(shareId: string, path: string) {
    const query = new URLSearchParams({
      shareId,
      path: path || ''
    })
    return request<{
      items: ShareEntryItem[]
    }>(`/api/v1/public/share/user/entries?${query.toString()}`)
  },

  createFolder(shareId: string, path: string) {
    return request('/api/v1/public/share/user/folder', {
      method: 'POST',
      body: { shareId, path }
    })
  },

  rename(shareId: string, from: string, to: string) {
    return request('/api/v1/public/share/user/rename', {
      method: 'POST',
      body: { shareId, from, to }
    })
  },

  remove(shareId: string, path: string) {
    return request('/api/v1/public/share/user/item', {
      method: 'DELETE',
      body: { shareId, path }
    })
  }
}

export const addressBookApi = {
  listGroups() {
    return request<{ items: AddressGroup[] }>('/api/v1/public/webdav/address/groups')
  },
  createGroup(name: string) {
    return request<AddressGroup>('/api/v1/public/webdav/address/groups/create', {
      method: 'POST',
      body: { name }
    })
  },
  updateGroup(id: string, name: string) {
    return request('/api/v1/public/webdav/address/groups/update', {
      method: 'PUT',
      body: { id, name }
    })
  },
  deleteGroup(id: string) {
    return request('/api/v1/public/webdav/address/groups/delete', {
      method: 'DELETE',
      body: { id }
    })
  },
  listContacts() {
    return request<{ items: AddressContact[] }>('/api/v1/public/webdav/address/contacts')
  },
  createContact(payload: { name: string; walletAddress: string; groupId?: string; tags?: string[] }) {
    return request<AddressContact>('/api/v1/public/webdav/address/contacts/create', {
      method: 'POST',
      body: payload
    })
  },
  updateContact(payload: { id: string; name?: string; walletAddress?: string; groupId?: string; tags?: string[] }) {
    return request<AddressContact>('/api/v1/public/webdav/address/contacts/update', {
      method: 'PUT',
      body: payload
    })
  },
  deleteContact(id: string) {
    return request('/api/v1/public/webdav/address/contacts/delete', {
      method: 'DELETE',
      body: { id }
    })
  }
}
