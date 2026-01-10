<script lang="ts" setup>
import { ref, onMounted } from 'vue'
import { Wallet } from '@element-plus/icons-vue'
import { isLoggedIn, getCurrentAccount, logout, hasWallet, loginWithWallet, getWalletName } from '@/plugins/auth'

const isAuth = ref(false)
const account = ref<string | null>(null)
const walletInfo = ref({ present: false, name: '' })

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
          连接 {{ walletInfo.name }}
        </el-button>
        <span v-else class="no-wallet">未检测到钱包</span>
      </template>

      <!-- 已登录 -->
      <span v-if="isAuth && account" class="account">
        {{ account.slice(0, 6) }}...{{ account.slice(-4) }}
      </span>

      <el-button v-if="isAuth" @click="handleLogout">
        退出
      </el-button>
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

    .no-wallet {
      color: #909399;
      font-size: 14px;
    }
  }
}
</style>
