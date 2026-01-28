<script setup lang="ts">
import { computed } from 'vue'
import { Delete, DocumentCopy } from '@element-plus/icons-vue'
import type { DirectShareItem, ShareItem } from '@/api'

const props = defineProps<{
  shareTab: 'link' | 'direct'
  shareList: ShareItem[]
  directShareList: DirectShareItem[]
  loading: boolean
  onRowClick: (...args: any[]) => void
  copyShareLink: (item: ShareItem) => void
  revokeShare: (item: ShareItem) => void
  revokeDirectShare: (item: DirectShareItem) => void
  formatTime: (time: string | number) => string
  shortenAddress: (address?: string) => string
}>()

const linkRows = computed<ShareItem[]>(() => props.shareList)
const directRows = computed<DirectShareItem[]>(() => props.directShareList)
</script>

<template>
  <el-table
    v-if="shareTab === 'link'"
    class="desktop-only"
    :data="linkRows"
    v-loading="loading"
    style="width: 100%"
    height="100%"
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
          <el-tooltip content="复制链接" placement="top">
            <el-button link :icon="DocumentCopy" @click="copyShareLink(row)" />
          </el-tooltip>
          <el-tooltip content="取消分享" placement="top">
            <el-button type="danger" link :icon="Delete" @click="revokeShare(row)" />
          </el-tooltip>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <el-table
    v-else
    class="desktop-only"
    :data="directRows"
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
    <el-table-column label="目标钱包" min-width="200">
      <template #default="{ row }">
        <span class="mono">{{ shortenAddress(row.targetWallet) }}</span>
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
          <el-tooltip content="取消分享" placement="top">
            <el-button type="danger" link :icon="Delete" @click="revokeDirectShare(row)" />
          </el-tooltip>
        </div>
      </template>
    </el-table-column>
  </el-table>

  <div class="mobile-only card-list" v-loading="loading">
    <el-empty
      v-if="!loading && shareTab === 'link' && !linkRows.length"
      description="暂无数据"
    />
    <el-empty
      v-else-if="!loading && shareTab === 'direct' && !directRows.length"
      description="暂无数据"
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
            <span class="card-meta-value">{{ row.viewCount ?? 0 }}/{{ row.downloadCount ?? 0 }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">
              {{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}
            </span>
          </div>
          <div class="card-actions card-actions-inline">
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
        </div>
        <div class="card-footer" @click.stop>
          <div class="card-meta card-meta-compact">
            <span class="card-meta-value">{{ shortenAddress(row.targetWallet) }}</span>
            <span class="card-meta-sep">·</span>
            <span class="card-meta-value">
              {{ row.expiresAt ? formatTime(row.expiresAt) : '永不过期' }}
            </span>
          </div>
          <div class="card-actions card-actions-inline">
            <el-button size="small" circle type="danger" :icon="Delete" @click="revokeDirectShare(row)" />
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped src="./homeShared.scss"></style>
