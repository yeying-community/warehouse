import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { addressBookApi, type AddressGroup, type GroupMember } from '@/api'
import { showSuccess } from '@/utils/toast'

type AddressGroupFilter = 'all' | string

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
      for (const member of state.groupMembers) {
        groups[member.groupId] = (groups[member.groupId] || 0) + 1
      }
      return {
        total: state.groupMembers.length,
        groups
      }
    },
    addressGroupLabel(state) {
      if (state.addressGroupFilter === 'all') return '全部'
      return state.addressGroups.find(group => group.id === state.addressGroupFilter)?.name || '全部'
    },
    filteredGroupMembers(state) {
      let items = state.groupMembers
      const filter = state.addressGroupFilter
      if (filter !== 'all') {
        items = items.filter(item => item.groupId === filter)
      }
      const keyword = state.addressSearch.trim().toLowerCase()
      if (!keyword) return items
      return items.filter(item => {
        if (item.name.toLowerCase().includes(keyword)) return true
        if (item.walletAddress.toLowerCase().includes(keyword)) return true
        if ((item.tags || []).some(tag => tag.toLowerCase().includes(keyword))) return true
        return false
      })
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
        if (this.memberForm.id) {
          await addressBookApi.updateMember({
            id: this.memberForm.id,
            name,
            walletAddress,
            groupId,
            tags
          })
        } else {
          await addressBookApi.createMember({
            name,
            walletAddress,
            groupId,
            tags
          })
        }
        this.resetMemberForm()
        this.memberDialogVisible = false
        await this.fetchAddressBook()
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
    }
  }
})
