<script setup lang="ts">
import { ref } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { Delete, DocumentCopy, Edit, Refresh } from '@element-plus/icons-vue'
import { useAddressBookStore } from '@/stores/addressBookStore'
import { copyText } from '@/utils/clipboard'
import { shortenAddress } from '@/utils/address'
import type { GroupMember } from '@/api'

const props = withDefaults(defineProps<{
  embedded?: boolean
}>(), {
  embedded: false
})

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
  memberForm,
  memberSaving,
  memberDialogVisible,
  filteredGroupMembers
} = storeToRefs(addressBookStore)
const {
  selectAddressGroup,
  createGroup,
  renameGroup,
  removeGroup,
  openCreateMemberDialog,
  submitMember,
  resetMemberForm,
  editMember,
  removeMember
} = addressBookStore

const groupDialogVisible = ref(false)

function showError(message: string, title = '错误') {
  void ElMessageBox.alert(message, title, {
    confirmButtonText: '确定',
    type: 'error',
    closeOnClickModal: false
  })
}

function copyMemberAddress(member: GroupMember) {
  const address = member.walletAddress?.trim()
  if (!address) {
    showError('暂无钱包地址')
    return
  }
  copyText(address, '钱包地址已复制')
}

function memberGroupName(member: GroupMember) {
  return addressGroups.value.find(group => group.id === member.groupId)?.name || '-'
}

function openCreateGroupDialog() {
  groupForm.value.name = ''
  groupDialogVisible.value = true
}

function closeCreateGroupDialog() {
  groupDialogVisible.value = false
  groupForm.value.name = ''
}

async function submitCreateGroup() {
  const created = await createGroup()
  if (created) {
    groupDialogVisible.value = false
  }
}
</script>

