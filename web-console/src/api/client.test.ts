import { describe, expect, it, vi } from 'vitest'
import { request, setAccessToken, setUnauthorizedHandler } from './client'
import { jsonResponse } from '../test/testUtils'

describe('fetch wrapper', () => {
  it('将内存 JWT 放入 Bearer 请求头', async () => {
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(() => jsonResponse({ items: [] }))
    vi.stubGlobal('fetch', fetchMock)
    setAccessToken('memory-only-token')
    await request('/conversations')
    const headers = new Headers(fetchMock.mock.calls[0][1]?.headers)
    expect(headers.get('Authorization')).toBe('Bearer memory-only-token')
    expect(window.localStorage.getItem('token')).toBeNull()
  })

  it('后端 401 清除内存凭据并触发登录状态', async () => {
    const onUnauthorized = vi.fn()
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(() => jsonResponse({ message: '登录已过期' }, 401))
    vi.stubGlobal('fetch', fetchMock)
    setAccessToken('expired-token')
    setUnauthorizedHandler(onUnauthorized)
    await expect(request('/me')).rejects.toMatchObject({ status: 401, message: '登录已过期' })
    expect(onUnauthorized).toHaveBeenCalledOnce()
    await request('/me').catch(() => undefined)
    const headers = new Headers(fetchMock.mock.calls[1][1]?.headers)
    expect(headers.has('Authorization')).toBe(false)
  })
})
