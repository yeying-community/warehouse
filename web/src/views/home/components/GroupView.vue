<script setup lang="ts">
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { ArrowLeft, Check, Close, Delete, DocumentCopy, Edit, Refresh } from '@element-plus/icons-vue'
import { useGroupStore } from '@/stores/groupStore'
import { copyText } from '@/utils/clipboard'
import { shortenAddress } from '@/utils/address'
import type { ManagedGroup, GroupMember } from '@/api'

const props = withDefaults(defineProps<{
  embedded?: boolean
}>(), {
  embedded: false
})

const groupStore = useGroupStore()
const emit = defineEmits<{
  (event: 'refresh'): void
}>()
const {
  managedGroups,
  groupMemberCounts,
  selectedGroupId,
  groupSearch,
  groupLoading,
  groupForm,
  groupSaving,
  memberForm,
  memberSaving,
  memberDialogVisible,
  activeGroupMembers,
  pendingGroupMembers,
  filteredGroupMembers
} = storeToRefs(groupStore)
const {
  selectGroup,
  createGroup,
  renameGroup,
  removeGroup,
  openCreateMemberDialog,
  submitMember,
  resetMemberForm,
  editMember,
  removeMember,
  approveMember,
  rejectMember
} = groupStore

const groupDialogVisible = ref(false)

const selectedGroup = computed(() =>
  managedGroups.value.find(group => group.id === selectedGroupId.value) || null
)
const inGroupDetail = computed(() => Boolean(selectedGroup.value))
const groupListKeyword = computed(() => groupSearch.value.trim().toLowerCase())
const visibleGroups = computed(() => {
  const keyword = groupListKeyword.value
  if (!keyword) return managedGroups.value
  return managedGroups.value.filter(group => group.name.toLowerCase().includes(keyword))
})
const detailMembers = computed(() => [
  ...pendingGroupMembers.value,
  ...filteredGroupMembers.value
])
const canManageSelectedGroup = computed(() => selectedGroup.value?.canManage === true)
const memberDialogTitle = computed(() => memberForm.value.id ? '编辑成员' : '添加成员')
const memberDialogGroupName = computed(() => {
  const groupID = memberForm.value.groupId || selectedGroup.value?.id || ''
  return managedGroups.value.find(group => group.id === groupID)?.name || selectedGroup.value?.name || '-'
})

const memberAddressOptions = computed(() => {
  const byWallet = new Map<string, {
    walletAddress: string
    names: Set<string>
    groups: Set<string>
  }>()
  for (const member of activeGroupMembers.value) {
    const walletAddress = member.walletAddress?.trim()
    if (!walletAddress) continue
    const key = walletAddress.toLowerCase()
    let option = byWallet.get(key)
    if (!option) {
      option = {
        walletAddress,
        names: new Set<string>(),
        groups: new Set<string>()
      }
      byWallet.set(key, option)
    }
    if (member.name?.trim() && member.name.trim().toLowerCase() !== key) {
      option.names.add(member.name.trim())
    }
    const groupName = memberGroupName(member)
    if (groupName && groupName !== '-') {
      option.groups.add(groupName)
    }
  }
  return Array.from(byWallet.values()).map(option => {
    const names = Array.from(option.names)
    const groups = Array.from(option.groups)
    const title = names[0] || shortenAddress(option.walletAddress)
    const subtitleParts = [
      option.walletAddress,
      groups.length ? groups.join(' / ') : ''
    ].filter(Boolean)
    return {
      value: option.walletAddress,
      label: [names.join(' '), option.walletAddress, groups.join(' ')].filter(Boolean).join(' '),
      title,
      subtitle: subtitleParts.join(' · ')
    }
  })
})

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
  return managedGroups.value.find(group => group.id === member.groupId)?.name || '-'
}

