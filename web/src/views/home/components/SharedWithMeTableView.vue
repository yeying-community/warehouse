<script setup lang="ts">
import { computed } from 'vue'
import { Delete, Download, Edit, MoreFilled, View } from '@element-plus/icons-vue'
import type { DirectShareItem } from '@/api'
import type { FileItem } from '../types'

const props = defineProps<{
  sharedActive: DirectShareItem | null
  sharedWithMeList: DirectShareItem[]
  sharedEntries: FileItem[]
  loading: boolean
  onRowClick: (...args: any[]) => void
  canDragSharedItem: (item: FileItem) => boolean
  isDraggingSharedItem: (item: FileItem) => boolean
  isSharedMoveTarget: (item: FileItem) => boolean
  handleSharedItemDragStart: (event: DragEvent, item: FileItem) => void
  handleSharedItemDragEnd: () => void
  handleSharedDirectoryDragEnter: (event: DragEvent, item: FileItem) => void
  handleSharedDirectoryDragOver: (event: DragEvent, item: FileItem) => void
  handleSharedDirectoryDragLeave: (event: DragEvent, item: FileItem) => void
  handleSharedDirectoryDrop: (event: DragEvent, item: FileItem) => void
  formatTime: (time: string | number) => string
  formatSize: (size: number) => string
  shortenAddress: (address?: string) => string
  sharedCanRead: boolean
  sharedCanUpdate: boolean
  sharedCanDelete: boolean
  openShareDetail: (mode: 'share' | 'directShare' | 'receivedShare', item: DirectShareItem) => void
  downloadSharedRoot: (item: DirectShareItem) => void
  getPreviewMode: (item: FileItem) => 'text' | 'pdf' | 'word' | 'image' | null
  openFilePreview: (item: FileItem) => void
  openSharedEntryDetail: (item: FileItem) => void
  downloadSharedFile: (item: FileItem) => void
  renameSharedItem: (item: FileItem) => void
  deleteSharedItem: (item: FileItem) => void
}>()

const sharedListRows = computed<DirectShareItem[]>(() => props.sharedWithMeList)
const sharedEntryRows = computed<FileItem[]>(() => props.sharedEntries)

function handleMobileCommand(row: FileItem, command: string | number) {
  const action = String(command)
  if (action === 'preview') {
    props.openFilePreview(row)
    return
  }
  if (action === 'rename') {
    props.renameSharedItem(row)
    return
  }
  if (action === 'delete') {
    props.deleteSharedItem(row)
  }
}

function canPreview(row: FileItem): boolean {
  return props.sharedCanRead && !row.isDir && !!props.getPreviewMode(row)
}

function getSharedEntryRowClassName({ row }: { row: FileItem }) {
  return props.isSharedMoveTarget(row) ? 'move-target-row' : ''
}
</script>

