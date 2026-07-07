import {
  getProvider,
  requestAccounts,
  loginWithChallenge,
  logout as sdkLogout,
  clearAccessToken,
  getAccessToken,
  setAccessToken,
  watchAccounts,
  watchProvider,
  focusPendingApproval,
  getWalletErrorMessage,
  isUserRejectedWalletAction,
  classifyWalletError,
  type Eip1193Provider
} from '@yeying-community/web3-bs'

const API_BASE = import.meta.env.VITE_API_BASE || ''
const AUTH_BASE = API_BASE ? `${API_BASE.replace(/\/+$/, '')}/api/v1/public/auth` : '/api/v1/public/auth'
const ACCOUNT_HISTORY_KEY = 'warehouse:accountHistory'
export const AUTH_CHANGED_EVENT = 'warehouse:auth-changed'

let currentWalletProvider: Eip1193Provider | null = null

export function watchWalletProvider(handler: (present: boolean) => void): () => void {
  return watchProvider(({ provider, present }) => {
    currentWalletProvider = provider
    handler(present)
  })
}

// 获取钱包名称
export function getWalletName(): string {
  const provider = currentWalletProvider
  if (!provider) return '未知钱包'

  if ((provider as unknown as { isYeYing?: boolean }).isYeYing) return '夜莺钱包'
  if (provider.isMetaMask) return 'MetaMask'
  return 'Web3 钱包'
}

function formatWalletLoginError(error: unknown): string {
  if (isUserRejectedWalletAction(error)) {
    return '你已取消钱包签名，请在钱包弹窗中确认后再试。'
  }
  const errorInfo = classifyWalletError(error)
  if (errorInfo.type === 'disconnected' || errorInfo.type === 'timeout') {
    return '钱包连接正在恢复，请稍后重试。'
  }
  const message = getWalletErrorMessage(error).replace(/^ProviderRpcError:\s*/i, '').trim()
  if (!message) return '钱包登录失败，请稍后重试。'
  return `钱包登录失败：${message}`
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

function normalizeAddress(address: string): string {
  return address.trim().toLowerCase()
}

function isWalletAddress(address: string): boolean {
  return /^0x[a-fA-F0-9]{40}$/.test(address.trim())
}

function readAccountHistory(): string[] {
  const stored = localStorage.getItem(ACCOUNT_HISTORY_KEY)
  if (!stored) return []
  try {
    const parsed = JSON.parse(stored)
    if (Array.isArray(parsed)) {
      return parsed.map(item => String(item)).filter(isWalletAddress)
    }
  } catch {
    // ignore
  }
  return []
}

function writeAccountHistory(accounts: string[]): void {
  localStorage.setItem(ACCOUNT_HISTORY_KEY, JSON.stringify(accounts))
}

export function getAccountHistory(): string[] {
  return readAccountHistory()
}

function rememberAccount(address: string): void {
  if (!isWalletAddress(address)) return
  const normalized = normalizeAddress(address)
  const history = readAccountHistory().map(normalizeAddress)
  const next = [normalized, ...history.filter(item => item !== normalized)]
  writeAccountHistory(next.slice(0, 10))
}

export async function watchWalletAccounts(handler: (payload: { account: string | null; accounts: string[] }) => void): Promise<() => void> {
  const provider = await getProvider()
  if (!provider) {
    return () => {}
  }
  return watchAccounts(provider, ({ account, accounts }) => {
    if (account) {
      rememberAccount(account)
    }
    handler({ account: account || null, accounts })
  })
}

export async function focusPendingWalletApproval(): Promise<boolean> {
  const provider = currentWalletProvider || await getProvider()
  if (!provider) return false

  try {
    const result = await focusPendingApproval(provider)
    return result.focused
  } catch (error) {
    console.warn('聚焦钱包待确认窗口失败:', error)
    return false
  }
}

// 钱包登录流程
export async function loginWithWallet(preferredAccount?: string): Promise<void> {
  const provider = await getProvider()
  if (!provider) {
    throw new Error('未检测到钱包')
  }

  const accounts = await requestAccounts({ provider })
  let address = accounts[0]
  if (preferredAccount) {
    const normalized = normalizeAddress(preferredAccount)
    const match = accounts.find(item => normalizeAddress(item) === normalized)
    if (!match) {
      throw new Error('请在钱包中切换到选中的账户')
    }
    address = match
  }
  if (!address) return

  localStorage.setItem('currentAccount', address)
  localStorage.setItem('walletAddress', address)
  rememberAccount(address)

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
    window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT))
  } catch (error) {
    throw new Error(formatWalletLoginError(error))
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
    rememberAccount(data.address)
  } else {
    localStorage.setItem('currentAccount', username)
  }

  if (data.username) {
    localStorage.setItem('username', data.username)
  }

  document.cookie = `authToken=${token}; path=/; max-age=86400`
  window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT))
}

// 发送邮箱验证码
export async function sendEmailCode(email: string): Promise<{ expiresAt?: number; retryAfter?: number }> {
  const response = await fetch(`${AUTH_BASE}/email/code`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'accept': 'application/json'
    },
    body: JSON.stringify({ email })
  })

  if (!response.ok) {
    const error = await response.text()
    throw new Error(error || `HTTP ${response.status}`)
  }

  const payload = await response.json()
  if (payload?.code !== 0) {
    throw new Error(payload?.message || '发送验证码失败')
  }

  return payload?.data || {}
}

// 邮箱验证码登录
export async function loginWithEmailCode(email: string, code: string): Promise<void> {
  const response = await fetch(`${AUTH_BASE}/email/login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'accept': 'application/json'
    },
    body: JSON.stringify({ email, code })
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

  const account = data.email || email
  if (account) {
    localStorage.setItem('currentAccount', account)
  }
  localStorage.removeItem('walletAddress')

  if (data.username) {
    localStorage.setItem('username', data.username)
  }

  document.cookie = `authToken=${token}; path=/; max-age=86400`
  window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT))
}

// 登出
export function logout(options: { reload?: boolean } = {}): void {
  const shouldReload = options.reload !== false
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
  window.dispatchEvent(new CustomEvent(AUTH_CHANGED_EVENT))
  if (shouldReload) {
    window.location.reload()
  }
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
