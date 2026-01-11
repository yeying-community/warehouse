export interface FileItem {
  name: string
  path: string
  isDir: boolean
  size: number
  modified: string
}

// 规范化路径用于比较（确保统一格式）
function normalizePathForCompare(path: string): string {
  if (path === '/') return '/'
  // 确保以 / 开头和结尾
  let normalized = path.startsWith('/') ? path : '/' + path
  return normalized.endsWith('/') ? normalized : normalized + '/'
}

// 检查两个路径是否表示同一目录
function isSamePath(path1: string, path2: string): boolean {
  return normalizePathForCompare(path1) === normalizePathForCompare(path2)
}

// 解析 WebDAV PROPFIND 响应
export function parsePropfindResponse(xml: string, currentPath: string): FileItem[] {
  const items: FileItem[] = []

  console.log('PROPFIND: raw xml length:', xml.length)
  console.log('PROPFIND: currentPath:', currentPath)

  // 检查 XML 中是否包含中文文件名
  const chineseCharMatch = xml.match(/[\u4e00-\u9fa5]/)
  console.log('PROPFIND: contains Chinese:', !!chineseCharMatch)

  // 按 <d:response> 分割，每个 response 块包含一个资源的信息
  const responseRegex = /<[Dd]:response[^>]*>([\s\S]*?)<\/[Dd]:response>/gi
  const responses = [...xml.matchAll(responseRegex)]

  console.log('PROPFIND: responses count:', responses.length)

  // 规范化当前路径用于比较
  const normalizedCurrentPath = normalizePathForCompare(currentPath)

  for (const match of responses) {
    const responseXml = match[1]

    // 提取 href - 使用更宽松的模式匹配所有字符
    const hrefMatch = /<[Dd]:href>([^<]*)<\/[Dd]:href>/i.exec(responseXml)
    if (!hrefMatch) continue

    // href 可能是 URL 编码的
    let href = hrefMatch[1]
    try {
      href = decodeURIComponent(href)
    } catch (e) {
      // 如果解码失败，保持原样
    }

    console.log('PROPFIND: href:', href)

    // 排除根目录自身和当前目录自身
    if (isSamePath(href, normalizedCurrentPath)) {
      continue
    }

    // 提取 displayname
    const nameMatch = /<[Dd]:displayname>([^<]*)<\/[Dd]:displayname>/i.exec(responseXml)
    let name = nameMatch?.[1] || ''

    // 如果 displayname 为空，从 href 提取名称
    if (!name) {
      const parts = href.split('/').filter(Boolean)
      name = parts[parts.length - 1] || ''
    }

    // 解码 displayname
    if (name) {
      try {
        name = decodeURIComponent(name)
      } catch (e) {
        // 保持原样
      }
    }

    if (name === '') continue

    // 提取文件大小（目录可能没有这个属性）
    const sizeMatch = /<[Dd]:getcontentlength>([^<]+)<\/[Dd]:getcontentlength>/i.exec(responseXml)
    const size = parseInt(sizeMatch?.[1] || '0')

    // 提取修改时间
    const lastModMatch = /<[Dd]:getlastmodified>([^<]+)<\/[Dd]:getlastmodified>/i.exec(responseXml)
    const lastMod = lastModMatch?.[1] || ''

    console.log('PROPFIND: item:', { name, path: href, isDir: href.endsWith('/') })

    items.push({
      name,
      path: href,
      isDir: href.endsWith('/'),
      size,
      modified: lastMod
    })
  }

  console.log('PROPFIND: parsed items:', items.length)
  return items
}