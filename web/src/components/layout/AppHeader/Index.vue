<script lang="ts" setup>
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { Bell, Notebook, SwitchButton, Wallet } from '@element-plus/icons-vue'
import { storeToRefs } from 'pinia'
import { notificationApi, type AdminNotificationCreatePayload, type NotificationItem, type NotificationPreferenceItem } from '@/api'
import { AUTH_CHANGED_EVENT, isLoggedIn, getCurrentAccount, logout, loginWithWallet, focusPendingWalletApproval, getWalletName, watchWalletAccounts, watchWalletProvider } from '@/plugins/auth'
import { useUploadTaskStore } from '@/stores/uploadTaskStore'
import UploadTaskListView from '@/views/home/components/UploadTaskListView.vue'
import type { UploadTask } from '@/views/home/types'

const isAuth = ref(false)
const account = ref<string | null>(null)
const walletInfo = ref({ present: false, name: '' })
const walletConnectSubmitting = ref(false)
const notificationOpen = ref(false)
const notificationLoading = ref(false)
const notificationView = ref<'messages' | 'preferences' | 'announce'>('messages')
const notifications = ref<NotificationItem[]>([])
const notificationPreferences = ref<Array<{ type: string; enabled: boolean }>>([])
const unreadCount = ref(0)
const uploadTaskStore = useUploadTaskStore()
const { addSignal: uploadTaskAddSignal, dialogVisible: uploadTasksVisible, summary: uploadTaskSummary, tasks: uploadTasks } = storeToRefs(uploadTaskStore)
const taskPulse = ref(false)
const canAnnounce = ref(false)
const announcementSubmitting = ref(false)
const announcementForm = ref<AdminNotificationCreatePayload>({
  recipientRole: 'all',
  targetUsernames: [],
  title: '',
  content: '',
  severity: 'info',
  actionUrl: ''
})
const announcementTargetsText = ref('')
const totalUnreadCount = computed(() => unreadCount.value)
let stopAccountWatch: (() => void) | null = null
let stopWalletProviderWatch: (() => void) | null = null
let notificationTimer: number | null = null
let userNotificationStream: EventSource | null = null
let taskPulseTimer: number | null = null

watch(uploadTaskAddSignal, (value, oldValue) => {
  if (!isAuth.value || value === oldValue) return
  taskPulse.value = false
  if (taskPulseTimer !== null) {
    window.clearTimeout(taskPulseTimer)
  }
  requestAnimationFrame(() => {
    taskPulse.value = true
    taskPulseTimer = window.setTimeout(() => {
      taskPulse.value = false
      taskPulseTimer = null
    }, 900)
  })
})

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!uploadTaskStore.hasActiveTasks) return
  void uploadTaskStore.flushPersistedTasks()
  event.preventDefault()
  event.returnValue = ''
}

onMounted(() => {
  isAuth.value = isLoggedIn()
  account.value = getCurrentAccount()
  stopWalletProviderWatch = watchWalletProvider((present) => {
    walletInfo.value = {
      present,
      name: present ? getWalletName() : ''
    }
  })
  if (isAuth.value) {
    void refreshUnreadCounts()
    startNotificationStreams()
    notificationTimer = window.setInterval(() => {
      void refreshUnreadCounts()
    }, 30000)
  }
  window.addEventListener(AUTH_CHANGED_EVENT, handleAuthChanged as EventListener)
  window.addEventListener('beforeunload', handleBeforeUnload)
})

onMounted(() => {
  void (async () => {
    stopAccountWatch = await watchWalletAccounts(({ account: next }) => {
      if (!next) return
      const current = account.value?.toLowerCase()
      if (current && current !== next.toLowerCase() && isAuth.value) {
        logout({ reload: false })
        return
      }
      account.value = next
    })
  })()
})

async function handleConnect() {
  if (walletConnectSubmitting.value) {
    await focusPendingWalletApproval()
    return
  }
  walletConnectSubmitting.value = true
  try {
    await loginWithWallet()
    window.location.reload()
  } catch (error) {
    console.error('连接失败:', error)
  } finally {
    walletConnectSubmitting.value = false
  }
}

function handleLogout() {
  logout()
}

function handleAuthChanged(): void {
  isAuth.value = isLoggedIn()
  account.value = getCurrentAccount()
  if (!isAuth.value) {
    notificationOpen.value = false
    uploadTasksVisible.value = false
    unreadCount.value = 0
    stopNotificationStreams()
    if (notificationTimer !== null) {
      window.clearInterval(notificationTimer)
      notificationTimer = null
    }
  }
}

