<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { quotaApi } from '@/api'
import { isLoggedIn, hasWallet, loginWithWallet } from '@/plugins/auth'

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
    console.log('PROPFIND response text:', text)

    fileList.value = parsePropfindResponse(text)
    console.log('parsed items:', fileList.value)
    currentPath.value = path
  } catch (error) {
    console.error('获取文件列表失败:', error)
  } finally {
    loading.value = false
  }
}

// 解析 WebDAV PROPFIND 响应
function parsePropfindResponse(xml: string): FileItem[] {
  const items: FileItem[] = []
  // 使用 /i 忽略大小写，因为 XML 中可能是 <D:href> 或 <d:href>
  const nameRegex = /<[Dd]:displayname>([^<]+)<\/[Dd]:displayname>/gi
  const sizeRegex = /<[Dd]:getcontentlength>([^<]+)<\/[Dd]:getcontentlength>/gi
  const lastModRegex = /<[Dd]:getlastmodified>([^<]+)<\/[Dd]:getlastmodified>/gi
  const hrefRegex = /<[Dd]:href>([^<]+)<\/[Dd]:href>/gi

  console.log('parsePropfindResponse xml:', xml)
  console.log('hrefRegex lastIndex:', hrefRegex.lastIndex)

  const hrefs = [...xml.matchAll(hrefRegex)]
  const names = [...xml.matchAll(nameRegex)]
  const sizes = [...xml.matchAll(sizeRegex)]
  const lastMods = [...xml.matchAll(lastModRegex)]

  console.log('hrefs count:', hrefs.length, 'names count:', names.length)
  console.log('names:', names.map(n => n[1]))

  // 从 hrefs 中提取路径，排除根目录
  for (let i = 1; i < hrefs.length; i++) {
    const href = decodeURIComponent(hrefs[i][1])
    // 从 href 路径提取名称，如 /test/ → test
    let name = href.split('/').filter(Boolean).pop() || ''
    // 优先使用 displayname
    if (names[i - 1]?.[1]) {
      name = names[i - 1][1]
    }
    const size = parseInt(sizes[i - 1]?.[1] || '0')
    const lastMod = lastMods[i - 1]?.[1] || ''

    // 排除自身
    if (name === '') continue

    items.push({
      name,
      path: href,
      isDir: href.endsWith('/'),
      size,
      modified: lastMod
    })
  }

  return items
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

  // 使用 cookie 认证，让浏览器自动处理 Range 请求
  // 通过隐藏的 iframe 下载文件
  const iframe = document.createElement('iframe')
  iframe.style.display = 'none'
  iframe.src = apiPath

  iframe.onload = function() {
    uploadProgress.value = null
  }

  iframe.onerror = function() {
    uploadProgress.value = null
    alert('下载失败: 网络错误')
  }

  document.body.appendChild(iframe)

  // 几秒后移除 iframe
  setTimeout(() => {
    document.body.removeChild(iframe)
    uploadProgress.value = null
  }, 5000)
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
    setTimeout(() => {
      uploadProgress.value = null
      fetchFiles(currentPath.value)
    }, 1000)
  } catch (error) {
    uploadProgress.value = `上传失败: ${error}`
  }
  // 清空 input，允许重复上传同一文件
  input.value = ''
}

// 删除文件
async function deleteFile(item: FileItem) {
  if (!confirm(`确定删除 ${item.name} 吗？`)) return

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
          <el-button @click="goParent" :disabled="currentPath === '/'">
            <span class="iconfont icon-fanhui"></span> 返回上级
          </el-button>
          <span class="path">当前路径: {{ currentPath }}</span>
        </div>
        <div class="right">
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
        </div>
      </div>

      <!-- 配额显示 -->
      <div class="quota-bar">
        <span>存储空间: {{ formatSize(quota.used) }} / {{ quota.unlimited ? '无限' : formatSize(quota.quota) }}</span>
        <el-progress
          v-if="!quota.unlimited"
          :percentage="Math.min(quota.percentage, 100)"
          :stroke-width="8"
        />
      </div>

      <!-- 文件列表 -->
      <el-table
        :data="fileList"
        v-loading="loading"
        style="width: 100%"
        @row-click="enterDirectory"
      >
        <el-table-column label="名称" min-width="300">
          <template #default="{ row }">
            <div class="file-name">
              <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
              <span class="name">{{ row.name }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="大小" width="120">
          <template #default="{ row }">
            {{ row.isDir ? '-' : formatSize(row.size) }}
          </template>
        </el-table-column>
        <el-table-column label="修改时间" width="180">
          <template #default="{ row }">
            {{ row.modified }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <div class="actions" @click.stop>
              <el-button v-if="!row.isDir" type="primary" link @click="downloadFile(row)">
                下载
              </el-button>
              <el-button type="danger" link @click="deleteFile(row)">
                删除
              </el-button>
            </div>
          </template>
        </el-table-column>
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