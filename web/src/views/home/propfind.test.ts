import { describe, it, expect } from 'vitest'
import { parsePropfindResponse } from '@/utils/webdav'

// 测试用例1：根目录响应（用户实际数据）
const rootDirResponse = `<?xml version="1.0" encoding="UTF-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getlastmodified>Sat, 10 Jan 2026 09:55:52 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/test/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>test</d:displayname>
        <d:getlastmodified>Sat, 10 Jan 2026 09:55:52 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/V2rayU-arm64.dmg</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getcontentlength>39316150</d:getcontentlength>
        <d:getlastmodified>Sat, 10 Jan 2026 06:22:46 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/test1/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>test1</d:displayname>
        <d:getlastmodified>Sat, 10 Jan 2026 06:08:38 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/qrcode_f95C97.png</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getcontentlength>2939</d:getcontentlength>
        <d:getlastmodified>Sat, 10 Jan 2026 08:26:12 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
</d:multistatus>`

// 测试用例2：子目录响应（包含带括号的文件名）
const subDirResponse = `<?xml version="1.0" encoding="UTF-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/test/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getlastmodified>Sat, 10 Jan 2026 09:55:52 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/test/PINGPONG-latest-universal.dmg</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getcontentlength>50234567</d:getcontentlength>
        <d:getlastmodified>Sat, 10 Jan 2026 09:51:21 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/test/qrcode_EA77C7%20(2).png</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname></d:displayname>
        <d:getcontentlength>4075</d:getcontentlength>
        <d:getlastmodified>Sat, 10 Jan 2026 09:51:21 GMT</d:getlastmodified>
      </d:prop>
    </d:propstat>
  </d:response>
</d:multistatus>`

// 测试用例3：用户实际API响应（真实数据，所有标签大写，文件有displayname）
const actualApiResponse = `<?xml version="1.0" encoding="UTF-8"?><D:multistatus xmlns:D="DAV:"><D:response><D:href>/</D:href><D:propstat><D:prop><D:displayname></D:displayname><D:getlastmodified>Sat, 10 Jan 2026 08:26:12 GMT</D:getlastmodified><D:resourcetype><D:collection xmlns:D="DAV:"/></D:resourcetype><D:supportedlock><D:lockentry xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/test/</D:href><D:propstat><D:prop><D:getlastmodified>Sat, 10 Jan 2026 09:55:52 GMT</D:getlastmodified><D:resourcetype><D:collection xmlns:D="DAV:"/></D:resourcetype><D:supportedlock><D:lockentry xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock><D:displayname>test</D:displayname></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/V2rayU-arm64.dmg</D:href><D:propstat><D:prop><D:displayname>V2rayU-arm64.dmg</D:displayname><D:getlastmodified>Sat, 10 Jan 2026 06:22:46 GMT</D:getlastmodified><D:getcontenttype>application/x-apple-diskimage</D:getcontenttype><D:resourcetype></D:resourcetype><D:getcontentlength>39316150</D:getcontentlength><D:getetag>"18894a6e65b0e20e257eab6"</D:getetag><D:supportedlock><D:lockentry xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/test1/</D:href><D:propstat><D:prop><D:supportedlock><D:lockentry xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock><D:displayname>test1</D:displayname><D:getlastmodified>Sat, 10 Jan 2026 06:08:38 GMT</D:getlastmodified><D:resourcetype><D:collection xmlns:D="DAV:"/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/qrcode_f95C97.png</D:href><D:propstat><D:prop><D:displayname>qrcode_f95C97.png</D:displayname><D:getlastmodified>Sat, 10 Jan 2026 08:26:12 GMT</D:getlastmodified><D:getcontenttype>image/png</D:getcontenttype><D:resourcetype></D:resourcetype><D:getcontentlength>2939</D:getcontentlength><D:getetag>"1889512aa06cefa9b7b"</D:getetag><D:supportedlock><D:lockentry xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockentry></D:supportedlock></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`

// 测试用例4：子目录的真实 API 响应（假设的格式）
const subDirActualResponse = `<?xml version="1.0" encoding="UTF-8"?><D:multistatus xmlns:D="DAV:"><D:response><D:href>/test/</D:href><D:propstat><D:prop><D:displayname></D:displayname><D:getlastmodified>Sat, 10 Jan 2026 09:55:52 GMT</D:getlastmodified><D:resourcetype><D:collection xmlns:D="DAV:"/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/test/PINGPONG-latest-universal.dmg</D:href><D:propstat><D:prop><D:displayname></D:displayname><D:getlastmodified>Sat, 10 Jan 2026 09:51:21 GMT</D:getlastmodified><D:getcontenttype>application/x-apple-diskimage</D:getcontenttype><D:resourcetype></D:resourcetype><D:getcontentlength>50234567</D:getcontentlength></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response><D:response><D:href>/test/qrcode_EA77C7%20(2).png</D:href><D:propstat><D:prop><D:displayname></D:displayname><D:getlastmodified>Sat, 10 Jan 2026 09:51:21 GMT</D:getlastmodified><D:getcontenttype>image/png</D:getcontenttype><D:resourcetype></D:resourcetype><D:getcontentlength>4075</D:getcontentlength></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`

