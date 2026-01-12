<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue'
import { Download, Delete, Refresh, FolderOpened, DocumentCopy, Edit, Share, User } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { quotaApi, userApi, recycleApi, shareApi, type RecycleItem, type ShareItem } from '@/api'
import { isLoggedIn, hasWallet, loginWithWallet, getUsername, getWalletName, getCurrentAccount, getUserPermissions, getUserCreatedAt } from '@/plugins/auth'
import { parsePropfindResponse } from '@/utils/webdav'

interface FileItem {
  name: string
  path: string
  isDir: boolean
  size: number
  modified: string
}

// 状态
const loading = ref(false)
const fileList = ref<FileItem[]>([])
const currentPath = ref('/')
const quota = ref({ quota: 0, used: 0, available: 0, percentage: 0, unlimited: true })
const userInfo = ref<{
  username: string
  wallet_address?: string
  permissions: string[]
  created_at?: string
  updated_at?: string
} | null>(null)
const uploadProgress = ref<string | null>(null)

// 回收站相关状态
const showRecycle = ref(false)
const recycleList = ref<RecycleItem[]>([])
const recycleLoading = ref(false)
const showShare = ref(false)
const shareList = ref<ShareItem[]>([])
const shareLoading = ref(false)
const showQuotaManage = ref(false)
const quotaManageLoading = ref(false)

// 是否显示回收站列表
const displayList = computed(() => {
  if (showRecycle.value) return recycleList.value
  if (showShare.value) return shareList.value
  return fileList.value
})
const userProfile = computed(() => {
  const username = userInfo.value?.username || getUsername() || '当前用户'
  const walletAddress = userInfo.value?.wallet_address || localStorage.getItem('walletAddress') || getCurrentAccount() || '-'
  const walletName = getWalletName()
  const permissions = userInfo.value?.permissions?.length ? userInfo.value.permissions : getUserPermissions()
  const createdAt = userInfo.value?.created_at || getUserCreatedAt()
  return { username, walletAddress, walletName, permissions, createdAt }
})
const quotaAvailable = computed(() => {
  if (quota.value.unlimited) return null
  const available = Number.isFinite(quota.value.available)
    ? quota.value.available
    : quota.value.quota - quota.value.used
  return Math.max(available, 0)
})
const breadcrumbItems = computed(() => {
  if (showRecycle.value) return []
  const parts = currentPath.value.split('/').filter(Boolean)
  const items: { name: string; path: string }[] = []
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    items.push({ name: part, path: acc + '/' })
  }
  return items
})

function encodePath(path: string): string {
  if (!path) return '/'
  const hasTrailing = path.endsWith('/') && path.length > 1
  const trimmed = path.replace(/^\/+/, '').replace(/\/+$/, '')
  if (!trimmed) return '/'
  const encoded = trimmed.split('/').map(encodeURIComponent).join('/')
  return '/' + encoded + (hasTrailing ? '/' : '')
}

function buildApiPath(path: string): string {
  const encodedPath = encodePath(path)
  return encodedPath === '/' ? '/api' : '/api' + encodedPath
}

function ensureAuthCookie(token: string): void {
  if (!token) return
  document.cookie = `authToken=${token}; path=/; max-age=86400`
}

function buildDavPath(path: string): string {
  return encodePath(path)
}

