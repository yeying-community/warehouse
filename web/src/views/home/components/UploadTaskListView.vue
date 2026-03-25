<script setup lang="ts">
import { FolderOpened, RefreshRight } from '@element-plus/icons-vue'
import type { UploadTask, UploadTaskStatus } from '../types'

const props = defineProps<{
  tasks: UploadTask[]
  formatSize: (size: number) => string
  formatTime: (time: string | number) => string
  retryTask: (task: UploadTask) => void
  openTaskLocation: (task: UploadTask) => void
}>()

function getTaskName(task: UploadTask): string {
  return task.relativePath || task.name
}

function statusLabel(status: UploadTaskStatus): string {
  switch (status) {
    case 'queued':
      return '等待中'
    case 'uploading':
      return '上传中'
    case 'success':
      return '已完成'
    case 'failed':
      return '失败'
    default:
      return '-'
  }
}

function statusTag(status: UploadTaskStatus): '' | 'success' | 'warning' | 'danger' | 'info' {
  switch (status) {
    case 'success':
      return 'success'
    case 'failed':
      return 'danger'
    case 'uploading':
      return 'warning'
    case 'queued':
      return 'info'
    default:
      return ''
  }
}

function progressStatus(status: UploadTaskStatus): '' | 'success' | 'exception' | 'warning' {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'exception'
  if (status === 'uploading') return 'warning'
  return ''
}

function taskProgress(task: UploadTask): number {
  const raw = Number.isFinite(task.progress) ? task.progress : 0
  return Math.min(100, Math.max(0, Math.round(raw)))
}

function handleRetry(task: UploadTask) {
  props.retryTask(task)
}

function canOpenTaskLocation(task: UploadTask): boolean {
  return task.status === 'success' && !task.isShared && Boolean(task.targetPath)
}

function handleOpenTaskLocation(task: UploadTask) {
  props.openTaskLocation(task)
}
</script>

<template>
  <el-table
    class="desktop-only"
    :data="tasks"
    empty-text="暂无上传任务"
    height="100%"
  >
    <el-table-column label="文件" min-width="240">
      <template #default="{ row }">
        <div class="task-name">
          <span class="task-title" :title="getTaskName(row)">{{ getTaskName(row) }}</span>
          <span v-if="row.isShared" class="task-meta">共享上传</span>
          <span v-if="row.error" class="task-error">{{ row.error }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="大小" width="120">
      <template #default="{ row }">
        <span class="size-cell">{{ formatSize(row.size) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="进度" min-width="200">
      <template #default="{ row }">
        <div class="task-progress">
          <el-progress
            :percentage="taskProgress(row)"
            :status="progressStatus(row.status)"
            :stroke-width="10"
          />
        </div>
      </template>
    </el-table-column>
    <el-table-column label="状态" width="120">
      <template #default="{ row }">
        <el-tag :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column label="更新时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ formatTime(row.updatedAt) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="120" fixed="right" align="left" header-align="left">
      <template #default="{ row }">
        <div class="task-actions">
          <el-tooltip
            v-if="canOpenTaskLocation(row)"
            content="打开所在目录"
            placement="top"
          >
            <el-button
              size="small"
              circle
              type="primary"
              plain
              :icon="FolderOpened"
              @click="handleOpenTaskLocation(row)"
            />
          </el-tooltip>
          <el-button
            v-if="row.status === 'failed'"
            size="small"
            type="primary"
            @click="handleRetry(row)"
          >
            重试
          </el-button>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div class="mobile-only card-list">
    <el-empty v-if="!tasks.length" description="暂无上传任务" />
    <div v-for="row in tasks" :key="row.id" class="card-item">
      <div class="card-header">
        <div class="card-title" :title="getTaskName(row)">{{ getTaskName(row) }}</div>
        <el-tag size="small" :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag>
      </div>
      <div class="card-meta-compact">
        <span class="card-meta-value">{{ formatTime(row.updatedAt) }}</span>
        <span class="card-meta-sep">·</span>
        <span class="card-meta-value">{{ formatSize(row.size) }}</span>
      </div>
      <el-progress
        :percentage="taskProgress(row)"
        :status="progressStatus(row.status)"
        :stroke-width="10"
      />
      <div v-if="row.error" class="task-error">{{ row.error }}</div>
      <div class="card-actions card-actions-inline">
        <el-tooltip
          v-if="canOpenTaskLocation(row)"
          content="打开所在目录"
          placement="top"
        >
          <el-button
            size="small"
            circle
            type="primary"
            plain
            :icon="FolderOpened"
            @click="handleOpenTaskLocation(row)"
          />
        </el-tooltip>
        <el-button
          v-if="row.status === 'failed'"
          size="small"
          circle
          :icon="RefreshRight"
          @click="handleRetry(row)"
        />
      </div>
    </div>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
<style scoped>
.task-name {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.task-title {
  font-weight: 600;
  color: #1f2d3d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-meta {
  font-size: 12px;
  color: #909399;
}

.task-error {
  font-size: 12px;
  color: #f56c6c;
  word-break: break-all;
}

.task-progress :deep(.el-progress) {
  min-width: 160px;
}

.card-item .el-progress {
  margin-top: 6px;
}

.task-actions {
  display: flex;
  justify-content: flex-start;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
