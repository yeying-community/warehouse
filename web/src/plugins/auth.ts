import { authApi } from '@/api'

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
  const provider = getWalletProvider()
  if (!provider) {
    throw new Error(`未检测到钱包，请安装 MetaMask 或夜莺钱包`)
  }

  try {
    const accounts = await provider.request({ method: 'eth_requestAccounts' }) as string[]
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
  const provider = getWalletProvider()
  if (!provider) {
    throw new Error('未检测到钱包')
  }

  const address = await connectWallet()
  if (!address) return

  localStorage.setItem('currentAccount', address)

  try {
    // 1. 获取挑战
    const challenge = await authApi.getChallenge(address)

    // 2. 使用钱包签名
    const signature = await provider.request({
      method: 'personal_sign',
      params: [challenge.message, address]
    }) as unknown as string

    // 3. 验证签名获取 token
    const result = await authApi.verifySignature(address, signature)

    localStorage.setItem('authToken', result.token)
    // 同时设置 cookie，浏览器会在 Range 请求中自动带上
    document.cookie = `authToken=${result.token}; path=/; max-age=86400`

    // 存储用户信息
    localStorage.setItem('username', result.user.username)
    localStorage.setItem('walletAddress', result.user.wallet_address)
  } catch (error) {
    throw new Error(`登录失败: ${error}`)
  }
}

// 登出
export function logout(): void {
  localStorage.removeItem('authToken')
  localStorage.removeItem('currentAccount')
  localStorage.removeItem('username')
  localStorage.removeItem('walletAddress')
  // 清除 cookie
  document.cookie = 'authToken=; path=/; max-age=0'
  window.location.reload()
}

// 检查是否已登录
export function isLoggedIn(): boolean {
  const token = localStorage.getItem('authToken')
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
  return localStorage.getItem('authToken')
}

// 获取用户名
export function getUsername(): string | null {
  return localStorage.getItem('username')
}