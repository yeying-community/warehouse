<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { Delete, DocumentCopy, Edit, Refresh } from '@element-plus/icons-vue'
import { useAddressBookStore } from '@/stores/addressBookStore'
import { copyText } from '@/utils/clipboard'
import { shortenAddress } from '@/utils/address'
import type { AddressContact } from '@/api'

const addressBookStore = useAddressBookStore()
const emit = defineEmits<{
  (event: 'refresh'): void
}>()
const {
  addressGroups,
  addressGroupCounts,
  addressGroupLabel,
  addressGroupFilter,
  addressSearch,
  addressBookLoading,
  groupForm,
  groupSaving,
  contactForm,
  contactSaving,
  contactDialogVisible,
  filteredAddressContacts
} = storeToRefs(addressBookStore)
const {
  selectAddressGroup,
  createGroup,
  renameGroup,
  removeGroup,
  openCreateContactDialog,
  submitContact,
  resetContactForm,
  editContact,
  removeContact
} = addressBookStore

function showError(message: string, title = '错误') {
  void ElMessageBox.alert(message, title, {
    confirmButtonText: '确定',
    type: 'error',
    closeOnClickModal: false
  })
}

function copyContactAddress(contact: AddressContact) {
  const address = contact.walletAddress?.trim()
  if (!address) {
    showError('暂无钱包地址')
    return
  }
  copyText(address, '钱包地址已复制')
}
</script>