<template>
  <el-table
    v-if="!sharedActive"
    class="desktop-only"
    :data="sharedListRows"
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
    <el-table-column label="分享人" min-width="160">
      <template #default="{ row }">
        <span>{{ row.ownerName || '-' }}</span>
      </template>
    </el-table-column>
    <el-table-column label="源钱包" min-width="200">
      <template #default="{ row }">
        <span class="mono">{{ shortenAddress(row.ownerWallet) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="过期时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</span>
      </template>
    </el-table-column>
    <el-table-column label="创建时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ row.createdAt ? formatTime(row.createdAt) : '-' }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="120" fixed="right">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip v-if="row.isDir" content="详情" placement="top">
            <el-button type="primary" link :icon="View" @click="openShareDetail('receivedShare', row)" />
          </el-tooltip>
          <el-tooltip v-else-if="row.permissions && row.permissions.includes('read')" content="下载" placement="top">
            <el-button type="primary" link :icon="Download" @click="downloadSharedRoot(row)" />
          </el-tooltip>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <el-table
    v-else
    class="desktop-only"
    :data="sharedEntryRows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    :row-class-name="getSharedEntryRowClassName"
    @row-click="onRowClick"
  >
    <el-table-column label="名称" min-width="280">
      <template #default="{ row }">
        <div
          class="file-name"
          :class="{
            'is-draggable': canDragSharedItem(row),
            'is-dragging': isDraggingSharedItem(row),
            'is-drop-target': isSharedMoveTarget(row)
          }"
          :draggable="canDragSharedItem(row)"
          @dragstart="handleSharedItemDragStart($event, row)"
          @dragend="handleSharedItemDragEnd"
          @dragenter="handleSharedDirectoryDragEnter($event, row)"
          @dragover="handleSharedDirectoryDragOver($event, row)"
          @dragleave="handleSharedDirectoryDragLeave($event, row)"
          @drop="handleSharedDirectoryDrop($event, row)"
        >
          <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
          <span v-if="isSharedMoveTarget(row)" class="drop-tip">移动到此目录</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="大小" width="120">
      <template #default="{ row }">
        <span class="size-cell">{{ row.isDir ? '-' : formatSize(row.size) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="修改时间" width="180">
      <template #default="{ row }">
        <span class="time-cell">{{ formatTime(row.modified) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="操作" width="220" fixed="right">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip v-if="row.isDir" content="详情" placement="top">
            <el-button type="primary" link :icon="View" @click="openSharedEntryDetail(row)" />
          </el-tooltip>
          <el-tooltip v-if="canPreview(row)" content="预览" placement="top">
            <el-button type="primary" link :icon="View" @click="openFilePreview(row)" />
          </el-tooltip>
          <el-tooltip v-if="!row.isDir && sharedCanRead" content="下载" placement="top">
            <el-button type="primary" link :icon="Download" @click="downloadSharedFile(row)" />
          </el-tooltip>
          <el-tooltip v-if="sharedCanUpdate" content="重命名" placement="top">
            <el-button type="primary" link :icon="Edit" @click="renameSharedItem(row)" />
          </el-tooltip>
          <el-tooltip v-if="sharedCanDelete" content="删除" placement="top">
            <el-button type="danger" link :icon="Delete" @click="deleteSharedItem(row)" />
          </el-tooltip>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div class="mobile-only card-list" v-loading="loading">
    <el-empty v-if="!loading && !sharedActive && !sharedListRows.length" description="暂无数据" />
    <el-empty v-else-if="!loading && sharedActive && !sharedEntryRows.length" description="暂无数据" />
    <template v-if="!sharedActive">
      <div
        v-for="row in sharedListRows"
        :key="row.id"
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
            <span class="card-meta-value mono">{{ shortenAddress(row.ownerWallet) }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</span>
          </div>
          <div class="card-actions card-actions-inline">
            <el-button
              v-if="row.isDir"
              size="small"
              circle
              :icon="View"
              @click="openShareDetail('receivedShare', row)"
            />
            <el-button
              v-else-if="row.permissions && row.permissions.includes('read')"
              size="small"
              circle
              type="primary"
              :icon="Download"
              @click="downloadSharedRoot(row)"
            />
          </div>
        </div>
      </div>
    </template>
    <template v-else>
      <div
        v-for="row in sharedEntryRows"
        :key="row.path"
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
            <span class="card-meta-value">{{ formatTime(row.modified) }}</span>
          </div>
          <div class="card-actions card-actions-inline">
            <el-button
              v-if="row.isDir"
              size="small"
              circle
              :icon="View"
              @click="openSharedEntryDetail(row)"
            />
            <el-button
              v-else-if="sharedCanRead"
              size="small"
              circle
              type="primary"
              :icon="Download"
              @click="downloadSharedFile(row)"
            />
            <el-dropdown @command="handleMobileCommand(row, $event)">
              <el-button size="small" circle :icon="MoreFilled" />
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item v-if="canPreview(row)" command="preview">预览</el-dropdown-item>
                  <el-dropdown-item v-if="sharedCanUpdate" command="rename">重命名</el-dropdown-item>
                  <el-dropdown-item v-if="sharedCanDelete" command="delete">删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
<style scoped>
:deep(.move-target-row td) {
  background: rgba(64, 158, 255, 0.08) !important;
}

:deep(.move-target-row .el-table__cell) {
  box-shadow: inset 0 -1px 0 rgba(64, 158, 255, 0.18);
}

.file-name.is-draggable {
  cursor: grab;
}

.file-name.is-draggable:active {
  cursor: grabbing;
}

.file-name.is-dragging {
  opacity: 0.48;
  transform: scale(0.985);
}

.file-name.is-drop-target {
  background: rgba(64, 158, 255, 0.12);
  border-radius: 10px;
  box-shadow: inset 0 0 0 1px rgba(64, 158, 255, 0.45);
  padding: 6px 8px;
  margin: -6px -8px;
}

.drop-tip {
  margin-left: 8px;
  font-size: 12px;
  color: #409eff;
  flex-shrink: 0;
}
</style>