function handleWalletAddressChange(value: string) {
  const walletAddress = String(value || '').trim()
  if (!walletAddress || memberForm.value.name.trim()) return
  const matched = activeGroupMembers.value.find(member => member.walletAddress?.toLowerCase() === walletAddress.toLowerCase())
  if (matched && matched.name && matched.name.toLowerCase() !== walletAddress.toLowerCase()) {
    memberForm.value.name = matched.name
  }
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

function openGroupDetail(group: ManagedGroup) {
  selectGroup(group.id)
}

function backToGroupList() {
  selectGroup('all')
}

function openAddMemberForCurrentGroup() {
  if (!selectedGroup.value) return
  openCreateMemberDialog()
  memberForm.value.groupId = selectedGroup.value.id
}

function groupActiveCount(group: ManagedGroup) {
  return groupMemberCounts.value.groups[group.id] || 0
}

function groupPendingCount(group: ManagedGroup) {
  return groupMemberCounts.value.pendingGroups[group.id] || 0
}

function memberStatusLabel(member: GroupMember) {
  return member.status === 'pending' ? '待审批' : '已通过'
}

function memberStatusType(member: GroupMember) {
  return member.status === 'pending' ? 'warning' : 'success'
}

function canManageMember(member: GroupMember) {
  return Boolean(member.canManage || canManageSelectedGroup.value)
}
</script>

<template>
  <div class="group-management-page" :class="{ embedded: props.embedded }">
    <div class="group-management-hero">
      <div class="group-management-title-row" :class="{ 'is-detail': inGroupDetail }">
        <div class="group-management-hero-main">
          <div class="group-management-title">分组管理</div>
          <div v-if="!inGroupDetail" class="group-management-sub">维护共享分组、成员准入和审批，用于安全地控制协作范围。</div>
        </div>
        <div v-if="inGroupDetail" class="detail-group-title">{{ selectedGroup?.name }}</div>
        <div v-if="inGroupDetail" class="group-management-hero-actions">
          <el-button :icon="ArrowLeft" @click="backToGroupList">返回分组列表</el-button>
        </div>
        <div v-else class="group-management-hero-actions">
          <el-button type="primary" @click="openCreateGroupDialog">新建分组</el-button>
          <el-tooltip content="刷新" placement="top">
            <el-button
              class="refresh-button"
              circle
              :icon="Refresh"
              :disabled="groupLoading"
              :class="{ 'is-refreshing': groupLoading }"
              @click="emit('refresh')"
            />
          </el-tooltip>
        </div>
      </div>
    </div>

    <div v-if="!inGroupDetail" class="group-management-list-page">
      <div class="group-management-toolbar">
        <div class="toolbar-left">
          <el-input v-model="groupSearch" clearable placeholder="搜索分组" />
        </div>
      </div>

      <div v-if="!visibleGroups.length" class="group-management-empty">暂无分组</div>
      <div v-else class="group-table">
        <button
          v-for="group in visibleGroups"
          :key="group.id"
          type="button"
          class="group-list-row"
          @click="openGroupDetail(group)"
        >
          <div class="group-list-main">
            <div class="group-list-name">{{ group.name }}</div>
            <div class="group-list-meta">点击查看成员与审批</div>
          </div>
          <div class="group-list-stats">
            <el-tag size="small" effect="plain">成员 {{ groupActiveCount(group) }}</el-tag>
            <el-tag v-if="groupPendingCount(group)" size="small" type="warning" effect="plain">
              待审批 {{ groupPendingCount(group) }}
            </el-tag>
          </div>
          <div v-if="group.canManage" class="group-list-actions" @click.stop>
            <el-button size="small" text @click="renameGroup(group)">重命名</el-button>
            <el-button size="small" text type="danger" @click="removeGroup(group)">删除</el-button>
          </div>
          <div v-else class="group-list-actions" @click.stop>
            <span class="group-management-tag-empty">无操作</span>
          </div>
        </button>
      </div>
    </div>

    <div v-else class="group-management-detail-page">
      <div class="group-management-toolbar">
        <div class="toolbar-left">
          <el-input v-model="groupSearch" clearable placeholder="搜索成员 / 钱包 / 标签" />
        </div>
        <div class="toolbar-right">
          <el-button type="primary" @click="openAddMemberForCurrentGroup">添加成员</el-button>
          <el-tooltip content="刷新" placement="top">
            <el-button
              class="refresh-button"
              circle
              :icon="Refresh"
              :disabled="groupLoading"
              :class="{ 'is-refreshing': groupLoading }"
              @click="emit('refresh')"
            />
          </el-tooltip>
        </div>
      </div>

      <div v-if="!detailMembers.length" class="group-management-empty">暂无成员</div>
      <div v-else class="member-table">
        <div class="member-table-head">
          <span>成员</span>
          <span>钱包地址</span>
          <span>标签</span>
          <span>状态</span>
          <span>操作</span>
        </div>
        <div
          v-for="member in detailMembers"
          :key="member.id"
          class="member-table-row"
        >
          <div class="member-name">{{ member.name }}</div>
          <div class="member-address-cell">
            <span class="mono wallet-text" :title="member.walletAddress">{{ shortenAddress(member.walletAddress) }}</span>
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
            <el-tag v-for="tag in member.tags || []" :key="tag" size="small" type="info">
              {{ tag }}
            </el-tag>
            <span v-if="!member.tags || !member.tags.length" class="group-management-tag-empty">无标签</span>
          </div>
          <div>
            <el-tag size="small" :type="memberStatusType(member)" effect="plain">
              {{ memberStatusLabel(member) }}
            </el-tag>
          </div>
          <div class="member-actions">
            <template v-if="canManageMember(member)">
              <el-button
                v-if="member.status === 'pending'"
                size="small"
                type="primary"
                :icon="Check"
                @click="approveMember(member)"
              >
                通过
              </el-button>
              <el-button
                v-if="member.status === 'pending'"
                size="small"
                :icon="Close"
                @click="rejectMember(member)"
              >
                拒绝
              </el-button>
              <el-tooltip v-if="member.status !== 'pending'" content="编辑" placement="top">
                <el-button class="icon-button" link :icon="Edit" @click="editMember(member)" />
              </el-tooltip>
              <el-tooltip content="移除" placement="top">
                <el-button class="icon-button" type="danger" link :icon="Delete" @click="removeMember(member)" />
              </el-tooltip>
            </template>
            <span v-else class="group-management-tag-empty">无操作</span>
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
      :title="memberDialogTitle"
      width="520px"
      class="member-dialog"
      @closed="resetMemberForm"
    >
      <div class="member-dialog-body">
        <div class="member-dialog-context">
          <span class="member-dialog-context-label">当前分组</span>
          <span class="member-dialog-context-value">{{ memberDialogGroupName }}</span>
        </div>
        <el-form label-position="top" class="member-dialog-form">
          <el-form-item label="钱包地址">
            <el-select
              v-model="memberForm.walletAddress"
              filterable
              allow-create
              default-first-option
              clearable
              placeholder="搜索名称 / 地址，或粘贴钱包地址"
              @change="handleWalletAddressChange"
            >
              <el-option
                v-for="option in memberAddressOptions"
                :key="option.value"
                :label="option.label"
                :value="option.value"
              >
                <div class="member-address-option" :title="option.subtitle">
                  <span class="member-address-title">{{ option.title }}</span>
                  <span class="member-address-subtitle mono">{{ option.subtitle }}</span>
                </div>
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item label="分组内显示名">
            <el-input
              v-model="memberForm.name"
              placeholder="可选"
              @keyup.enter="submitMember"
            />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button @click="memberDialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          :loading="memberSaving"
          @click="submitMember"
        >
          {{ memberForm.id ? '保存' : '添加' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style lang="scss" scoped>
.group-management-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 8px;
  min-height: 0;
}

.group-management-page.embedded {
  padding: 0;
  width: 100%;
  flex: 1;
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

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.group-management-hero,
.group-management-list-page,
.group-management-detail-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.group-management-title-row,
.detail-header,
.group-management-toolbar,
.list-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.group-management-title-row.is-detail {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
}

.group-management-title-row.is-detail .group-management-hero-actions {
  justify-content: flex-end;
}

.group-management-hero-main,
.detail-title-side {
  min-width: 0;
}

.group-management-hero-actions,
.detail-actions,
.toolbar-left,
.toolbar-right,
.detail-title-side {
  display: flex;
  align-items: center;
  gap: 10px;
}

.toolbar-left {
  flex: 1;
}

.group-management-title {
  font-size: 20px;
  font-weight: 600;
  color: #1f2d3d;
}

.detail-group-title {
  min-width: 0;
  max-width: 480px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 18px;
  font-weight: 600;
  color: #1f2d3d;
  text-align: center;
}

.group-management-sub {
  font-size: 13px;
  color: #909399;
}

.group-management-card {
  background: #fff;
  border-radius: 12px;
  padding: 12px;
  border: 1px solid #eef1f4;
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
}

.group-table {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 620px;
  overflow: auto;
}

.group-list-row {
  width: 100%;
  border: 1px solid #eef1f4;
  background: #f7f9fc;
  border-radius: 12px;
  padding: 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 14px;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.group-list-row:hover {
  border-color: #d6e6ff;
  background: #f3f8ff;
}

.group-list-main {
  min-width: 0;
}

.group-list-name {
  font-size: 14px;
  font-weight: 600;
  color: #1f2d3d;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-list-meta,
.group-management-total {
  font-size: 12px;
  color: #606266;
}

.group-list-stats,
.group-list-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.member-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.member-form :deep(.el-select) {
  width: 100%;
}

.group-dialog-body {
  padding-top: 4px;
}

.member-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.member-dialog-context {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid #eef1f4;
  border-radius: 10px;
  background: #f7f9fc;
}

.member-dialog-context-label {
  font-size: 12px;
  color: #909399;
}

.member-dialog-context-value {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 600;
  color: #1f2d3d;
}

.member-dialog-form {
  display: flex;
  flex-direction: column;
}

.member-dialog-form :deep(.el-select) {
  width: 100%;
}

.member-table {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  max-height: 620px;
}

.member-table-head,
.member-table-row {
  display: grid;
  grid-template-columns: minmax(120px, 1.1fr) minmax(180px, 1.4fr) minmax(140px, 1fr) 90px minmax(150px, auto);
  align-items: center;
  gap: 12px;
}

.member-table-head {
  padding: 0 12px 4px;
  font-size: 12px;
  color: #909399;
}

.member-table-row {
  padding: 12px;
  border: 1px solid #eef1f4;
  border-radius: 12px;
  background: #f7f9fc;
}

.member-address-cell,
.member-tags,
.member-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex-wrap: wrap;
}

.member-actions {
  justify-content: flex-start;
  flex-shrink: 0;
  padding-top: 2px;
}

.approval-actions {
  align-items: center;
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

.member-address-option {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.member-address-title {
  font-size: 13px;
  color: #1f2d3d;
  font-weight: 500;
}

.member-address-subtitle {
  color: #909399;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.back-button {
  padding-left: 0;
}

.icon-button {
  padding: 0 4px;
}

.icon-button-inline {
  padding: 0;
}

.group-management-tag-empty,
.group-management-empty {
  font-size: 12px;
  color: #909399;
}

.group-management-empty {
  padding: 8px;
}

@media (max-width: 900px) {
  .group-list-row,
  .member-table-head,
  .member-table-row {
    grid-template-columns: 1fr;
  }

  .group-management-title-row,
  .detail-header,
  .group-management-toolbar,
  .list-title-row,
  .detail-title-side {
    flex-direction: column;
    align-items: stretch;
  }

  .toolbar-right,
  .detail-actions,
  .group-management-hero-actions {
    justify-content: flex-start;
  }

  .group-management-title-row.is-detail {
    display: flex;
  }

  .detail-group-title {
    max-width: 100%;
    text-align: left;
  }

  .member-form {
    grid-template-columns: 1fr;
  }

  .member-table-head {
    display: none;
  }
}
</style>
