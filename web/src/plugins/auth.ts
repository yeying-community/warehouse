import { getProvider, requestAccounts, loginWithChallenge, logout as sdkLogout, clearAccessToken, getAccessToken, setAccessToken } from '@yeying-community/web3-bs'

const API_BASE = import.meta.env.VITE_API_BASE || ''
const AUTH_BASE = API_BASE ? `${API_BASE.replace(/\/+$/, '')}/api/v1/public/auth` : '/api/v1/public/auth'

// 钱包 Provider 类型
interface WalletProvider {
  request(args: { method: string; params?: unknown[] }): Promise<unknown>
  on(event: string, handler: (...args: unknown[]) => void): void
  removeListener(event: string, handler: (...args: unknown[]) => void): void
  isMetaMask?: boolean
  isYeYing?: boolean
  [key: string]: unknown
}

// 扩展 Window 类型
declare global {
  interface Window {
    ethereum?: WalletProvider
    yeeying?: WalletProvider
    yeying?: WalletProvider
    __YEYING_PROVIDER__?: WalletProvider
  }
}

// 获取钱包 provider（支持多种钱包）
export function getWalletProvider(): WalletProvider | null {
  // 1. 检查常见的 provider 属性
  const providerNames = ['ethereum', 'yeeying', 'yeying', 'coinbaseWallet', 'bitkeep', 'tokenpocket', '__YEYING_PROVIDER__']

  for (const name of providerNames) {
    const provider = (window as unknown as Record<string, WalletProvider | undefined>)[name]
    if (provider && typeof provider.request === 'function') {
      return provider
    }
  }

  // 2. 检查 ethereum.providers（某些浏览器扩展会注入多个 provider）
  if (window.ethereum && Array.isArray((window.ethereum as unknown as { providers?: WalletProvider[] }).providers)) {
    const providers = (window.ethereum as unknown as { providers: WalletProvider[] }).providers
    // 优先使用 MetaMask 或夜莺钱包
    for (const provider of providers) {
      if (provider.isMetaMask || (provider as unknown as { isYeYing?: boolean }).isYeYing) {
        return provider
      }
    }
    // 使用第一个可用的
    if (providers.length > 0) {
      return providers[0]
    }
  }

  // 3. 直接使用 window.ethereum
  if (window.ethereum) {
    return window.ethereum
  }

  return null
}

// 检测是否有钱包注入
export function hasWallet(): boolean {
  return getWalletProvider() !== null
}

// 获取钱包名称
export function getWalletName(): string {
  const provider = getWalletProvider()
  if (!provider) return '未知钱包'

  if ((provider as unknown as { isYeYing?: boolean }).isYeYing) return '夜莺钱包'
  if (provider.isMetaMask) return 'MetaMask'
  return 'Web3 钱包'
}

// 连接钱包并登录
export async function connectWallet(): Promise<string | null> {
  const provider = await getProvider()
  if (!provider) {
    throw new Error(`未检测到钱包，请安装 MetaMask 或夜莺钱包`)
  }

  try {
    const accounts = await requestAccounts({ provider })
    if (!accounts || accounts.length === 0) {
      throw new Error('未获取到账户')
    }
    return accounts[0]
  } catch (error) {
    throw new Error(`连接钱包失败: ${error}`)
  }
}

// 获取当前账户
export function getCurrentAccount(): string | null {
  return localStorage.getItem('currentAccount')
}

// 钱包登录流程
export async function loginWithWallet(): Promise<void> {
  const provider = await getProvider()
  if (!provider) {
    throw new Error('未检测到钱包')
  }

  const accounts = await requestAccounts({ provider })
  const address = accounts[0]
  if (!address) return

  localStorage.setItem('currentAccount', address)
  localStorage.setItem('walletAddress', address)

  try {
    const result = await loginWithChallenge({
      provider,
      address,
      baseUrl: AUTH_BASE
    })

    if (result.token) {
      // 用于 Range/下载请求携带认证
      document.cookie = `authToken=${result.token}; path=/; max-age=86400`
    }
  } catch (error) {
    throw new Error(`登录失败: ${error}`)
  }
}

// 用户名密码登录
export async function loginWithPassword(username: string, password: string): Promise<void> {
  const response = await fetch(`${AUTH_BASE}/password/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'accept': 'application/json'
    },
    body: JSON.stringify({ username, password })
  })

  if (!response.ok) {
    const error = await response.text()
    throw new Error(error || `HTTP ${response.status}`)
  }

  const payload = await response.json()
  if (payload?.code !== 0) {
    throw new Error(payload?.message || '登录失败')
  }

  const data = payload?.data || {}
  const token = data.token
  if (!token) {
    throw new Error('登录失败：未返回 token')
  }

  setAccessToken(token)

  if (data.address) {
    localStorage.setItem('currentAccount', data.address)
    localStorage.setItem('walletAddress', data.address)
  } else {
    localStorage.setItem('currentAccount', username)
  }

  if (data.username) {
    localStorage.setItem('username', data.username)
  }

  document.cookie = `authToken=${token}; path=/; max-age=86400`
}

// 登出
export function logout(): void {
  void sdkLogout({ baseUrl: AUTH_BASE }).catch((error) => {
    console.warn('logout failed:', error)
  })
  clearAccessToken()
  localStorage.removeItem('currentAccount')
  localStorage.removeItem('username')
  localStorage.removeItem('walletAddress')
  localStorage.removeItem('permissions')
  localStorage.removeItem('createdAt')
  // 清除 cookie
  document.cookie = 'authToken=; path=/; max-age=0'
  window.location.reload()
}

// 检查是否已登录
export function isLoggedIn(): boolean {
  const token = getAccessToken()
  if (!token) return false

  try {
    const payload = token.split('.')[1]
    const decoded = JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')))
    return decoded.exp * 1000 > Date.now()
  } catch {
    return false
  }
}

// 获取 token
export function getToken(): string | null {
  return getAccessToken()
}

// 获取用户名
export function getUsername(): string | null {
  return localStorage.getItem('username')
}

function parseTokenPayload(): Record<string, unknown> | null {
  const token = getAccessToken()
  if (!token) return null
  try {
    const payload = token.split('.')[1]
    return JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')))
  } catch {
    return null
  }
}

export function getUserPermissions(): string[] {
  const stored = localStorage.getItem('permissions')
  if (stored) {
    try {
      const parsed = JSON.parse(stored)
      if (Array.isArray(parsed)) {
        return parsed.map(item => String(item))
      }
    } catch {
      // ignore parse errors
    }
  }
  const payload = parseTokenPayload()
  const raw = (payload?.permissions ||
    (payload?.user as { permissions?: unknown } | undefined)?.permissions) as unknown
  return Array.isArray(raw) ? raw.map(item => String(item)) : []
}

export function getUserCreatedAt(): string | null {
  const stored = localStorage.getItem('createdAt')
  return stored || null
}
