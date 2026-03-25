import { defineStore } from 'pinia'
import { ElMessageBox } from 'element-plus'
import { addressBookApi, type AddressContact, type AddressGroup } from '@/api'
import { showSuccess } from '@/utils/toast'

type AddressGroupFilter = 'all' | 'ungrouped' | string

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
    addressContacts: [] as AddressContact[],
    groupForm: { name: '' },
    groupSaving: false,
    contactForm: {
      id: '',
      name: '',
      walletAddress: '',
      groupId: '',
      tags: [] as string[]
    },
    contactSaving: false,
    contactDialogVisible: false,
    addressSearch: '',
    addressGroupFilter: 'all' as AddressGroupFilter
  }),
  getters: {
    addressGroupCounts(state) {
      const groups: Record<string, number> = {}
      let ungrouped = 0
      for (const contact of state.addressContacts) {
        if (contact.groupId) {
          groups[contact.groupId] = (groups[contact.groupId] || 0) + 1
        } else {
          ungrouped += 1
        }
      }
      return {
        total: state.addressContacts.length,
        ungrouped,
        groups
      }
    },
    addressGroupLabel(state) {
      if (state.addressGroupFilter === 'all') return '全部'
      if (state.addressGroupFilter === 'ungrouped') return '未分组'
      return state.addressGroups.find(group => group.id === state.addressGroupFilter)?.name || '全部'
    },
    filteredAddressContacts(state) {
      let items = state.addressContacts
      const filter = state.addressGroupFilter
      if (filter === 'ungrouped') {
        items = items.filter(item => !item.groupId)
      } else if (filter !== 'all') {
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
        const [groups, contacts] = await Promise.all([
          addressBookApi.listGroups(),
          addressBookApi.listContacts()
        ])
        this.addressGroups = groups.items || []
        this.addressContacts = contacts.items || []
        if (this.addressGroupFilter !== 'all' && this.addressGroupFilter !== 'ungrouped') {
          const groupIds = new Set(this.addressGroups.map(group => group.id))
          if (!groupIds.has(this.addressGroupFilter)) {
            this.selectAddressGroup('all')
          }
        }
      } catch (error) {
        console.error('获取地址簿失败:', error)
      } finally {
        this.addressBookLoading = false
      }
    },
    selectAddressGroup(groupId: AddressGroupFilter) {
      this.addressGroupFilter = groupId
      if (!this.contactForm.id) {
        this.contactForm.groupId = groupId !== 'all' && groupId !== 'ungrouped' ? groupId : ''
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
        this.groupForm.name = ''
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
    resetContactForm() {
      const filter = this.addressGroupFilter
      this.contactForm = {
        id: '',
        name: '',
        walletAddress: '',
        groupId: filter !== 'all' && filter !== 'ungrouped' ? filter : '',
        tags: []
      }
    },
    openCreateContactDialog() {
      this.resetContactForm()
      this.contactDialogVisible = true
    },
    async submitContact() {
      const name = this.contactForm.name.trim()
      const walletAddress = this.contactForm.walletAddress.trim()
      const tags = Array.isArray(this.contactForm.tags) ? this.contactForm.tags : []
      if (!name || !walletAddress) {
        showError('请输入联系人名称和钱包地址')
        return
      }
      this.contactSaving = true
      try {
        if (this.contactForm.id) {
          await addressBookApi.updateContact({
            id: this.contactForm.id,
            name,
            walletAddress,
            groupId: this.contactForm.groupId || '',
            tags
          })
        } else {
          await addressBookApi.createContact({
            name,
            walletAddress,
            groupId: this.contactForm.groupId || '',
            tags
          })
        }
        this.resetContactForm()
        this.contactDialogVisible = false
        await this.fetchAddressBook()
      } catch (error: any) {
        showError(error?.message || '保存联系人失败')
      } finally {
        this.contactSaving = false
      }
    },
    editContact(contact: AddressContact) {
      this.contactForm = {
        id: contact.id,
        name: contact.name,
        walletAddress: contact.walletAddress,
        groupId: contact.groupId || '',
        tags: contact.tags ? [...contact.tags] : []
      }
      this.contactDialogVisible = true
    },
    async removeContact(contact: AddressContact) {
      if (!(await confirmAction(`确定删除 ${contact.name} 吗？`, '删除联系人'))) return
      try {
        await addressBookApi.deleteContact(contact.id)
        if (this.contactForm.id === contact.id) {
          this.resetContactForm()
        }
        await this.fetchAddressBook()
      } catch (error: any) {
        showError(error?.message || '删除联系人失败')
      }
    }
  }
})