// 获取文件列表 (WebDAV PROPFIND)
async function fetchFiles(path: string = '/') {
  loading.value = true
  const apiPath = buildApiPath(path)
  console.log('fetchFiles:', path, '→', apiPath)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      method: 'PROPFIND',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/xml',
        'Depth': '1'
      }
    })

    console.log('PROPFIND response:', response.status, response.statusText)

    if (!response.ok) {
      throw new Error('获取文件列表失败')
    }

    const text = await response.text()
    console.log('PROPFIND response text length:', text.length)

    // 先更新 currentPath，再解析
    currentPath.value = path
    fileList.value = parsePropfindResponse(text, currentPath.value)
    console.log('parsed items:', fileList.value)
  } catch (error) {
    console.error('获取文件列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 获取配额
async function fetchQuota(withLoading = false) {
  if (withLoading) {
    quotaManageLoading.value = true
  }
  try {
    const data = await quotaApi.get()
    quota.value = data
  } catch (error) {
    console.error('获取配额失败:', error)
  } finally {
    if (withLoading) {
      quotaManageLoading.value = false
    }
  }
}

// 获取用户信息
async function fetchUserInfo() {
  try {
    const data = await userApi.getInfo()
    userInfo.value = data
    if (data.username) {
      localStorage.setItem('username', data.username)
    }
    if (data.wallet_address) {
      localStorage.setItem('walletAddress', data.wallet_address)
    }
    if (Array.isArray(data.permissions)) {
      localStorage.setItem('permissions', JSON.stringify(data.permissions))
    }
    if (data.created_at) {
      localStorage.setItem('createdAt', data.created_at)
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
  }
}

async function fetchUserCenter() {
  quotaManageLoading.value = true
  try {
    await Promise.all([fetchUserInfo(), fetchQuota()])
  } finally {
    quotaManageLoading.value = false
  }
}

// 进入目录
function enterDirectory(item: FileItem) {
  if (item.isDir) {
    // 确保路径格式正确：/test/ 而不是 /test
    let path = item.path
    if (!path.endsWith('/')) {
      path += '/'
    }
    fetchFiles(path)
  }
}

// 单击行进入目录（回收站模式不响应）
function handleRowClick(row: FileItem) {
  if (showRecycle.value || showShare.value || showQuotaManage.value) return
  if (row.isDir) {
    enterDirectory(row)
  }
}

// 刷新当前视图
function refreshCurrentView() {
  if (showRecycle.value) {
    fetchRecycle()
  } else if (showShare.value) {
    fetchShare()
  } else if (showQuotaManage.value) {
    fetchUserCenter()
  } else {
    fetchFiles(currentPath.value)
  }
}

async function copyText(text: string, successMessage: string) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
    } else {
      const textarea = document.createElement('textarea')
      textarea.value = text
      textarea.setAttribute('readonly', 'true')
      textarea.style.position = 'fixed'
      textarea.style.opacity = '0'
      document.body.appendChild(textarea)
      textarea.select()
      document.execCommand('copy')
      document.body.removeChild(textarea)
    }
    ElMessage.success(successMessage)
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error('复制失败')
  }
}

async function copyCurrentPath() {
  const text = showRecycle.value ? '回收站' : currentPath.value
  await copyText(text, '已复制当前路径')
}

// 返回上级目录
function goParent() {
  if (currentPath.value === '/') return
  const parts = currentPath.value.split('/').filter(Boolean)
  parts.pop()
  const parentPath = parts.length > 0 ? '/' + parts.join('/') + '/' : '/'
  fetchFiles(parentPath)
}

// 下载文件
async function downloadFile(item: FileItem) {
  const apiPath = buildApiPath(item.path)

  uploadProgress.value = '下载中...'

  try {
    const token = localStorage.getItem('authToken') || ''
    ensureAuthCookie(token)
    const a = document.createElement('a')
    a.href = apiPath
    a.download = item.name
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  } catch (error) {
    alert(`下载失败: ${error}`)
  } finally {
    window.setTimeout(() => {
      uploadProgress.value = null
    }, 800)
  }
}

async function renameItem(item: FileItem) {
  if (item.path === '/') return

  const rawPath = item.path.startsWith('/') ? item.path : '/' + item.path
  const isDir = item.isDir
  const normalized = isDir ? rawPath.replace(/\/$/, '') : rawPath
  const segments = normalized.split('/').filter(Boolean)
  const oldName = segments.pop()
  const parentPath = segments.length ? '/' + segments.join('/') + '/' : '/'

  const newName = prompt('请输入新的名称', oldName || '')
  if (!newName || newName === oldName) return
  if (newName.includes('/')) {
    alert('名称不能包含 "/"')
    return
  }

  const sourcePath = isDir ? normalized + '/' : normalized
  const destinationPath = (parentPath === '/' ? '/' + newName : parentPath + newName) + (isDir ? '/' : '')

  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(buildApiPath(sourcePath), {
      method: 'MOVE',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Destination': buildDavPath(destinationPath),
        'Overwrite': 'F'
      }
    })

    if (!response.ok) {
      throw new Error(`重命名失败: ${response.status}`)
    }

    await fetchFiles(currentPath.value)
  } catch (error) {
    alert(`重命名失败: ${error}`)
  }
}