function formatTaskSize(bytes: number): string {
  const value = Number(bytes)
  if (!Number.isFinite(value) || value < 0) return '-'
  const units = ['B', 'K', 'M', 'G', 'T', 'P']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${Math.round(size)} ${units[index]}`
}

function formatTaskTime(time: string | number): string {
  if (time === null || time === undefined || time === '') return '-'
  const value = typeof time === 'string' && /^\d+$/.test(time.trim()) ? Number(time) : time
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  const pad = (num: number) => String(num).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

function emitTaskEvent(name: string, task: UploadTask) {
  window.dispatchEvent(new CustomEvent(name, { detail: { task } }))
}

function retryTask(task: UploadTask) {
  emitTaskEvent('warehouse:upload-task-retry', task)
}

function openTaskLocation(task: UploadTask) {
  emitTaskEvent('warehouse:upload-task-open', task)
}

function handleMenuCommand(command: string) {
  if (command === 'logout') {
    handleLogout()
  }
}

async function refreshUnreadCounts() {
  try {
    const userCount = await notificationApi.unreadCount()
    unreadCount.value = Number(userCount.count || 0)
  } catch (error) {
    console.warn('获取消息未读数失败:', error)
  }
}

async function loadNotifications() {
  notificationLoading.value = true
  try {
    const userResult = await notificationApi.list(20)
    notifications.value = userResult.items || []
    canAnnounce.value = Boolean(userResult.canAnnounce)
  } catch (error) {
    console.warn('获取消息列表失败:', error)
  } finally {
    notificationLoading.value = false
  }
}

async function loadPreferences() {
  try {
    const result = await notificationApi.preferences()
    notificationPreferences.value = normalizePreferences(result.items || [])
  } catch (error) {
    console.warn('获取消息偏好失败:', error)
  }
}

async function handleNotificationOpenChange(open: boolean) {
  notificationOpen.value = open
  if (open) {
    notificationView.value = 'messages'
    await loadNotifications()
    await loadPreferences()
    await refreshUnreadCounts()
  }
}

async function markNotificationRead(item: NotificationItem) {
  if (item.readAt) return
  try {
    await notificationApi.markRead([item.id])
    item.readAt = new Date().toISOString()
    await refreshUnreadCounts()
  } catch (error) {
    console.warn('标记消息已读失败:', error)
  }
}

async function markAllNotificationsRead() {
  try {
    await notificationApi.markAllRead()
    notifications.value = notifications.value.map(item => ({ ...item, readAt: item.readAt || new Date().toISOString() }))
    await refreshUnreadCounts()
  } catch (error) {
    console.warn('全部标记已读失败:', error)
  }
}

function currentNotifications() {
  return notifications.value
}

function normalizePreferences(items: NotificationPreferenceItem[]) {
  const labels = ['quota', 'share', 'system', 'admin_notice']
  const byType = new Map<string, boolean>()
  items.forEach(item => {
    const type = item.type || item.Type || ''
    if (!type) return
    byType.set(type, Boolean(item.enabled ?? item.Enabled ?? true))
  })
  return labels.map(type => ({
    type,
    enabled: byType.get(type) ?? true
  }))
}

function preferenceLabel(type: string): string {
  if (type === 'quota') return '额度提醒'
  if (type === 'share') return '分享提醒'
  if (type === 'system') return '系统提醒'
  if (type === 'admin_notice') return '管理员公告'
  return type
}

async function updatePreference(type: string, enabled: boolean) {
  const target = notificationPreferences.value.find(item => item.type === type)
  const previous = target?.enabled ?? true
  if (target) target.enabled = enabled
  try {
    await notificationApi.setPreference(type, enabled)
  } catch (error) {
    if (target) target.enabled = previous
    console.warn('更新消息偏好失败:', error)
  }
}

function handlePreferenceChange(type: string, value: string | number | boolean) {
  void updatePreference(type, Boolean(value))
}

function formatNotificationTime(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function notificationSeverityClass(severity: string): string {
  if (severity === 'error') return 'is-error'
  if (severity === 'warning') return 'is-warning'
  return 'is-info'
}

async function handleNotificationClick(item: NotificationItem) {
  await markNotificationRead(item)
  if (!item.actionUrl) return
  notificationOpen.value = false
  if (item.actionUrl === '#shared-with-me') {
    window.dispatchEvent(new CustomEvent('warehouse:navigate', { detail: { view: 'sharedWithMe' } }))
    return
  }
  if (item.actionUrl === '#admin-quota') {
    window.dispatchEvent(new CustomEvent('warehouse:navigate', { detail: { view: 'quotaManage', section: 'adminUsers' } }))
    return
  }
  if (item.actionUrl === '#quota') {
    window.dispatchEvent(new CustomEvent('warehouse:navigate', { detail: { view: 'quotaManage', section: 'account' } }))
  }
}

async function submitAnnouncement() {
  const title = announcementForm.value.title.trim()
  const content = announcementForm.value.content.trim()
  if (!title || !content) return
  announcementSubmitting.value = true
  try {
    const targetUsernames = announcementTargetsText.value
      .split(/[,\n]/)
      .map(item => item.trim())
      .filter(Boolean)
    await notificationApi.adminCreate({
      ...announcementForm.value,
      title,
      content,
      targetUsernames,
      actionUrl: announcementForm.value.actionUrl?.trim() || undefined
    })
    announcementForm.value = {
      recipientRole: 'all',
      targetUsernames: [],
      title: '',
      content: '',
      severity: 'info',
      actionUrl: ''
    }
    announcementTargetsText.value = ''
    notificationView.value = 'messages'
    await loadNotifications()
    await refreshUnreadCounts()
  } catch (error) {
    console.warn('发送公告失败:', error)
  } finally {
    announcementSubmitting.value = false
  }
}

function startNotificationStreams() {
  stopNotificationStreams()
  try {
    userNotificationStream = new EventSource(notificationApi.streamUrl(), { withCredentials: true })
    userNotificationStream.addEventListener('unread', (event) => {
      const data = JSON.parse((event as MessageEvent).data || '{}')
      unreadCount.value = Number(data.count ?? data.user ?? 0)
    })
  } catch (error) {
    console.warn('用户消息推送连接失败:', error)
  }
}

function stopNotificationStreams() {
  userNotificationStream?.close()
  userNotificationStream = null
}

onBeforeUnmount(() => {
  if (notificationTimer !== null) {
    window.clearInterval(notificationTimer)
  }
  if (taskPulseTimer !== null) {
    window.clearTimeout(taskPulseTimer)
  }
  stopNotificationStreams()
  window.removeEventListener(AUTH_CHANGED_EVENT, handleAuthChanged as EventListener)
  window.removeEventListener('beforeunload', handleBeforeUnload)
  stopWalletProviderWatch?.()
  stopAccountWatch?.()
})
</script>

<template>
  <div class="myHeader">
    <a
      class="logo"
      href="https://www.yeying.pub"
      target="_blank"
      rel="noopener noreferrer"
      aria-label="打开夜莺社区官网"
    >
      <img src="/logo.svg" alt="夜莺社区" class="logo-icon" />
    </a>

    <div class="right">
      <!-- 未登录 + 有钱包 -->
      <template v-if="!isAuth">
        <el-button
          v-if="walletInfo.present"
          type="primary"
          @click="handleConnect"
        >
          <el-icon><Wallet /></el-icon>
          {{ walletConnectSubmitting ? '查看钱包弹窗' : '连接钱包' }}
        </el-button>
        <span v-else class="no-wallet">未检测到钱包</span>
      </template>

      <!-- 已登录 -->
      <el-popover
        v-if="isAuth"
        v-model:visible="uploadTasksVisible"
        placement="bottom-end"
        width="min(760px, calc(100vw - 24px))"
        trigger="click"
        popper-class="task-popover"
      >
        <template #reference>
          <el-button class="task-button" :class="{ 'is-task-pulse': taskPulse }" title="任务" circle>
            <el-badge
              :value="uploadTaskSummary.total"
              :hidden="uploadTaskSummary.total === 0"
              :max="99"
              class="task-badge"
            >
              <el-icon><Notebook /></el-icon>
            </el-badge>
          </el-button>
        </template>
        <div class="task-panel">
          <div class="task-panel-head">
            <div class="task-panel-title">任务</div>
            <div class="task-panel-summary">
              <span>总数：{{ uploadTaskSummary.total }}</span>
              <span>等待中：{{ uploadTaskSummary.queued }}</span>
              <span>进行中：{{ uploadTaskSummary.uploading }}</span>
              <span>已完成：{{ uploadTaskSummary.success }}</span>
              <span>失败：{{ uploadTaskSummary.failed }}</span>
            </div>
            <el-button size="small" :disabled="uploadTaskSummary.success === 0" @click="uploadTaskStore.clearFinished()">
              清理已完成
            </el-button>
          </div>
          <div class="task-panel-list">
            <UploadTaskListView
              :is-mobile="false"
              :tasks="uploadTasks"
              :format-size="formatTaskSize"
              :format-time="formatTaskTime"
              :retry-task="retryTask"
              :open-task-location="openTaskLocation"
            />
          </div>
        </div>
      </el-popover>
      <el-popover
        v-if="isAuth"
        :visible="notificationOpen"
        placement="bottom-end"
        width="360"
        trigger="click"
        popper-class="notification-popover"
        @update:visible="handleNotificationOpenChange"
      >
        <template #reference>
          <el-button class="notification-button" circle>
            <el-badge :value="totalUnreadCount" :hidden="totalUnreadCount === 0" :max="99">
              <el-icon><Bell /></el-icon>
            </el-badge>
          </el-button>
        </template>
        <div class="notification-panel" v-loading="notificationLoading">
          <div class="notification-head">
            <span>消息</span>
            <div class="notification-head-actions">
              <el-button size="small" text @click="notificationView = notificationView === 'preferences' ? 'messages' : 'preferences'">
                偏好
              </el-button>
              <el-button
                v-if="canAnnounce"
                size="small"
                text
                @click="notificationView = notificationView === 'announce' ? 'messages' : 'announce'"
              >
                发公告
              </el-button>
              <el-button
                v-if="notificationView === 'messages'"
                size="small"
                text
                :disabled="!currentNotifications().length"
                @click="markAllNotificationsRead"
              >
                全部已读
              </el-button>
            </div>
          </div>
          <template v-if="notificationView === 'messages'">
            <div v-if="!currentNotifications().length" class="notification-empty">暂无消息</div>
            <div v-else class="notification-list">
              <button
                v-for="item in currentNotifications()"
                :key="item.id"
                type="button"
                class="notification-item"
                :class="[{ unread: !item.readAt }, notificationSeverityClass(item.severity)]"
                @click="handleNotificationClick(item)"
              >
                <span class="notification-dot" />
                <span class="notification-main">
                  <span class="notification-title">{{ item.title }}</span>
                  <span class="notification-content">{{ item.content }}</span>
                  <span class="notification-time">{{ formatNotificationTime(item.createdAt) }}</span>
                </span>
              </button>
            </div>
          </template>
          <div v-else-if="notificationView === 'preferences'" class="notification-preferences">
            <div
              v-for="item in notificationPreferences"
              :key="item.type"
              class="notification-preference-row"
            >
              <span>{{ preferenceLabel(item.type) }}</span>
              <el-switch
                v-model="item.enabled"
                @change="handlePreferenceChange(item.type, $event)"
              />
            </div>
          </div>
          <div v-else class="notification-announce">
            <el-select v-model="announcementForm.recipientRole" size="small" class="notification-field">
              <el-option label="所有用户" value="all" />
              <el-option label="指定用户" value="user" />
              <el-option label="管理员" value="admin" />
            </el-select>
            <el-input
              v-if="announcementForm.recipientRole === 'user'"
              v-model="announcementTargetsText"
              class="notification-field"
              type="textarea"
              :rows="2"
              placeholder="用户名，多个用逗号或换行分隔"
            />
            <el-input v-model="announcementForm.title" class="notification-field" size="small" placeholder="标题" />
            <el-input
              v-model="announcementForm.content"
              class="notification-field"
              type="textarea"
              :rows="3"
              placeholder="内容"
            />
            <el-select v-model="announcementForm.severity" size="small" class="notification-field">
              <el-option label="普通" value="info" />
              <el-option label="警告" value="warning" />
              <el-option label="重要" value="error" />
            </el-select>
            <el-input v-model="announcementForm.actionUrl" class="notification-field" size="small" placeholder="动作链接（可选）" />
            <div class="notification-announce-actions">
              <el-button size="small" @click="notificationView = 'messages'">取消</el-button>
              <el-button
                size="small"
                type="primary"
                :loading="announcementSubmitting"
                :disabled="!announcementForm.title.trim() || !announcementForm.content.trim()"
                @click="submitAnnouncement"
              >
                发送
              </el-button>
            </div>
          </div>
        </div>
      </el-popover>
      <el-dropdown v-if="isAuth && account" trigger="click" @command="handleMenuCommand">
        <span class="account account-trigger">
          {{ account.slice(0, 6) }}...{{ account.slice(-4) }}
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item divided command="logout" :icon="SwitchButton">退出</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.myHeader {
  display: flex;
  justify-content: space-between;
  align-items: center;
  height: 100%;
  border-bottom: 1px solid var(--el-border-color);

  .logo {
    display: flex;
    align-items: center;
    gap: 0px;
    text-decoration: none;

    .logo-icon {
      width: 42px;
      height: 42px;
      display: block;
      object-fit: contain;
    }

    .title {
      font-size: 20px;
      font-weight: bold;
      color: #303133;
    }
  }

  .right {
    display: flex;
    align-items: center;
    gap: 12px;

    .account {
      padding: 4px 12px;
      background: #f5f7fa;
      border-radius: 4px;
      font-size: 14px;
      color: #606266;
    }

    .account-trigger {
      cursor: pointer;
      user-select: none;
    }

    .no-wallet {
      color: #909399;
      font-size: 14px;
    }

    .notification-button {
      width: 34px;
      height: 34px;
      padding: 0;
    }

    .task-button {
      width: 34px;
      height: 34px;
      padding: 0;
      transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
    }

    .task-button.is-task-pulse {
      animation: task-added-pulse 0.9s ease;
    }

    .task-badge {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 18px;
      height: 18px;
    }
  }
}

@keyframes task-added-pulse {
  0% {
    transform: scale(1);
    box-shadow: 0 0 0 0 rgba(64, 158, 255, 0.32);
  }
  35% {
    transform: scale(1.12);
    box-shadow: 0 0 0 8px rgba(64, 158, 255, 0.16);
    border-color: #409eff;
  }
  100% {
    transform: scale(1);
    box-shadow: 0 0 0 0 rgba(64, 158, 255, 0);
  }
}

.notification-panel {
  min-height: 160px;
}

.task-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 320px;
  max-width: 100%;
}

.task-panel-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding-bottom: 10px;
  border-bottom: 1px solid #eef1f4;
}

@media (max-width: 640px) {
  .task-panel-head {
    grid-template-columns: 1fr;
    align-items: stretch;
  }

  .task-panel-summary {
    justify-content: flex-start;
  }
}

.task-panel-title {
  font-weight: 600;
  color: #1f2d3d;
}

.task-panel-summary {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  min-width: 0;
  color: #606266;
  font-size: 12px;
}

.task-panel-list {
  height: min(52vh, 420px);
  min-height: 260px;
  min-width: 0;
  overflow: hidden;
}

.notification-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
  margin-bottom: 6px;
}

.notification-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.notification-tabs {
  margin-bottom: 4px;
}

.notification-empty {
  color: #909399;
  font-size: 13px;
  text-align: center;
  padding: 34px 0;
}

.notification-list {
  max-height: 320px;
  overflow-y: auto;
}

.notification-item {
  width: 100%;
  display: grid;
  grid-template-columns: 8px 1fr;
  gap: 10px;
  padding: 10px 4px;
  border: 0;
  border-bottom: 1px solid #edf0f5;
  background: transparent;
  text-align: left;
  cursor: pointer;
}

.notification-item:hover {
  background: #f7f9fc;
}

.notification-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-top: 6px;
  background: #c0c4cc;
}

.notification-item.unread .notification-dot {
  background: #409eff;
}

.notification-item.is-warning.unread .notification-dot {
  background: #e6a23c;
}

.notification-item.is-error.unread .notification-dot {
  background: #f56c6c;
}

.notification-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.notification-title {
  color: #303133;
  font-weight: 600;
  font-size: 13px;
}

.notification-content {
  color: #606266;
  font-size: 12px;
  line-height: 1.45;
  word-break: break-word;
}

.notification-time {
  color: #a8abb2;
  font-size: 12px;
}

.notification-preferences {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 4px;
}

.notification-preference-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 34px;
  color: #303133;
  font-size: 13px;
  border-bottom: 1px solid #edf0f5;
}

.notification-announce {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 4px;
}

.notification-field {
  width: 100%;
}

.notification-announce-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 2px;
}

</style>
