<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue'
import { Download, Delete, Refresh, FolderOpened, DocumentCopy, Edit } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { quotaApi, recycleApi, type RecycleItem } from '@/api'
import { isLoggedIn, hasWallet, loginWithWallet } from '@/plugins/auth'
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
const quota = ref({ quota: 0, used: 0, percentage: 0, unlimited: true })
const uploadProgress = ref<string | null>(null)

// 回收站相关状态
const showRecycle = ref(false)
const recycleList = ref<RecycleItem[]>([])
const recycleLoading = ref(false)

// 是否显示回收站列表
const displayList = computed(() => showRecycle.value ? recycleList.value : fileList.value)
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
async function fetchQuota() {
  try {
    const data = await quotaApi.get()
    quota.value = data
  } catch (error) {
    console.error('获取配额失败:', error)
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
  if (showRecycle.value) return
  if (row.isDir) {
    enterDirectory(row)
  }
}

// 刷新当前视图
function refreshCurrentView() {
  if (showRecycle.value) {
    fetchRecycle()
  } else {
    fetchFiles(currentPath.value)
  }
}

async function copyCurrentPath() {
  const text = showRecycle.value ? '回收站' : currentPath.value
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
    ElMessage.success('已复制当前路径')
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error('复制失败')
  }
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
  fetchRecycle()
}

// 返回文件列表
function backToFiles() {
  showRecycle.value = false
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
function formatTime(timeStr: string): string {
  if (!timeStr) return '-'
  try {
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
  } catch {
    return timeStr
  }
}

// 格式化删除时间
function formatDeletedTime(timeStr: string): string {
  if (!timeStr) return '-'
  try {
    const date = new Date(timeStr)
    return date.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    })
  } catch {
    return timeStr
  }
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
              :class="{ active: !showRecycle }"
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
            <button type="button" class="nav-item is-soon" disabled>
              <el-icon class="nav-icon"><Download /></el-icon>
              <span>文件分享</span>
              <el-tag size="small" type="info">规划中</el-tag>
            </button>
            <button type="button" class="nav-item is-soon" disabled>
              <el-icon class="nav-icon"><Refresh /></el-icon>
              <span>配额管理</span>
              <el-tag size="small" type="info">规划中</el-tag>
            </button>
          </div>
        </div>
      </aside>

      <main class="main-panel">
        <header class="page-header">
          <div class="header-left">
            <div class="path-row">
              <template v-if="showRecycle">
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
          <!-- 文件列表 -->
          <el-table
            :data="displayList"
            v-loading="showRecycle ? recycleLoading : loading"
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
              <el-table-column label="操作" width="140" fixed="right">
                <template #default="{ row }">
                  <div class="actions" @click.stop>
                    <el-tooltip v-if="!row.isDir" content="下载" placement="top">
                      <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
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

      <aside class="info-panel">
        <div class="info-card">
          <div class="card-title">空间使用</div>
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
        </div>

        <div class="info-card muted">
          <div class="card-title">文件分享</div>
          <p class="card-desc">为单个文件生成可控访问链接。</p>
          <el-button size="small" disabled>创建分享链接</el-button>
        </div>

        <div class="info-card muted">
          <div class="card-title">配额管理</div>
          <p class="card-desc">按用户与目录设置空间配额。</p>
          <el-button size="small" disabled>打开配额面板</el-button>
        </div>
      </aside>
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
  grid-template-columns: 240px minmax(0, 1fr) 280px;
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

.upload-tip {
  align-self: flex-start;
  padding: 8px 16px;
  background: #ecf9f1;
  border: 1px solid #d3f1df;
  border-radius: 10px;
  color: #2f8f5b;
  font-size: 13px;
}

.info-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.info-card {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  border: 1px solid #eef1f4;
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.info-card.muted {
  opacity: 0.8;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
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

.card-desc {
  margin: 0;
  color: #7a7f87;
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 1200px) {
  .app-shell {
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .info-panel {
    display: none;
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
