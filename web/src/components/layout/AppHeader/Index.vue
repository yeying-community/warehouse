<script lang="ts" setup>
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Notebook, SwitchButton, User, Wallet } from '@element-plus/icons-vue'
import { isLoggedIn, getCurrentAccount, logout, hasWallet, loginWithWallet, getWalletName } from '@/plugins/auth'

const isAuth = ref(false)
const account = ref<string | null>(null)
const walletInfo = ref({ present: false, name: '' })
const activeView = ref<string | null>(localStorage.getItem('webdav:lastView'))

onMounted(() => {
  isAuth.value = isLoggedIn()
  account.value = getCurrentAccount()
  walletInfo.value = {
    present: hasWallet(),
    name: getWalletName()
  }
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

function navigateTo(view: 'quotaManage' | 'addressBook') {
  window.dispatchEvent(new CustomEvent('webdav:navigate', { detail: { view } }))
}

function handleMenuCommand(command: string) {
  if (command === 'logout') {
    handleLogout()
    return
  }
  if (command === 'userCenter') {
    activeView.value = 'quotaManage'
    navigateTo('quotaManage')
    return
  }
  if (command === 'addressBook') {
    activeView.value = 'addressBook'
    navigateTo('addressBook')
  }
}

function handleViewChanged(event: Event) {
  const customEvent = event as CustomEvent<{ view?: string }>
  if (customEvent?.detail?.view) {
    activeView.value = customEvent.detail.view
  }
}

onMounted(() => {
  window.addEventListener('webdav:view-changed', handleViewChanged as EventListener)
})

onBeforeUnmount(() => {
  window.removeEventListener('webdav:view-changed', handleViewChanged as EventListener)
})
</script>

<template>
  <div class="myHeader">
    <div class="logo">
      <img src="/logo.svg" alt="Logo" class="logo-icon" />
    </div>

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
      <el-dropdown v-if="isAuth && account" trigger="click" @command="handleMenuCommand">
        <span class="account account-trigger">
          {{ account.slice(0, 6) }}...{{ account.slice(-4) }}
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item
              command="userCenter"
              :icon="User"
              :class="{ 'dropdown-active': activeView === 'quotaManage' }"
            >
              用户中心
            </el-dropdown-item>
            <el-dropdown-item
              command="addressBook"
              :icon="Notebook"
              :class="{ 'dropdown-active': activeView === 'addressBook' }"
            >
              地址簿
            </el-dropdown-item>
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

    .logo-icon {
      width: 128px;
      height: 128px;
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
  }
}

:deep(.dropdown-active) {
  color: #409eff;
  font-weight: 600;
}
</style>
