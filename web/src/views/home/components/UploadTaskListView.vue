<script setup lang="ts">
import { FolderOpened, RefreshRight } from '@element-plus/icons-vue'
import type { UploadTask, UploadTaskStatus } from '../types'

const props = defineProps<{
  isMobile: boolean
  tasks: UploadTask[]
  formatSize: (size: number) => string
  formatTime: (time: string | number) => string
  retryTask: (task: UploadTask) => void
  openTaskLocation: (task: UploadTask) => void
}>()

function getTaskName(task: UploadTask): string {
  return task.relativePath || task.name
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

function taskStatusLabel(status: UploadTaskStatus): string {
  if (status === 'queued') return '等待中'
  if (status === 'uploading') return '上传中'
  if (status === 'success') return '已完成'
  return '失败'
}

function taskSpeed(task: UploadTask): string {
  if (task.status !== 'uploading' && task.status !== 'success') return '-'
  const speed = Number(task.uploadSpeed || 0)
  if (!Number.isFinite(speed) || speed <= 0) {
    return task.status === 'uploading' ? '计算中' : '-'
  }
  return `${props.formatSize(speed)}/s`
}

function taskSpeedLabel(task: UploadTask): string {
  return task.status === 'success' ? '平均速度' : '速度'
}

function taskUploaded(task: UploadTask): string {
  const uploaded = Math.min(Number(task.uploadedBytes || 0), Number(task.size || 0))
  if (!Number.isFinite(uploaded) || uploaded <= 0) return `0 / ${props.formatSize(task.size)}`
  return `${props.formatSize(uploaded)} / ${props.formatSize(task.size)}`
}

function handleRetry(task: UploadTask) {
  props.retryTask(task)
}

function canRetryTask(task: UploadTask): boolean {
  return task.status === 'failed' && (Boolean(task.file) || Boolean(task.uploadSessionId) || Boolean(task.uploadPayloadStorageKey))
}

function retryTooltip(task: UploadTask): string {
  if (task.encryptedRoot && task.uploadPayloadStorageKey) return '继续加密上传'
  return task.file ? '重试上传' : '重新选择文件继续上传'
}

function canOpenTaskLocation(task: UploadTask): boolean {
  return task.status === 'success' && !task.isShared && Boolean(task.targetPath)
}

function handleOpenTaskLocation(task: UploadTask) {
  props.openTaskLocation(task)
}
</script>

<template>
  <div class="task-list" :class="{ 'is-mobile': isMobile }">
    <el-empty v-if="!tasks.length" description="暂无任务" />
    <div v-for="row in tasks" :key="row.id" class="task-item">
      <div class="task-main">
        <div class="task-head">
          <span class="task-title" :title="getTaskName(row)">{{ getTaskName(row) }}</span>
          <span class="task-status" :class="`is-${row.status}`">{{ taskStatusLabel(row.status) }}</span>
        </div>
        <div class="task-meta">
          <span>{{ taskUploaded(row) }}</span>
          <span>{{ taskSpeedLabel(row) }} {{ taskSpeed(row) }}</span>
          <span>{{ formatTime(row.updatedAt) }}</span>
        </div>
        <el-progress
          :percentage="taskProgress(row)"
          :status="progressStatus(row.status)"
          :stroke-width="10"
        />
        <div v-if="row.error" class="task-error">{{ row.error }}</div>
      </div>
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
        <el-tooltip
          v-if="canRetryTask(row)"
          :content="retryTooltip(row)"
          placement="top"
        >
          <el-button
            size="small"
            circle
            :icon="RefreshRight"
            @click="handleRetry(row)"
          />
        </el-tooltip>
      </div>
    </div>
  </div>
</template>

<style scoped>
.task-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  height: 100%;
  overflow: auto;
  padding-right: 2px;
}

.task-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  padding: 12px;
  border: 1px solid #eef1f4;
  border-radius: 12px;
  background: #fff;
}

.task-main {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.task-head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.task-title {
  flex: 1;
  min-width: 0;
  font-weight: 600;
  color: #1f2d3d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-status {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: 999px;
  background: #f4f6f8;
  color: #606266;
  font-size: 12px;
}

.task-status.is-uploading {
  background: #ecf5ff;
  color: #409eff;
}

.task-status.is-success {
  background: #f0f9eb;
  color: #67c23a;
}

.task-status.is-failed {
  background: #fef0f0;
  color: #f56c6c;
}

.task-meta {
  display: flex;
  align-items: center;
  gap: 8px 14px;
  flex-wrap: wrap;
  color: #606266;
  font-size: 12px;
  line-height: 1.4;
}

.task-error {
  font-size: 12px;
  color: #f56c6c;
  word-break: break-all;
}

.task-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.task-list.is-mobile .task-item {
  grid-template-columns: 1fr;
}

.task-list.is-mobile .task-actions {
  justify-content: flex-start;
}
</style>
