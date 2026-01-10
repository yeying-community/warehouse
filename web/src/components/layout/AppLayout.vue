<script lang="ts" setup>
import AppHeader from './AppHeader/Index.vue'
</script>

<template>
  <div class="container_box">
    <el-container>
      <el-header>
        <AppHeader />
      </el-header>
      <el-main>
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
      padding: 0 10px;
      --el-header-height: 60px;
    }

    .el-main {
      padding: 16px;
      overflow: auto;
    }
  }
}
</style>