async function shareFile(item: FileItem) {
  if (item.isDir) return
  try {
    let expiresIn: number | undefined
    try {
      const { value } = await ElMessageBox.prompt(
        '设置有效期（小时，0 表示永不过期）',
        '创建分享链接',
        {
          confirmButtonText: '创建',
          cancelButtonText: '取消',
          inputPattern: /^\d+$/,
          inputErrorMessage: '请输入非负整数',
          inputValue: '0'
        }
      )
      const hours = parseInt(value, 10)
      if (Number.isFinite(hours) && hours > 0) {
        expiresIn = hours * 3600
      }
    } catch {
      return
    }

    const data = await shareApi.create(item.path, expiresIn)
    const url = data.url || `${window.location.origin}/api/v1/public/share/${data.token}`
    await copyText(url, '分享链接已复制')
  } catch (error) {
    console.error('创建分享失败:', error)
    ElMessage.error('创建分享失败')
  }
}

// 上传文件
const fileInput = ref<HTMLInputElement | null>(null)

function triggerUpload() {
  fileInput.value?.click()
}

async function handleFileSelect(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  uploadProgress.value = '上传中...'
  const cleanPath = currentPath.value.replace(/^\//, '').replace(/\/$/, '')
  const targetPath = cleanPath ? '/' + cleanPath + '/' + file.name : '/' + file.name
  const apiPath = buildApiPath(targetPath)
  try {
    const token = localStorage.getItem('authToken') || ''
    const formData = new FormData()
    formData.append('file', file)

    const response = await fetch(apiPath, {
      method: 'PUT',
      headers: {
        'Authorization': `Bearer ${token}`
      },
      body: formData
    })

    if (!response.ok) {
      throw new Error('上传失败')
    }

    uploadProgress.value = '上传完成'
    // 等待文件完全写入后再刷新列表
    await new Promise(resolve => setTimeout(resolve, 500))
    await fetchFiles(currentPath.value)
    // 等待 Vue 更新 DOM 后再清除进度
    await nextTick()
    uploadProgress.value = null
  } catch (error) {
    uploadProgress.value = `上传失败: ${error}`
  }
  // 清空 input，允许重复上传同一文件
  input.value = ''
}

// 删除文件
async function deleteFile(item: FileItem) {
  if (!confirm(`确定删除 ${item.name} 吗？删除后可在回收站恢复`)) return

  const apiPath = buildApiPath(item.path)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      method: 'DELETE',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })

    if (!response.ok) {
      throw new Error('删除失败')
    }

    fetchFiles(currentPath.value)
  } catch (error) {
    alert(`删除失败: ${error}`)
  }
}

// 获取回收站列表
async function fetchRecycle() {
  recycleLoading.value = true
  try {
    const data = await recycleApi.list()
    recycleList.value = data.items
  } catch (error) {
    console.error('获取回收站失败:', error)
  } finally {
    recycleLoading.value = false
  }
}

// 进入回收站
function enterRecycle() {
  showRecycle.value = true
  showShare.value = false
  showQuotaManage.value = false
  fetchRecycle()
}

// 返回文件列表
function backToFiles() {
  showRecycle.value = false
  showShare.value = false
  showQuotaManage.value = false
  fetchFiles(currentPath.value)
}

// 恢复文件
async function recoverFile(item: RecycleItem) {
  if (!confirm(`确定恢复 ${item.name} 吗？`)) return
  try {
    await recycleApi.recover(item.hash)
    fetchRecycle()
  } catch (error) {
    alert(`恢复失败: ${error}`)
  }
}

