<script setup lang="ts">
import { Delete, FolderOpened } from '@element-plus/icons-vue'
import type { RecycleItem } from '@/api'

defineProps<{
  rows: RecycleItem[]
  loading: boolean
  onRowClick: (...args: any[]) => void
  formatRecycleFullPath: (path: string) => string
  formatRecycleLocation: (path: string) => string
  formatSize: (size: number) => string
  formatDeletedTime: (time: string) => string
  recoverFile: (item: RecycleItem) => void
  permanentlyDelete: (item: RecycleItem) => void
}>()
</script>

<template>
  <el-table
    class="desktop-only"
    :data="rows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    @row-click="onRowClick"
  >
    <el-table-column label="名称" min-width="200">
      <template #default="{ row }">
        <div class="file-name">
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="原始路径" min-width="220">
      <template #default="{ row }">
        <span class="path-cell" :title="formatRecycleFullPath(row.path)">
          {{ formatRecycleLocation(row.path) }}
        </span>
      </template>
    </el-table-column>
    <el-table-column label="大小" width="120">
      <template #default="{ row }">
        <span class="size-cell">{{ formatSize(row.size) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="删除时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ formatDeletedTime(row.deletedAt) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="140" fixed="right">
      <template #default="{ row }">
        <div class="actions" @click.stop>
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
  </el-table>

  <div class="mobile-only card-list" v-loading="loading">
    <el-empty v-if="!loading && !rows.length" description="暂无数据" />
    <div
      v-for="row in rows"
      :key="row.hash"
      class="card-item"
      @click="onRowClick(row)"
    >
      <div class="card-header">
        <div class="file-name">
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
        </div>
      </div>
      <div class="card-footer" @click.stop>
        <div class="card-meta card-meta-compact">
          <span class="card-meta-value path-cell" :title="formatRecycleFullPath(row.path)">
            {{ formatRecycleLocation(row.path) }}
          </span>
          <span class="card-meta-sep">·</span>
          <span class="card-meta-value">{{ formatDeletedTime(row.deletedAt) }}</span>
        </div>
        <div class="card-actions card-actions-inline">
          <el-button size="small" circle type="primary" @click="recoverFile(row)">
            <el-icon><FolderOpened /></el-icon>
          </el-button>
          <el-button size="small" circle type="danger" @click="permanentlyDelete(row)">
            <el-icon><Delete /></el-icon>
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
