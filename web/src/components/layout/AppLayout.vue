<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import AppHeader from './AppHeader/Index.vue'
import { AUTH_CHANGED_EVENT, isLoggedIn } from '@/plugins/auth'

const showHeader = ref(false)

function refreshHeader(): void {
  showHeader.value = isLoggedIn()
}

function handleVisibilityChange(): void {
  if (!document.hidden) {
    refreshHeader()
  }
}

onMounted(() => {
  refreshHeader()
  window.addEventListener(AUTH_CHANGED_EVENT, refreshHeader as EventListener)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onBeforeUnmount(() => {
  window.removeEventListener(AUTH_CHANGED_EVENT, refreshHeader as EventListener)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<template>
  <div class="container_box">
    <el-container>
      <el-header v-if="showHeader">
        <AppHeader />
      </el-header>
      <el-main :class="{ 'with-header': showHeader }">
        <router-view />
      </el-main>
    </el-container>
  </div>
</template>

<style scoped lang="scss">
.container_box {
  height: 100vh;

  .el-container {
    height: 100%;

    .el-header {
      padding: 0 16px;
      --el-header-height: 60px;
    }

    .el-main {
      padding: 0;
      overflow: hidden;
      min-height: 0;

      &.with-header {
        padding: 16px;
      }
    }
  }
}
</style>
