<script setup lang="ts">
import { ref, onMounted, nextTick, computed } from 'vue'
import { Download, Delete, Refresh, FolderOpened } from '@element-plus/icons-vue'
import { ElIcon } from 'element-plus'
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

// 获取文件列表 (WebDAV PROPFIND)
async function fetchFiles(path: string = '/') {
  loading.value = true
  const apiPath = path === '/' ? '/api' : '/api' + path
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
  const cleanPath = item.path.replace(/^\//, '')
  const apiPath = '/api/' + cleanPath

  uploadProgress.value = '下载中...'

  try {
    const token = localStorage.getItem('authToken') || ''
    const response = await fetch(apiPath, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    })

    if (!response.ok) {
      throw new Error('下载失败')
    }

    const blob = await response.blob()
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = item.name
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)
  } catch (error) {
    alert(`下载失败: ${error}`)
  } finally {
    uploadProgress.value = null
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
  const apiPath = cleanPath ? '/api/' + cleanPath + '/' + file.name : '/api/' + file.name
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

  const cleanPath = item.path.replace(/^\//, '')
  const apiPath = '/api/' + cleanPath
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
  const apiPath = cleanPath ? '/api/' + cleanPath + '/' + name : '/api/' + name
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
    <div v-else class="file-manager">
      <!-- 顶部工具栏 -->
      <div class="toolbar">
        <div class="left">
          <template v-if="showRecycle">
            <el-button @click="backToFiles">
              <span class="iconfont icon-fanhui"></span> 返回文件列表
            </el-button>
            <span class="path">回收站</span>
          </template>
          <template v-else>
            <el-button @click="goParent" :disabled="currentPath === '/'">
              <span class="iconfont icon-fanhui"></span> 返回上级
            </el-button>
            <span class="path">当前路径: {{ currentPath }}</span>
          </template>
        </div>
        <div class="right">
          <template v-if="showRecycle">
            <el-button @click="fetchRecycle" :loading="recycleLoading">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
          </template>
          <template v-else>
            <el-button @click="enterRecycle">
              <el-icon><Delete /></el-icon> 回收站
            </el-button>
            <el-button @click="createFolder">
              <span class="iconfont icon-tianjia"></span> 新建文件夹
            </el-button>
            <el-button type="primary" @click="triggerUpload">
              <span class="iconfont icon-shangchuan"></span> 上传文件
            </el-button>
            <input
              ref="fileInput"
              type="file"
              style="display:none"
              @change="handleFileSelect"
            />
          </template>
        </div>
      </div>

      <!-- 配额显示 -->
      <div class="quota-bar">
        <span>存储空间: {{ formatSize(quota.used) }} / {{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
        <el-progress
          v-if="!quota.unlimited"
          :percentage="Math.min(Number(quota.percentage.toFixed(2)), 100)"
          :stroke-width="8"
        />
      </div>

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
          <el-table-column label="操作" width="100" fixed="right">
            <template #default="{ row }">
              <div class="actions" @click.stop>
                <el-tooltip v-if="!row.isDir" content="下载" placement="top">
                  <el-button type="primary" link :icon="Download" @click="downloadFile(row)" />
                </el-tooltip>
                <el-tooltip content="删除" placement="top">
                  <el-button type="danger" link :icon="Delete" @click="deleteFile(row)" />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
        </template>
      </el-table>

      <!-- 上传进度 -->
      <div v-if="uploadProgress" class="upload-tip">
        {{ uploadProgress }}
      </div>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.home-container {
  height: 100%;
}

.login-page {
  display: flex;
  justify-content: center;
  align-items: center;
  height: 100%;

  .login-box {
    text-align: center;
    padding: 40px;
    border-radius: 8px;
    background: #fff;
    box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);

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

.file-manager {
  padding: 16px;

  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;

    .left {
      display: flex;
      align-items: center;
      gap: 16px;
    }

    .right {
      display: flex;
      gap: 12px;
    }

    .path {
      color: #666;
      font-size: 14px;
    }
  }

  .quota-bar {
    margin-bottom: 16px;
    font-size: 14px;
    color: #666;
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
    margin-top: 16px;
    padding: 8px 16px;
    background: #f0f9eb;
    border-radius: 4px;
    color: #67c23a;
  }
}
</style>