// 永久删除文件
async function permanentlyDelete(item: RecycleItem) {
  if (!confirm(`确定永久删除 ${item.name} 吗？此操作不可恢复！`)) return
  try {
    await recycleApi.remove(item.hash)
    fetchRecycle()
  } catch (error) {
    alert(`删除失败: ${error}`)
  }
}

// 获取分享列表
async function fetchShare() {
  shareLoading.value = true
  try {
    const data = await shareApi.list()
    shareList.value = data.items
  } catch (error) {
    console.error('获取分享列表失败:', error)
  } finally {
    shareLoading.value = false
  }
}

// 进入分享管理
function enterShare() {
  showShare.value = true
  showRecycle.value = false
  showQuotaManage.value = false
  fetchShare()
}

// 取消分享
async function revokeShare(item: ShareItem) {
  if (!confirm(`确定取消分享 ${item.name} 吗？`)) return
  try {
    await shareApi.revoke(item.token)
    fetchShare()
  } catch (error) {
    alert(`取消分享失败: ${error}`)
  }
}

async function copyShareLink(item: ShareItem) {
  const url = item.url || `${window.location.origin}/api/v1/public/share/${item.token}`
  await copyText(url, '分享链接已复制')
}

// 进入用户中心
function enterQuotaManage() {
  showQuotaManage.value = true
  showShare.value = false
  showRecycle.value = false
  fetchUserCenter()
}

// 新建文件夹
async function createFolder() {
  const name = prompt('请输入文件夹名称')
  if (!name) return

  const cleanPath = currentPath.value.replace(/^\//, '').replace(/\/$/, '')
  const targetPath = cleanPath ? '/' + cleanPath + '/' + name : '/' + name
  const apiPath = buildApiPath(targetPath)
  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      method: 'MKCOL',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })

    // 201 = 创建成功, 405 = 方法不允许（通常因为已存在）
    if (response.ok || response.status === 405) {
      // 刷新文件列表
      fetchFiles(currentPath.value)
      if (response.status === 405) {
        alert('文件夹已存在')
      }
    } else {
      throw new Error(`创建失败: ${response.status}`)
    }
  } catch (error) {
    alert(`创建失败: ${error}`)
  }
}

