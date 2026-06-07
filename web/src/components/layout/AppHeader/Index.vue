<script lang="ts" setup>
import { computed, ref, onMounted, onBeforeUnmount } from 'vue'
import { Bell, SwitchButton, Wallet } from '@element-plus/icons-vue'
import { notificationApi, type NotificationItem } from '@/api'
import { isLoggedIn, getCurrentAccount, logout, loginWithWallet, getWalletName, watchWalletAccounts, watchWalletProvider, markAccountChanged } from '@/plugins/auth'

const isAuth = ref(false)
const account = ref<string | null>(null)
const walletInfo = ref({ present: false, name: '' })
const notificationOpen = ref(false)
const notificationLoading = ref(false)
const notificationTab = ref<'user' | 'admin'>('user')
const userNotifications = ref<NotificationItem[]>([])
const adminNotifications = ref<NotificationItem[]>([])
const userUnreadCount = ref(0)
const adminUnreadCount = ref(0)
const adminNotificationsAvailable = ref(false)
const totalUnreadCount = computed(() => userUnreadCount.value + adminUnreadCount.value)
let stopAccountWatch: (() => void) | null = null
let stopWalletProviderWatch: (() => void) | null = null
let notificationTimer: number | null = null

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

async function handleNotificationOpenChange(open: boolean) {
  notificationOpen.value = open
  if (open) {
    await loadNotifications()
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

onBeforeUnmount(() => {
  if (notificationTimer !== null) {
    window.clearInterval(notificationTimer)
  }
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
            <el-button size="small" text :disabled="!currentNotifications().length" @click="markAllNotificationsRead">
              全部已读
            </el-button>
          </div>
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

</style>