<template>
  <div class="address-page" :class="{ embedded: props.embedded }">
    <div v-if="!props.embedded" class="address-hero">
      <div class="address-title-row">
        <div class="address-hero-main">
          <div class="address-title">分组管理</div>
          <div class="address-sub">维护共享分组与成员，用于快速、安全地选择共享对象。</div>
        </div>
        <div class="address-hero-actions">
          <el-button type="primary" @click="openCreateMemberDialog">添加成员</el-button>
          <el-tooltip content="刷新" placement="top">
            <el-button
              class="refresh-button"
              circle
              :icon="Refresh"
              :disabled="addressBookLoading"
              :class="{ 'is-refreshing': addressBookLoading }"
              @click="emit('refresh')"
            />
          </el-tooltip>
        </div>
      </div>
      <div class="address-stats">
        <div class="stat-card">
          <span class="stat-label">成员</span>
          <span class="stat-value">{{ addressGroupCounts.total }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">分组</span>
          <span class="stat-value">{{ addressGroups.length }}</span>
        </div>
      </div>
    </div>

    <div class="address-layout">
      <div class="address-sidebar">
        <div class="address-card">
          <div class="section-header">
            <div>
              <div class="card-title">分组筛选</div>
              <div class="card-subtitle">按分组筛选成员，方便批量共享。</div>
            </div>
            <el-button size="small" @click="openCreateGroupDialog">新建分组</el-button>
          </div>
          <div class="group-list">
            <div class="group-row" :class="{ active: addressGroupFilter === 'all' }">
              <button type="button" class="group-chip" @click="selectAddressGroup('all')">
                <span>全部</span>
                <span class="count">{{ addressGroupCounts.total }}</span>
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
            <el-input v-model="addressSearch" clearable placeholder="搜索成员 / 钱包 / 标签" size="small" />
            <span class="address-filter">当前分组：{{ addressGroupLabel }}</span>
          </div>
          <div class="toolbar-right">
            <el-button v-if="props.embedded" type="primary" size="small" @click="openCreateMemberDialog">添加成员</el-button>
            <span class="address-total">共 {{ filteredGroupMembers.length }} 位成员</span>
          </div>
        </div>

        <div class="address-card address-list-card">
          <div v-if="!props.embedded" class="list-title-row">
            <div>
              <div class="card-title">成员列表</div>
              <div class="card-subtitle">按名称、钱包地址或标签快速定位成员。</div>
            </div>
          </div>
          <div class="member-list">
            <div v-if="!filteredGroupMembers.length" class="address-empty">暂无成员</div>
            <div v-for="member in filteredGroupMembers" :key="member.id" class="member-row">
              <div class="member-main">
                <div class="member-top">
                  <div class="member-name">{{ member.name }}</div>
                  <el-tag size="small" effect="plain">{{ memberGroupName(member) }}</el-tag>
                </div>
                <div class="member-wallet">
                  <span class="member-label">钱包地址</span>
                  <span class="mono wallet-text" :title="member.walletAddress">
                    {{ shortenAddress(member.walletAddress) }}
                  </span>
                  <el-tooltip content="复制钱包地址" placement="top">
                    <el-button
                      class="icon-button icon-button-inline"
                      link
                      :icon="DocumentCopy"
                      :disabled="!member.walletAddress"
                      @click="copyMemberAddress(member)"
                    />
                  </el-tooltip>
                </div>
                <div class="member-tags">
                  <span class="member-label">标签</span>
                  <el-tag v-for="tag in member.tags || []" :key="tag" size="small" type="info">
                    {{ tag }}
                  </el-tag>
                  <span v-if="!member.tags || !member.tags.length" class="address-tag-empty">无标签</span>
                </div>
              </div>
              <div class="member-actions">
                <el-tooltip content="编辑" placement="top">
                  <el-button class="icon-button" link :icon="Edit" @click="editMember(member)" />
                </el-tooltip>
                <el-tooltip content="删除" placement="top">
                  <el-button class="icon-button" type="danger" link :icon="Delete" @click="removeMember(member)" />
                </el-tooltip>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <el-dialog
      v-model="groupDialogVisible"
      title="新建分组"
      width="420px"
      @closed="closeCreateGroupDialog"
    >
      <div class="group-dialog-body">
        <el-input
          v-model="groupForm.name"
          placeholder="分组名称"
          size="small"
          @keyup.enter="submitCreateGroup"
        />
      </div>
      <template #footer>
        <el-button @click="closeCreateGroupDialog">取消</el-button>
        <el-button type="primary" :loading="groupSaving" @click="submitCreateGroup">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="memberDialogVisible"
      :title="memberForm.id ? '编辑成员' : '添加成员'"
      width="520px"
      @closed="resetMemberForm"
    >
      <div class="member-form">
        <el-input v-model="memberForm.name" placeholder="成员名称 / 备注" size="small" />
        <el-input v-model="memberForm.walletAddress" placeholder="钱包地址" size="small" />
        <el-select v-model="memberForm.groupId" placeholder="选择分组" size="small">
          <el-option
            v-for="group in addressGroups"
            :key="group.id"
            :label="group.name"
            :value="group.id"
          />
        </el-select>
        <el-select
          v-model="memberForm.tags"
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
        <el-button @click="memberDialogVisible = false">取消</el-button>
        <el-button @click="resetMemberForm">清空</el-button>
        <el-button type="primary" :loading="memberSaving" @click="submitMember">
          {{ memberForm.id ? '保存' : '添加' }}
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

.address-page.embedded {
  padding: 0;
}

.card-title {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
}

.card-subtitle {
  margin-top: 4px;
  font-size: 12px;
  color: #909399;
}

.actions {
  display: flex;
  gap: 8px;
}

.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
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

.address-hero-main {
  min-width: 0;
}

.address-hero-actions {
  display: flex;
  align-items: center;
  gap: 10px;
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
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

.address-page.embedded .address-sidebar,
.address-page.embedded .address-main {
  gap: 10px;
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

.address-page.embedded .address-card {
  background: #f7f9fc;
}

.group-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  max-height: 520px;
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

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

.address-filter {
  font-size: 12px;
  color: #909399;
  padding: 4px 10px;
  border-radius: 999px;
  background: #f5f7fa;
  white-space: nowrap;
}

.address-total {
  font-size: 12px;
  color: #606266;
}

.member-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.group-dialog-body {
  padding-top: 4px;
}

.list-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.member-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  max-height: 480px;
}

.member-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 12px;
  border-radius: 12px;
  background: #f7f9fc;
  border: 1px solid #eef1f4;
}

.member-main {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.member-top {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.member-wallet {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
  flex-wrap: wrap;
}

.member-label {
  font-size: 12px;
  color: #909399;
  flex-shrink: 0;
}

.wallet-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.member-name {
  font-weight: 600;
  color: #1f2d3d;
  font-size: 14px;
  min-width: 0;
}

.member-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.member-actions {
  display: flex;
  gap: 6px;
  justify-content: flex-start;
  flex-shrink: 0;
  padding-top: 2px;
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

  .toolbar-right {
    justify-content: flex-start;
  }

  .section-header {
    flex-direction: column;
    align-items: stretch;
  }

  .member-form {
    grid-template-columns: 1fr;
  }

  .address-title-row {
    flex-direction: column;
    align-items: stretch;
  }

  .address-hero-actions {
    justify-content: space-between;
  }

  .member-row {
    flex-direction: column;
  }

  .member-actions {
    justify-content: flex-start;
  }
}
</style>
