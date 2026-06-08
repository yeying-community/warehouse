<script lang="ts" setup>
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { Bell, SwitchButton, Wallet } from '@element-plus/icons-vue'
import { notificationApi, type AdminNotificationCreatePayload, type NotificationItem, type NotificationPreferenceItem } from '@/api'
import { isLoggedIn, getCurrentAccount, logout, loginWithWallet, getWalletName, watchWalletAccounts, watchWalletProvider, markAccountChanged } from '@/plugins/auth'

const isAuth = ref(false)
const account = ref<string | null>(null)
const walletInfo = ref({ present: false, name: '' })
const notificationOpen = ref(false)
const notificationLoading = ref(false)
const notificationTab = ref<'user' | 'admin'>('user')
const notificationView = ref<'messages' | 'preferences' | 'announce'>('messages')
const userNotifications = ref<NotificationItem[]>([])
const adminNotifications = ref<NotificationItem[]>([])
const notificationPreferences = ref<Array<{ type: string; enabled: boolean }>>([])
const userUnreadCount = ref(0)
const adminUnreadCount = ref(0)
const adminNotificationsAvailable = ref(false)
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
const totalUnreadCount = computed(() => userUnreadCount.value + adminUnreadCount.value)
let stopAccountWatch: (() => void) | null = null
let stopWalletProviderWatch: (() => void) | null = null
let notificationTimer: number | null = null
let userNotificationStream: EventSource | null = null
let adminNotificationStream: EventSource | null = null

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
})

onMounted(() => {
  void (async () => {
    stopAccountWatch = await watchWalletAccounts(({ account: next }) => {
      if (!next) return
      const current = account.value?.toLowerCase()
      if (current && current !== next.toLowerCase() && isAuth.value) {
        markAccountChanged(next)
        logout()
        return
      }
      account.value = next
    })
  })()
})

async function handleConnect() {
  try {
    await loginWithWallet()
    window.location.reload()
  } catch (error) {
    console.error('连接失败:', error)
  }
}

function handleLogout() {
  logout()
}

function handleMenuCommand(command: string) {
  if (command === 'logout') {
    handleLogout()
  }
}

async function refreshUnreadCounts() {
  try {
    const userCount = await notificationApi.unreadCount()
    userUnreadCount.value = Number(userCount.count || 0)
  } catch (error) {
    console.warn('获取消息未读数失败:', error)
  }

  try {
    const adminCount = await notificationApi.adminUnreadCount()
    adminUnreadCount.value = Number(adminCount.count || 0)
    adminNotificationsAvailable.value = true
  } catch {
    adminUnreadCount.value = 0
    adminNotificationsAvailable.value = false
    if (notificationTab.value === 'admin') {
      notificationTab.value = 'user'
    }
  }
}

async function loadNotifications() {
  notificationLoading.value = true
  try {
    const userResult = await notificationApi.list(20)
    userNotifications.value = userResult.items || []
    if (adminNotificationsAvailable.value) {
      const adminResult = await notificationApi.adminList(20)
      adminNotifications.value = adminResult.items || []
    }
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

async function markNotificationRead(item: NotificationItem, scope: 'user' | 'admin') {
  if (item.readAt) return
  try {
    if (scope === 'admin') {
      await notificationApi.adminMarkRead([item.id])
    } else {
      await notificationApi.markRead([item.id])
    }
    item.readAt = new Date().toISOString()
    await refreshUnreadCounts()
  } catch (error) {
    console.warn('标记消息已读失败:', error)
  }
}

async function markAllNotificationsRead() {
  try {
    if (notificationTab.value === 'admin') {
      await notificationApi.adminMarkAllRead()
      adminNotifications.value = adminNotifications.value.map(item => ({ ...item, readAt: item.readAt || new Date().toISOString() }))
    } else {
      await notificationApi.markAllRead()
      userNotifications.value = userNotifications.value.map(item => ({ ...item, readAt: item.readAt || new Date().toISOString() }))
    }
    await refreshUnreadCounts()
  } catch (error) {
    console.warn('全部标记已读失败:', error)
  }
}

function currentNotifications() {
  return notificationTab.value === 'admin' ? adminNotifications.value : userNotifications.value
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

async function handleNotificationClick(item: NotificationItem, scope: 'user' | 'admin') {
  await markNotificationRead(item, scope)
  if (!item.actionUrl) return
  notificationOpen.value = false
  if (item.actionUrl === '#shared-with-me') {
    window.dispatchEvent(new CustomEvent('warehouse:navigate', { detail: { view: 'sharedWithMe' } }))
    return
  }
  if (item.actionUrl === '#admin-quota') {
    window.dispatchEvent(new CustomEvent('warehouse:navigate', { detail: { view: 'quotaManage', section: 'adminQuota' } }))
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
      userUnreadCount.value = Number(data.user || 0)
    })
  } catch (error) {
    console.warn('用户消息推送连接失败:', error)
  }
  try {
    adminNotificationStream = new EventSource(notificationApi.adminStreamUrl(), { withCredentials: true })
    adminNotificationStream.addEventListener('unread', (event) => {
      const data = JSON.parse((event as MessageEvent).data || '{}')
      adminUnreadCount.value = Number(data.admin || 0)
      adminNotificationsAvailable.value = true
    })
    adminNotificationStream.onerror = () => {
      adminNotificationsAvailable.value = false
      adminNotificationStream?.close()
      adminNotificationStream = null
    }
  } catch {
    adminNotificationsAvailable.value = false
  }
}

function stopNotificationStreams() {
  userNotificationStream?.close()
  adminNotificationStream?.close()
  userNotificationStream = null
  adminNotificationStream = null
}

onBeforeUnmount(() => {
  if (notificationTimer !== null) {
    window.clearInterval(notificationTimer)
  }
  stopNotificationStreams()
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
          连接钱包
        </el-button>
        <span v-else class="no-wallet">未检测到钱包</span>
      </template>

      <!-- 已登录 -->
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
                v-if="adminNotificationsAvailable"
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
            <el-tabs v-model="notificationTab" class="notification-tabs">
              <el-tab-pane :label="`我的 ${userUnreadCount ? `(${userUnreadCount})` : ''}`" name="user" />
              <el-tab-pane
                v-if="adminNotificationsAvailable"
                :label="`管理员 ${adminUnreadCount ? `(${adminUnreadCount})` : ''}`"
                name="admin"
              />
            </el-tabs>
            <div v-if="!currentNotifications().length" class="notification-empty">暂无消息</div>
            <div v-else class="notification-list">
              <button
                v-for="item in currentNotifications()"
                :key="item.id"
                type="button"
                class="notification-item"
                :class="[{ unread: !item.readAt }, notificationSeverityClass(item.severity)]"
                @click="handleNotificationClick(item, notificationTab)"
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
  }
}

.notification-panel {
  min-height: 160px;
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
