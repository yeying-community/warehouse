<script setup lang="ts">
import { computed } from 'vue'
import { Delete, DocumentCopy, FolderOpened, MoreFilled, View } from '@element-plus/icons-vue'
import type { DirectShareItem, ShareItem } from '@/api'

const props = defineProps<{
  isMobile: boolean
  shareTab: 'link' | 'direct'
  shareList: ShareItem[]
  directShareList: DirectShareItem[]
  loading: boolean
  onRowClick: (...args: any[]) => void
  copyShareLink: (item: ShareItem) => void
  openShareLocation: (item: ShareItem) => void
  revokeShare: (item: ShareItem) => void
  revokeDirectShare: (item: DirectShareItem) => void
  openDirectShareDetail: (item: DirectShareItem) => void
  formatTime: (time: string | number) => string
  shortenAddress: (address?: string) => string
  isDirectShareOwner: (item: DirectShareItem) => boolean
}>()

const linkRows = computed<ShareItem[]>(() => props.shareList)
const directRows = computed<DirectShareItem[]>(() => props.directShareList)
const linkEmptyText = '还没有创建任何分享链接'
const directEmptyText = '还没有任何分享对象'

function getDirectRelationLabel(row: DirectShareItem): string {
  return props.isDirectShareOwner(row) ? '我分享的' : '分享我的'
}

function getDirectRelationType(row: DirectShareItem): 'primary' | 'success' {
  return props.isDirectShareOwner(row) ? 'primary' : 'success'
}

function handleLinkCommand(row: ShareItem, command: string | number) {
  const action = String(command)
  if (action === 'copy') {
    props.copyShareLink(row)
    return
  }
  if (action === 'revoke') {
    props.revokeShare(row)
  }
}

function getShareModeLabel(row: ShareItem): string {
  return row.mode === 'preview' ? '浏览器打开' : '仅下载'
}

function handleDirectCommand(row: DirectShareItem, command: string | number) {
  const action = String(command)
  if (action === 'detail') {
    props.openDirectShareDetail(row)
    return
  }
  if (action === 'revoke') {
    props.revokeDirectShare(row)
  }
}
</script>

<template>
  <el-table
    v-if="!isMobile && shareTab === 'link'"
    :data="linkRows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    :empty-text="linkEmptyText"
    @row-click="onRowClick"
  >
    <el-table-column label="名称" min-width="200">
      <template #default="{ row }">
        <div class="file-name">
          <span class="iconfont icon-wenjian1"></span>
          <span class="name" :title="row.name">{{ row.name }}</span>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="访问/下载" width="110">
      <template #default="{ row }">
        {{ row.viewCount ?? 0 }}/{{ row.downloadCount ?? 0 }}
      </template>
    </el-table-column>
    <el-table-column label="打开方式" width="120">
      <template #default="{ row }">
        {{ getShareModeLabel(row) }}
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
    <el-table-column label="操作" width="120" fixed="right" align="left" header-align="left">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip content="打开所在资产" placement="top">
            <el-button type="primary" link :icon="FolderOpened" @click="openShareLocation(row)" />
          </el-tooltip>
          <el-dropdown @command="handleLinkCommand(row, $event)">
            <el-button type="primary" link :icon="MoreFilled" />
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="copy">复制链接</el-dropdown-item>
                <el-dropdown-item command="revoke">取消分享</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <el-table
    v-else-if="!isMobile"
    :data="directRows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
    :empty-text="directEmptyText"
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
    <el-table-column label="关系" width="110">
      <template #default="{ row }">
        <el-tag :type="getDirectRelationType(row)" size="small" effect="light">
          {{ getDirectRelationLabel(row) }}
        </el-tag>
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
    <el-table-column label="操作" width="120" fixed="right" align="left" header-align="left">
      <template #default="{ row }">
        <div class="actions" @click.stop>
          <el-tooltip content="详情" placement="top">
            <el-button type="primary" link :icon="View" @click="openDirectShareDetail(row)" />
          </el-tooltip>
          <el-dropdown @command="handleDirectCommand(row, $event)">
            <el-button type="primary" link :icon="MoreFilled" />
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="detail">查看详情</el-dropdown-item>
                <el-dropdown-item v-if="isDirectShareOwner(row)" command="revoke">取消分享</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div v-else class="card-list" v-loading="loading">
    <el-empty
      v-if="!loading && shareTab === 'link' && !linkRows.length"
      :description="linkEmptyText"
    />
    <el-empty
      v-else-if="!loading && shareTab === 'direct' && !directRows.length"
      :description="directEmptyText"
    />
    <template v-if="shareTab === 'link'">
      <div
        v-for="row in linkRows"
        :key="row.token"
        class="card-item"
        @click="onRowClick(row)"
      >
        <div class="card-header">
          <div class="file-name">
            <span class="iconfont icon-wenjian1"></span>
            <span class="name" :title="row.name">{{ row.name }}</span>
          </div>
        </div>
        <div class="card-footer" @click.stop>
          <div class="card-meta card-meta-compact">
            <span class="card-meta-value">{{ getShareModeLabel(row) }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">{{ row.viewCount ?? 0 }}/{{ row.downloadCount ?? 0 }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">
              {{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}
            </span>
          </div>
          <div class="card-actions card-actions-inline">
            <el-button size="small" circle type="primary" :icon="FolderOpened" @click="openShareLocation(row)" />
            <el-button size="small" circle :icon="DocumentCopy" @click="copyShareLink(row)" />
            <el-button size="small" circle type="danger" :icon="Delete" @click="revokeShare(row)" />
          </div>
        </div>
      </div>
    </template>
    <template v-else>
      <div
        v-for="row in directRows"
        :key="row.id"
        class="card-item"
        @click="onRowClick(row)"
      >
        <div class="card-header">
          <div class="file-name">
            <span class="iconfont" :class="row.isDir ? 'icon-wenjianjia' : 'icon-wenjian1'"></span>
            <span class="name" :title="row.name">{{ row.name }}</span>
          </div>
          <el-tag :type="getDirectRelationType(row)" size="small" effect="light">
            {{ getDirectRelationLabel(row) }}
          </el-tag>
        </div>
        <div class="card-footer" @click.stop>
          <div class="card-meta card-meta-compact">
            <span class="card-meta-value">{{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">{{ row.createdAt ? formatTime(row.createdAt) : '-' }}</span>
          </div>
          <div class="card-actions card-actions-inline">
            <el-button size="small" circle type="primary" :icon="View" @click="openDirectShareDetail(row)" />
            <el-button
              v-if="isDirectShareOwner(row)"
              size="small"
              circle
              type="danger"
              :icon="Delete"
              @click="revokeDirectShare(row)"
            />
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
