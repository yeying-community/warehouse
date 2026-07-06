import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { addressBookApi, type AddressGroup, type GroupMember } from '@/api'
import { showSuccess } from '@/utils/toast'

type AddressGroupFilter = 'all' | string
type MemberStatus = 'active' | 'pending' | string

function normalizeMemberStatus(status?: MemberStatus) {
  return String(status || 'active').trim().toLowerCase()
}

function isPendingMember(member: GroupMember) {
  return normalizeMemberStatus(member.status) === 'pending'
}

function matchesMemberKeyword(member: GroupMember, keyword: string) {
  if (!keyword) return true
  if (member.name.toLowerCase().includes(keyword)) return true
  if (member.walletAddress.toLowerCase().includes(keyword)) return true
  if ((member.tags || []).some(tag => tag.toLowerCase().includes(keyword))) return true
  return false
}

async function confirmAction(message: string, title = '提示') {
  try {
    await ElMessageBox.confirm(message, title, {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning',
      closeOnClickModal: false
    })
    return true
  } catch {
    return false
  }
}

function showError(message: string, title = '错误') {
  void ElMessageBox.alert(message, title, {
    confirmButtonText: '确定',
    type: 'error',
    closeOnClickModal: false
  })
}

export const useAddressBookStore = defineStore('addressBook', {
  state: () => ({
    addressBookLoading: false,
    addressGroups: [] as AddressGroup[],
    groupMembers: [] as GroupMember[],
    groupForm: { name: '' },
    groupSaving: false,
    memberForm: {
      id: '',
      name: '',
      walletAddress: '',
      groupId: '',
      tags: [] as string[]
    },
    memberSaving: false,
    memberDialogVisible: false,
    addressSearch: '',
    addressGroupFilter: 'all' as AddressGroupFilter
  }),
  getters: {
    addressGroupCounts(state) {
      const groups: Record<string, number> = {}
      const pendingGroups: Record<string, number> = {}
      let activeTotal = 0
      let pendingTotal = 0
      for (const member of state.groupMembers) {
        if (isPendingMember(member)) {
          pendingTotal += 1
          pendingGroups[member.groupId] = (pendingGroups[member.groupId] || 0) + 1
        } else {
          activeTotal += 1
          groups[member.groupId] = (groups[member.groupId] || 0) + 1
        }
      }
      return {
        total: activeTotal,
        active: activeTotal,
        pending: pendingTotal,
        all: state.groupMembers.length,
        groups,
        pendingGroups
      }
    },
    activeGroupMembers(state) {
      return state.groupMembers.filter(member => !isPendingMember(member))
    },
    pendingGroupMembers(state) {
      let items = state.groupMembers.filter(member => isPendingMember(member))
      const filter = state.addressGroupFilter
      if (filter !== 'all') {
        items = items.filter(item => item.groupId === filter)
      }
      const keyword = state.addressSearch.trim().toLowerCase()
      return items.filter(item => matchesMemberKeyword(item, keyword))
    },
    addressGroupLabel(state) {
      if (state.addressGroupFilter === 'all') return '全部'
      return state.addressGroups.find(group => group.id === state.addressGroupFilter)?.name || '全部'
    },
    filteredGroupMembers(state) {
      let items = state.groupMembers.filter(member => !isPendingMember(member))
      const filter = state.addressGroupFilter
      if (filter !== 'all') {
        items = items.filter(item => item.groupId === filter)
      }
      const keyword = state.addressSearch.trim().toLowerCase()
      return items.filter(item => matchesMemberKeyword(item, keyword))
    }
  },
  actions: {
    async fetchAddressBook() {
      if (this.addressBookLoading) return
      this.addressBookLoading = true
      try {
        const [groups, members] = await Promise.all([
          addressBookApi.listGroups(),
          addressBookApi.listMembers()
        ])
        this.addressGroups = groups.items || []
        this.groupMembers = members.items || []
        if (this.addressGroupFilter !== 'all') {
          const groupIds = new Set(this.addressGroups.map(group => group.id))
          if (!groupIds.has(this.addressGroupFilter)) {
            this.selectAddressGroup('all')
          }
        }
      } catch (error) {
        console.error('获取分组成员失败:', error)
      } finally {
        this.addressBookLoading = false
      }
    },
    selectAddressGroup(groupId: AddressGroupFilter) {
      this.addressGroupFilter = groupId
      if (!this.memberForm.id) {
        this.memberForm.groupId = groupId !== 'all' ? groupId : ''
      }
    },
    async createGroup() {
      const name = this.groupForm.name.trim()
      if (!name) {
        showError('请输入分组名称')
        return false
      }
      this.groupSaving = true
      try {
        await addressBookApi.createGroup(name)
        this.groupForm = { name: '' }
        await this.fetchAddressBook()
        showSuccess('分组已创建')
        return true
      } catch (error: any) {
        showError(error?.message || '创建分组失败')
        return false
      } finally {
        this.groupSaving = false
      }
    },
    async renameGroup(group: AddressGroup) {
      try {
        const { value } = await ElMessageBox.prompt('请输入新的分组名称', '重命名分组', {
          confirmButtonText: '保存',
          cancelButtonText: '取消',
          inputValue: group.name
        })
        const name = String(value || '').trim()
        if (!name || name === group.name) return
        await addressBookApi.updateGroup(group.id, name)
        await this.fetchAddressBook()
      } catch {
        // ignore
      }
    },
    async removeGroup(group: AddressGroup) {
      if (!(await confirmAction(`确定删除分组 ${group.name} 吗？`, '删除分组'))) return
      try {
        await addressBookApi.deleteGroup(group.id)
        await this.fetchAddressBook()
      } catch (error: any) {
        showError(error?.message || '删除分组失败')
      }
    },
    resetMemberForm() {
      const filter = this.addressGroupFilter
      this.memberForm = {
        id: '',
        name: '',
        walletAddress: '',
        groupId: filter !== 'all' ? filter : '',
        tags: []
      }
    },
    openCreateMemberDialog() {
      if (!this.addressGroups.length) {
        showError('请先创建分组')
        return
      }
      this.resetMemberForm()
      if (!this.memberForm.groupId) {
        this.memberForm.groupId = this.addressGroups[0]?.id || ''
      }
      this.memberDialogVisible = true
    },
    async submitMember() {
      const walletAddress = this.memberForm.walletAddress.trim()
      const name = this.memberForm.name.trim() || walletAddress
      const groupId = this.memberForm.groupId.trim()
      const tags = Array.isArray(this.memberForm.tags) ? this.memberForm.tags : []
      if (!walletAddress || !groupId) {
        showError('请输入钱包地址并选择分组')
        return
      }
      this.memberSaving = true
      try {
        let savedMember: GroupMember | null = null
        const isEditing = Boolean(this.memberForm.id)
        if (this.memberForm.id) {
          await addressBookApi.updateMember({
            id: this.memberForm.id,
            name,
            walletAddress,
            groupId,
            tags
          })
        } else {
          savedMember = await addressBookApi.createMember({
            name,
            walletAddress,
            groupId,
            tags
          })
        }
        this.resetMemberForm()
        this.memberDialogVisible = false
        await this.fetchAddressBook()
        if (savedMember && isPendingMember(savedMember)) {
          showSuccess('成员申请已提交，等待分组维护者审批')
        } else {
          showSuccess(isEditing ? '成员已保存' : '成员已添加')
        }
      } catch (error: any) {
        showError(error?.message || '保存成员失败')
      } finally {
        this.memberSaving = false
      }
    },
    editMember(member: GroupMember) {
      this.memberForm = {
        id: member.id,
        name: member.name,
        walletAddress: member.walletAddress,
        groupId: member.groupId,
        tags: member.tags ? [...member.tags] : []
      }
      this.memberDialogVisible = true
    },
    async removeMember(member: GroupMember) {
      if (!(await confirmAction(`确定删除成员 ${member.name} 吗？`, '删除成员'))) return
      try {
        await addressBookApi.deleteMember(member.id)
        if (this.memberForm.id === member.id) {
          this.resetMemberForm()
        }
        await this.fetchAddressBook()
      } catch (error: any) {
        showError(error?.message || '删除成员失败')
      }
    },
    async approveMember(member: GroupMember) {
      try {
        await addressBookApi.approveMember(member.id)
        await this.fetchAddressBook()
        showSuccess('成员已通过')
      } catch (error: any) {
        showError(error?.message || '审批成员失败')
      }
    },
    async rejectMember(member: GroupMember) {
      if (!(await confirmAction(`确定拒绝 ${member.name} 的加入申请吗？`, '拒绝申请'))) return
      try {
        await addressBookApi.rejectMember(member.id)
        if (this.memberForm.id === member.id) {
          this.resetMemberForm()
        }
        await this.fetchAddressBook()
        showSuccess('申请已拒绝')
      } catch (error: any) {
        showError(error?.message || '拒绝申请失败')
      }
    }
  }
})

export { isPendingMember }
