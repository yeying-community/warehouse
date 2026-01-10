import { ElMessage } from 'element-plus'

export const messages = (message: string, type: 'success' | 'warning' | 'info' | 'error' = 'info') =>
  ElMessage({
    showClose: true,
    message,
    type
  })

export const isMobile = (): boolean => {
  const isMobileDevice = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent)
  const isSmallScreen = window.innerWidth <= 768
  return isMobileDevice || isSmallScreen
}