describe('parsePropfindResponse', () => {
  it('should parse root directory with subdirs and files', () => {
    const items = parsePropfindResponse(rootDirResponse, '/')

    expect(items).toHaveLength(4)
    // 检查文件是否都存在
    const names = items.map(i => i.name)
    expect(names).toContain('test')
    expect(names).toContain('V2rayU-arm64.dmg')
    expect(names).toContain('test1')
    expect(names).toContain('qrcode_f95C97.png')
  })

  it('should correctly identify files vs directories', () => {
    const items = parsePropfindResponse(rootDirResponse, '/')

    const test = items.find(i => i.name === 'test')
    const v2ray = items.find(i => i.name === 'V2rayU-arm64.dmg')
    const test1 = items.find(i => i.name === 'test1')
    const qrcode = items.find(i => i.name === 'qrcode_f95C97.png')

    expect(test?.isDir).toBe(true)
    expect(v2ray?.isDir).toBe(false)
    expect(test1?.isDir).toBe(true)
    expect(qrcode?.isDir).toBe(false)
  })

  it('should parse subdirectory with files (including special chars)', () => {
    const items = parsePropfindResponse(subDirResponse, '/test/')

    expect(items).toHaveLength(2)
    const names = items.map(i => i.name)
    expect(names).toContain('PINGPONG-latest-universal.dmg')
    expect(names).toContain('qrcode_EA77C7 (2).png') // URL 解码后应该有括号
  })

  it('should correctly parse file sizes', () => {
    const items = parsePropfindResponse(rootDirResponse, '/')

    const v2ray = items.find(i => i.name === 'V2rayU-arm64.dmg')
    const qrcode = items.find(i => i.name === 'qrcode_f95C97.png')

    expect(v2ray?.size).toBe(39316150)
    expect(qrcode?.size).toBe(2939)
  })

  it('should exclude current directory itself (with trailing slash)', () => {
    const items = parsePropfindResponse(subDirResponse, '/test/')
    expect(items).toHaveLength(2)
    expect(items.map(i => i.name)).toEqual(['PINGPONG-latest-universal.dmg', 'qrcode_EA77C7 (2).png'])
  })

  it('should exclude current directory itself (without trailing slash)', () => {
    const items = parsePropfindResponse(subDirResponse, '/test')
    expect(items).toHaveLength(2)
    expect(items.map(i => i.name)).toEqual(['PINGPONG-latest-universal.dmg', 'qrcode_EA77C7 (2).png'])
  })

  it('should handle uppercase tags (real API response)', () => {
    const items = parsePropfindResponse(actualApiResponse, '/')

    expect(items).toHaveLength(4)
    const names = items.map(i => i.name)
    expect(names).toContain('test')
    expect(names).toContain('V2rayU-arm64.dmg')
    expect(names).toContain('test1')
    expect(names).toContain('qrcode_f95C97.png')
  })

  it('should correctly identify files vs directories (real API response)', () => {
    const items = parsePropfindResponse(actualApiResponse, '/')

    const test = items.find(i => i.name === 'test')
    const v2ray = items.find(i => i.name === 'V2rayU-arm64.dmg')
    const test1 = items.find(i => i.name === 'test1')
    const qrcode = items.find(i => i.name === 'qrcode_f95C97.png')

    expect(test?.isDir).toBe(true)
    expect(v2ray?.isDir).toBe(false)
    expect(test1?.isDir).toBe(true)
    expect(qrcode?.isDir).toBe(false)
  })

  it('should correctly parse file sizes (real API response)', () => {
    const items = parsePropfindResponse(actualApiResponse, '/')

    const v2ray = items.find(i => i.name === 'V2rayU-arm64.dmg')
    const qrcode = items.find(i => i.name === 'qrcode_f95C97.png')

    expect(v2ray?.size).toBe(39316150)
    expect(qrcode?.size).toBe(2939)
  })

  it('should correctly identify directories by trailing slash', () => {
    const items = parsePropfindResponse(rootDirResponse, '/')

    const dir = items.find(i => i.name === 'test')
    const file = items.find(i => i.name === 'V2rayU-arm64.dmg')

    expect(dir?.isDir).toBe(true)
    expect(file?.isDir).toBe(false)
  })

  it('should parse subdirectory with actual API format (uppercase tags)', () => {
    const items = parsePropfindResponse(subDirActualResponse, '/test/')

    expect(items).toHaveLength(2)
    const names = items.map(i => i.name)
    expect(names).toContain('PINGPONG-latest-universal.dmg')
    expect(names).toContain('qrcode_EA77C7 (2).png')
  })

  it('should not show current directory when entering subdirectory', () => {
    // 模拟进入 /test/ 目录，parse 时传入 /test/
    // 应该只返回子项，不包含 /test/ 自身
    const items = parsePropfindResponse(subDirResponse, '/test/')

    // 不应该包含当前目录自身
    const currentDir = items.find(i => i.path === '/test/')
    expect(currentDir).toBeUndefined()

    // 应该包含两个子项
    expect(items).toHaveLength(2)
  })

  it('should parse root directory after returning from subdirectory', () => {
    // 模拟返回上级后解析根目录
    const items = parsePropfindResponse(rootDirResponse, '/')

    // 应该包含 test 目录
    const test = items.find(i => i.name === 'test')
    expect(test).toBeDefined()
    expect(test?.path).toBe('/test/')
    expect(test?.isDir).toBe(true)
  })
})