<template>
  <div class="address-page">
    <div class="address-hero">
      <div class="address-title-row">
        <div class="address-title">我的好友</div>
        <el-tooltip content="刷新" placement="top">
          <el-button
            class="refresh-button"
            circle
            size="small"
            :icon="Refresh"
            :disabled="addressBookLoading"
            :class="{ 'is-refreshing': addressBookLoading }"
            @click="emit('refresh')"
          />
        </el-tooltip>
      </div>
      <div class="address-sub">管理分享过的好友地址与分组，仅自己可见。</div>
      <div class="address-stats">
        <div class="stat-card">
          <span class="stat-label">联系人</span>
          <span class="stat-value">{{ addressGroupCounts.total }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">分组</span>
          <span class="stat-value">{{ addressGroups.length }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">未分组</span>
          <span class="stat-value">{{ addressGroupCounts.ungrouped }}</span>
        </div>
      </div>
    </div>

    <div class="address-layout">
      <div class="address-sidebar">
        <div class="address-card">
          <div class="card-title">分组</div>
          <div class="group-toolbar">
            <el-input v-model="groupForm.name" placeholder="分组名称" size="small" />
            <el-button type="primary" size="small" :loading="groupSaving" @click="createGroup">新增</el-button>
          </div>
          <div class="group-list">
            <div class="group-row" :class="{ active: addressGroupFilter === 'all' }">
              <button type="button" class="group-chip" @click="selectAddressGroup('all')">
                <span>全部</span>
                <span class="count">{{ addressGroupCounts.total }}</span>
              </button>
            </div>
            <div class="group-row" :class="{ active: addressGroupFilter === 'ungrouped' }">
              <button type="button" class="group-chip" @click="selectAddressGroup('ungrouped')">
                <span>未分组</span>
                <span class="count">{{ addressGroupCounts.ungrouped }}</span>
              </button>
            </div>
            <div v-if="!addressGroups.length" class="address-empty">暂无分组</div>
            <div v-for="group in addressGroups" :key="group.id" class="group-row" :class="{ active: addressGroupFilter === group.id }">
              <button type="button" class="group-chip" @click="selectAddressGroup(group.id)">
                <span>{{ group.name }}</span>
                <span class="count">{{ addressGroupCounts.groups[group.id] || 0 }}</span>
              </button>
              <div class="actions">
                <el-button size="small" text @click="renameGroup(group)">重命名</el-button>
                <el-button size="small" text type="danger" @click="removeGroup(group)">删除</el-button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div class="address-main">
        <div class="address-toolbar">
          <div class="toolbar-left">
            <el-input v-model="addressSearch" clearable placeholder="搜索名称 / 钱包 / 标签" size="small" />
            <span class="address-filter">当前分组：{{ addressGroupLabel }}</span>
          </div>
          <div class="toolbar-right">
            <el-button size="small" @click="openCreateContactDialog">新建联系人</el-button>
          </div>
        </div>

        <div class="address-card address-list-card">
          <div class="card-title">联系人列表</div>
          <div class="contact-header">
            <span>联系人</span>
            <span>钱包</span>
            <span>分组</span>
            <span>标签</span>
            <span class="contact-header-actions">操作</span>
          </div>
          <div class="contact-list">
            <div v-if="!filteredAddressContacts.length" class="address-empty">暂无联系人</div>
            <div v-for="contact in filteredAddressContacts" :key="contact.id" class="contact-row">
              <div class="contact-cell contact-name">{{ contact.name }}</div>
              <div class="contact-cell contact-wallet">
                <span class="mono wallet-text" :title="contact.walletAddress">
                  {{ shortenAddress(contact.walletAddress) }}
                </span>
                <el-tooltip content="复制钱包地址" placement="top">
                  <el-button
                    class="icon-button icon-button-inline"
                    link
                    :icon="DocumentCopy"
                    :disabled="!contact.walletAddress"
                    @click="copyContactAddress(contact)"
                  />
                </el-tooltip>
              </div>
              <div class="contact-cell contact-group">
                {{ addressGroups.find(g => g.id === contact.groupId)?.name || '未分组' }}
              </div>
              <div class="contact-cell contact-tags">
                <el-tag v-for="tag in contact.tags || []" :key="tag" size="small" type="info">
                  {{ tag }}
                </el-tag>
                <span v-if="!contact.tags || !contact.tags.length" class="address-tag-empty">无标签</span>
              </div>
              <div class="contact-cell contact-actions">
                <el-tooltip content="编辑" placement="top">
                  <el-button class="icon-button" link :icon="Edit" @click="editContact(contact)" />
                </el-tooltip>
                <el-tooltip content="删除" placement="top">
                  <el-button class="icon-button" type="danger" link :icon="Delete" @click="removeContact(contact)" />
                </el-tooltip>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <el-dialog
      v-model="contactDialogVisible"
      :title="contactForm.id ? '编辑联系人' : '新建联系人'"
      width="520px"
      @closed="resetContactForm"
    >
      <div class="contact-form">
        <el-input v-model="contactForm.name" placeholder="联系人名称" size="small" />
        <el-input v-model="contactForm.walletAddress" placeholder="钱包地址" size="small" />
        <el-select v-model="contactForm.groupId" placeholder="选择分组" size="small">
          <el-option label="未分组" value="" />
          <el-option v-for="group in addressGroups" :key="group.id" :label="group.name" :value="group.id" />
        </el-select>
        <el-select
          v-model="contactForm.tags"
          multiple
          filterable
          allow-create
          default-first-option
          collapse-tags
          placeholder="标签（回车添加）"
          size="small"
        />
      </div>
      <template #footer>
        <el-button @click="contactDialogVisible = false">取消</el-button>
        <el-button @click="resetContactForm">清空</el-button>
        <el-button type="primary" :loading="contactSaving" @click="submitContact">
          {{ contactForm.id ? '保存' : '新增' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style lang="scss" scoped>
.address-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 8px;
  min-height: 0;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.actions {
  display: flex;
  gap: 8px;
}

.actions .el-button {
  padding: 0 4px;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.address-hero {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.address-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.address-title {
  font-size: 20px;
  font-weight: 600;
  color: #1f2d3d;
}

.address-sub {
  font-size: 13px;
  color: #909399;
}

.address-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.stat-card {
  background: #f7f9fc;
  border-radius: 12px;
  padding: 10px 12px;
  border: 1px solid #eef1f4;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.stat-label {
  font-size: 12px;
  color: #909399;
}

.stat-value {
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
}

.address-layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 16px;
  min-height: 0;
}

.address-sidebar,
.address-main {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.address-card {
  background: #fff;
  border-radius: 12px;
  padding: 12px;
  border: 1px solid #eef1f4;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.group-toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
}

.group-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  max-height: 420px;
}

.group-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 10px;
  border: 1px solid transparent;
  background: #f7f9fc;
}

.group-row.active {
  background: #eaf2ff;
  border-color: #d6e6ff;
}

.group-chip {
  border: none;
  background: transparent;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0;
  color: #2b2f36;
  cursor: pointer;
  font-size: 13px;
  min-width: 0;
}

.group-row.active .group-chip {
  color: #1c4fb8;
  font-weight: 600;
}

.group-chip span:first-child {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-chip .count {
  background: #e9edf5;
  color: #5b6472;
  border-radius: 999px;
  padding: 2px 6px;
  font-size: 11px;
}

.address-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
}

.address-filter {
  font-size: 12px;
  color: #909399;
  padding: 4px 10px;
  border-radius: 999px;
  background: #f5f7fa;
  white-space: nowrap;
}

.contact-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.contact-header {
  display: grid;
  grid-template-columns: 1.1fr 1.2fr 0.8fr 1.3fr 120px;
  gap: 8px;
  padding: 6px 8px;
  font-size: 12px;
  color: #909399;
}

.contact-header-actions {
  text-align: right;
}

.contact-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  max-height: 360px;
}

.contact-row {
  display: grid;
  grid-template-columns: 1.1fr 1.2fr 0.8fr 1.3fr 120px;
  gap: 8px;
  align-items: center;
  padding: 8px;
  border-radius: 10px;
  background: #f7f9fc;
}

.contact-cell {
  min-width: 0;
}

.contact-wallet {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  min-width: 0;
  max-width: 100%;
}

.wallet-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.contact-name {
  font-weight: 400;
  color: inherit;
  font-size: 12px;
}

.contact-group {
  font-weight: 400;
  color: inherit;
  font-size: 12px;
}

.contact-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.contact-actions {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
}

.icon-button {
  padding: 0 4px;
}

.icon-button-inline {
  padding: 0;
}

.address-tag-empty {
  font-size: 12px;
  color: #909399;
}

.address-empty {
  font-size: 12px;
  color: #909399;
  padding: 8px;
}

@media (max-width: 1200px) {
  .address-layout {
    grid-template-columns: 220px minmax(0, 1fr);
  }
}

@media (max-width: 900px) {
  .address-layout {
    grid-template-columns: 1fr;
  }

  .address-stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .address-toolbar {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-left {
    flex-direction: column;
    align-items: stretch;
  }

  .contact-form {
    grid-template-columns: 1fr;
  }

  .contact-header {
    display: none;
  }

  .contact-row {
    grid-template-columns: 1fr;
    align-items: flex-start;
  }

  .contact-actions {
    justify-content: flex-start;
  }
}
</style>