// 格式化文件大小
function formatSize(bytes: number): string {
  if (bytes === 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  const k = 1024
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + units[i]
}

// 格式化时间
function formatDateTime(value: string | number): string {
  if (value === null || value === undefined || value === '') return '-'
  try {
    let raw: number | string = value
    if (typeof value === 'string' && /^\d+$/.test(value)) {
      const asNumber = Number(value)
      raw = value.length <= 10 ? asNumber * 1000 : asNumber
    }
    const date = new Date(raw)
    if (Number.isNaN(date.getTime())) return '-'
    const pad = (num: number) => String(num).padStart(2, '0')
    const year = date.getFullYear()
    const month = pad(date.getMonth() + 1)
    const day = pad(date.getDate())
    const hour = pad(date.getHours())
    const minute = pad(date.getMinutes())
    const second = pad(date.getSeconds())
    return `${year}-${month}-${day} ${hour}:${minute}:${second}`
  } catch {
    return '-'
  }
}

// 格式化时间
function formatTime(timeStr: string | number): string {
  return formatDateTime(timeStr)
}

// 格式化删除时间
function formatDeletedTime(timeStr: string): string {
  return formatDateTime(timeStr)
}

// 连接钱包
async function handleConnect() {
  try {
    await loginWithWallet()
    window.location.reload()
  } catch (error) {
    alert(`连接失败: ${error}`)
  }
}

onMounted(() => {
  if (isLoggedIn()) {
    fetchFiles()
    fetchQuota()
    fetchUserInfo()
  }
})
</script>

<template>
  <div class="home-container">
    <!-- 未登录状态 -->
    <div v-if="!isLoggedIn()" class="login-page">
      <div class="login-box">
        <h1>WebDAV Storage</h1>
        <p>连接钱包登录</p>
        <el-button v-if="hasWallet()" type="primary" size="large" @click="handleConnect">
          连接 MetaMask
        </el-button>
        <p v-else class="warning">请安装 MetaMask 等钱包插件</p>
      </div>
    </div>

    <!-- 已登录状态 -->
    <div v-else class="app-shell">
      <aside class="side-panel">
        <div class="brand">
          <div class="brand-mark"></div>
          <div class="brand-text">
            <div class="brand-sub">资产管理中心</div>
          </div>
        </div>

        <div class="nav-block">
          <div class="nav-title">导航</div>
          <div class="nav-list">
            <button
              type="button"
              class="nav-item"
              :class="{ active: !showRecycle && !showShare && !showQuotaManage }"
              @click="backToFiles"
            >
              <el-icon class="nav-icon"><FolderOpened /></el-icon>
              <span>文件管理</span>
            </button>
            <button
              type="button"
              class="nav-item"
              :class="{ active: showRecycle }"
              @click="enterRecycle"
            >
              <el-icon class="nav-icon"><Delete /></el-icon>
              <span>回收站</span>
            </button>
            <button
              type="button"
              class="nav-item"
              :class="{ active: showShare }"
              @click="enterShare"
            >
              <el-icon class="nav-icon"><Share /></el-icon>
              <span>文件分享</span>
            </button>
            <button
              type="button"
              class="nav-item"
              :class="{ active: showQuotaManage }"
              @click="enterQuotaManage"
            >
              <el-icon class="nav-icon"><User /></el-icon>
              <span>用户中心</span>
            </button>
          </div>
        </div>
      </aside>

      <main class="main-panel">
        <header class="page-header">
          <div class="header-left">
            <div class="path-row">
              <template v-if="showRecycle || showShare || showQuotaManage">
                <el-button @click="backToFiles">
                  <span class="iconfont icon-fanhui"></span> 返回文件列表
                </el-button>
              </template>
              <template v-else>
                <el-button @click="goParent" :disabled="currentPath === '/'">
                  <span class="iconfont icon-fanhui"></span> 返回上级
                </el-button>
              </template>
              <template v-if="showRecycle">
                <div class="path-pill">
                  <span class="path-label">当前位置</span>
                  <span class="path-value">回收站</span>
                  <el-tooltip content="复制路径" placement="top">
                    <button type="button" class="copy-icon" @click="copyCurrentPath">
                      <el-icon><DocumentCopy /></el-icon>
                    </button>
                  </el-tooltip>
                </div>
              </template>
              <template v-else-if="showShare">
                <div class="path-pill">
                  <span class="path-label">当前位置</span>
                  <span class="path-value">文件分享</span>
                </div>
              </template>
              <template v-else-if="showQuotaManage">
                <div class="path-pill">
                  <span class="path-label">当前位置</span>
                  <span class="path-value">用户中心</span>
                </div>
              </template>
              <template v-else>
                <div class="breadcrumb">
                  <el-breadcrumb separator="/">
                    <el-breadcrumb-item>
                      <button class="breadcrumb-link" type="button" @click="fetchFiles('/')">根目录</button>
                    </el-breadcrumb-item>
                    <el-breadcrumb-item v-for="crumb in breadcrumbItems" :key="crumb.path">
                      <button class="breadcrumb-link" type="button" @click="fetchFiles(crumb.path)">
                        {{ crumb.name }}
                      </button>
                    </el-breadcrumb-item>
                  </el-breadcrumb>
                  <el-tooltip content="复制路径" placement="top">
                    <button type="button" class="copy-icon" @click="copyCurrentPath">
                      <el-icon><DocumentCopy /></el-icon>
                    </button>
                  </el-tooltip>
                </div>
              </template>
            </div>
          </div>
          <div class="header-actions">
            <template v-if="showRecycle">
              <el-button @click="refreshCurrentView" :loading="recycleLoading">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </template>
            <template v-else-if="showShare">
              <el-button @click="refreshCurrentView" :loading="shareLoading">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </template>
            <template v-else-if="showQuotaManage">
              <el-button @click="refreshCurrentView" :loading="quotaManageLoading">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </template>
            <template v-else>
              <el-button @click="createFolder">
                <span class="iconfont icon-tianjia"></span> 新建文件夹
              </el-button>
              <el-button type="primary" @click="triggerUpload">
                <span class="iconfont icon-shangchuan"></span> 上传文件
              </el-button>
              <el-button @click="refreshCurrentView" :loading="loading">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
              <input
                ref="fileInput"
                type="file"
                style="display:none"
                @change="handleFileSelect"
              />
            </template>
          </div>
        </header>

        <section class="content-card">
          <div v-if="showQuotaManage" class="user-center" v-loading="quotaManageLoading">
            <div class="user-card">
              <div class="card-title">基础信息</div>
              <div class="user-list">
                <div class="user-row">
                  <span class="user-label">用户名</span>
                  <span class="user-value">{{ userProfile.username }}</span>
                </div>
                <div class="user-row">
                  <span class="user-label">钱包地址</span>
                  <span class="user-value mono">{{ userProfile.walletAddress }}</span>
                </div>
                <div class="user-row">
                  <span class="user-label">钱包类型</span>
                  <span class="user-value">{{ userProfile.walletName }}</span>
                </div>
                <div class="user-row">
                  <span class="user-label">权限</span>
                  <span class="user-value">
                    <span v-if="!userProfile.permissions.length">-</span>
                    <span v-else class="user-tags">
                      <el-tag v-for="permission in userProfile.permissions" :key="permission" size="small" type="info">
                        {{ permission }}
                      </el-tag>
                    </span>
                  </span>
                </div>
                <div class="user-row">
                  <span class="user-label">注册时间</span>
                  <span class="user-value">
                    {{ userProfile.createdAt ? formatTime(userProfile.createdAt) : '-' }}
                  </span>
                </div>
              </div>
            </div>
            <div class="user-card">
              <div class="card-title">当前额度</div>
              <div class="quota-value">
                <span>{{ formatSize(quota.used) }}</span>
                <span class="quota-sep">/</span>
                <span>{{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
              </div>
              <el-progress
                v-if="!quota.unlimited"
                :percentage="Math.min(Number(quota.percentage.toFixed(2)), 100)"
                :stroke-width="8"
              />
              <div class="quota-meta">
                <span v-if="quota.unlimited">未设置上限</span>
                <span v-else>已使用 {{ quota.percentage.toFixed(2) }}%</span>
              </div>
              <div class="quota-grid">
                <div class="quota-item">
                  <span class="quota-label">已使用</span>
                  <span class="quota-amount">{{ formatSize(quota.used) }}</span>
                </div>
                <div class="quota-item">
                  <span class="quota-label">可用</span>
                  <span class="quota-amount">
                    {{ quota.unlimited ? '无限' : formatSize(quotaAvailable ?? 0) }}
                  </span>
                </div>
                <div class="quota-item">
                  <span class="quota-label">总额度</span>
                  <span class="quota-amount">{{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
                </div>
                <div class="quota-item">
                  <span class="quota-label">使用率</span>
                  <span class="quota-amount">{{ quota.unlimited ? '-' : `${quota.percentage.toFixed(2)}%` }}</span>
                </div>
              </div>
            </div>
          </div>
          <el-table
            v-else
            :data="displayList"
            v-loading="showRecycle ? recycleLoading : (showShare ? shareLoading : loading)"
            style="width: 100%"
            @row-click="handleRowClick"
          >
            <!-- 回收站模式 -->
            <template v-if="showRecycle">
              <el-table-column label="目录" width="120">
                <template #default="{ row }">
                  <el-tag size="small">{{ row.directory }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="名称" min-width="200">
                <template #default="{ row }">
                  <div class="file-name">
                    <span class="iconfont icon-wenjian1"></span>
                    <span class="name">{{ row.name }}</span>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="原始路径" min-width="180">
                <template #default="{ row }">
                  {{ row.path }}
                </template>
              </el-table-column>
              <el-table-column label="大小" width="80">
                <template #default="{ row }">
                  {{ formatSize(row.size) }}
                </template>
              </el-table-column>
              <el-table-column label="删除时间" width="150">
                <template #default="{ row }">
                  {{ formatDeletedTime(row.deletedAt) }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="140" fixed="right">
                <template #default="{ row }">
                  <div class="actions">
                    <el-tooltip content="恢复" placement="top">
                      <el-button type="primary" link @click="recoverFile(row)">
                        <el-icon><FolderOpened /></el-icon>
                      </el-button>
                    </el-tooltip>
                    <el-tooltip content="永久删除" placement="top">
                      <el-button type="danger" link @click="permanentlyDelete(row)">
                        <el-icon><Delete /></el-icon>
                      </el-button>
                    </el-tooltip>
                  </div>
                </template>
              </el-table-column>
            </template>
            <template v-else-if="showShare">
              <el-table-column label="名称" min-width="200">
                <template #default="{ row }">
                  <div class="file-name">
                    <span class="iconfont icon-wenjian1"></span>
                    <span class="name">{{ row.name }}</span>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="路径" min-width="180">
                <template #default="{ row }">
                  {{ row.path }}
                </template>
              </el-table-column>
              <el-table-column label="访问/下载" width="110">
                <template #default="{ row }">
                  {{ row.viewCount ?? 0 }}/{{ row.downloadCount ?? 0 }}
                </template>
              </el-table-column>
              <el-table-column label="过期时间" width="160">
                <template #default="{ row }">
                  {{ row.expiresAt ? formatTime(row.expiresAt) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="创建时间" width="160">
                <template #default="{ row }">
                  {{ row.createdAt ? formatTime(row.createdAt) : '-' }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="120" fixed="right">
                <template #default="{ row }">
                  <div class="actions">
                    <el-tooltip content="复制链接" placement="top">
                      <el-button link :icon="DocumentCopy" @click="copyShareLink(row)" />
                    </el-tooltip>
                    <el-tooltip content="取消分享" placement="top">
                      <el-button type="danger" link :icon="Delete" @click="revokeShare(row)" />
                    </el-tooltip>
                  </div>
                </template>
              </el-table-column>
            </template>
            <!-- 文件列表模式 -->
            <template v-else>
              <el-table-column label="名称" min-width="280">
                <template #default="{ row }">
                  <div class="file-name">
                    <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
                    <span class="name">{{ row.name }}</span>
                  </div>
                </template>
              </el-table-column>
              <el-table-column label="大小" width="100">
                <template #default="{ row }">
                  {{ row.isDir ? '-' : formatSize(row.size) }}
                </template>
              </el-table-column>
              <el-table-column label="修改时间" width="160">
                <template #default="{ row }">
                  {{ formatTime(row.modified) }}
                </template>
              </el-table-column>
              <el-table-column label="操作" width="180" fixed="right">
                <template #default="{ row }">
                  <div class="actions" @click.stop>
                    <el-tooltip v-if="!row.isDir" content="下载" placement="top">
                      <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
                    </el-tooltip>
                    <el-tooltip v-if="!row.isDir" content="分享" placement="top">
                      <el-button type="primary" link :icon="Share" @click="shareFile(row)" />
                    </el-tooltip>
                    <el-tooltip content="重命名" placement="top">
                      <el-button type="primary" link :icon="Edit" @click="renameItem(row)" />
                    </el-tooltip>
                    <el-tooltip content="删除" placement="top">
                      <el-button type="danger" link :icon="Delete" @click="deleteFile(row)" />
                    </el-tooltip>
                  </div>
                </template>
              </el-table-column>
            </template>
          </el-table>
        </section>

        <!-- 上传进度 -->
        <div v-if="uploadProgress" class="upload-tip">
          {{ uploadProgress }}
        </div>
      </main>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.home-container {
  min-height: 100%;
  background: linear-gradient(180deg, #f6f8fb 0%, #f2f4f7 100%);
}

.login-page {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;

  .login-box {
    text-align: center;
    padding: 40px;
    border-radius: 16px;
    background: #fff;
    box-shadow: 0 12px 28px rgba(15, 23, 42, 0.12);

    h1 {
      margin-bottom: 16px;
    }

    p {
      margin-bottom: 24px;
      color: #666;
    }

    .warning {
      color: #e6a23c;
    }
  }
}

.app-shell {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  gap: 16px;
  padding: 16px;
  height: 100%;
  box-sizing: border-box;
}

.side-panel {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid #f0f2f5;
}

.brand-mark {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: linear-gradient(135deg, #409eff, #7cc6ff);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.6);
}

.brand-title {
  font-weight: 600;
  font-size: 16px;
  color: #1f2d3d;
}

.brand-sub {
  font-size: 14px;
  color: #98a2b3;
  margin-top: 2px;
}

.nav-block {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.nav-title {
  font-size: 12px;
  color: #8a8f98;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.nav-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.nav-item {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 12px;
  border: 1px solid transparent;
  background: transparent;
  color: #2b2f36;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.nav-item:hover {
  background: #f5f7fa;
}

.nav-item.active {
  background: #eef5ff;
  border-color: #d6e6ff;
  color: #1c4fb8;
  font-weight: 600;
}

.nav-item.is-soon {
  cursor: not-allowed;
}

.nav-item:disabled {
  background: #fafafa;
  color: #9aa0a6;
  border-color: #f0f0f0;
}

.nav-icon {
  font-size: 16px;
}

.nav-item .el-tag {
  margin-left: auto;
}

.main-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.page-header {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.view-title {
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.path-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.path-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #f5f7fa;
  color: #606266;
  font-size: 12px;
}

.path-label {
  color: #909399;
}

.breadcrumb {
  display: flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  background: #f5f7fa;
  color: #606266;
  gap: 8px;
}

.breadcrumb-link {
  border: none;
  background: transparent;
  padding: 0;
  font-size: 12px;
  color: #409eff;
  cursor: pointer;
}

.breadcrumb-link:hover {
  color: #1d73c7;
}

.copy-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  background: transparent;
  color: #909399;
  cursor: pointer;
  border-radius: 999px;
  transition: all 0.2s ease;
}

.share-link {
  display: flex;
  align-items: center;
  gap: 6px;
}

.share-text {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #606266;
}

.copy-icon:hover {
  background: rgba(64, 158, 255, 0.12);
  color: #409eff;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.content-card {
  background: #fff;
  border-radius: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  padding: 8px 8px 12px;
}

.file-name {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;

  .iconfont {
    font-size: 20px;
  }

  .icon-wenjianjia {
    color: #e6a23c;
  }

  .icon-wenjian1 {
    color: #409eff;
  }
}

.actions {
  display: flex;
  gap: 8px;
}

.actions .el-button {
  padding: 0 4px;
}

.upload-tip {
  align-self: flex-start;
  padding: 8px 16px;
  background: #ecf9f1;
  border: 1px solid #d3f1df;
  border-radius: 10px;
  color: #2f8f5b;
  font-size: 13px;
}

.user-center {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  padding: 8px;
}

.user-card {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.user-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.user-row {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
}

.user-label {
  font-size: 12px;
  color: #909399;
}

.user-value {
  font-size: 13px;
  color: #1f2d3d;
  word-break: break-all;
}

.user-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.user-value.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.quota-value {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.quota-sep {
  color: #c0c4cc;
  font-weight: 400;
}

.quota-meta {
  font-size: 12px;
  color: #909399;
}

.quota-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.quota-item {
  padding: 10px 12px;
  border-radius: 12px;
  background: #f7f9fc;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.quota-label {
  font-size: 12px;
  color: #909399;
}

.quota-amount {
  font-size: 13px;
  font-weight: 600;
  color: #1f2d3d;
}

@media (max-width: 1200px) {
  .app-shell {
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .user-center {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 900px) {
  .app-shell {
    grid-template-columns: 1fr;
    padding: 12px;
  }

  .side-panel {
    padding: 12px;
  }

  .brand {
    padding-bottom: 0;
    border-bottom: none;
  }

  .nav-list {
    flex-direction: row;
    flex-wrap: wrap;
  }

  .nav-item {
    width: auto;
  }

  .page-header {
    padding: 12px;
  }
}
</style>
