import { render, waitFor } from '@testing-library/react'
import dd from 'dingtalk-jsapi'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../auth/AuthContext'
import { jsonResponse, user } from '../test/testUtils'
import { LoginPage } from './LoginPage'

vi.mock('dingtalk-jsapi', () => ({ default: { requestAuthCode: vi.fn() } }))

describe('钉钉登录', () => {
  beforeEach(() => {
    vi.mocked(dd.requestAuthCode).mockResolvedValue({ code: 'temporary-auth-code' })
    Object.defineProperty(window.navigator, 'userAgent', { value: 'DingTalk/7.0', configurable: true })
  })

  it('使用 corpId/clientId 获取授权码，仅向登录接口提交 code 与 corpId', async () => {
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>((input: RequestInfo | URL) => {
      if (String(input).endsWith('/me')) return jsonResponse(user)
      return jsonResponse({ token: 'jwt-token', ...user })
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<AuthProvider><LoginPage /></AuthProvider>)
    await waitFor(() => expect(dd.requestAuthCode).toHaveBeenCalledWith({ corpId: 'test-corp', clientId: 'test-client' }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/dingtalk/login', expect.anything()))
    const loginCall = fetchMock.mock.calls.find(([url]) => String(url).endsWith('/auth/dingtalk/login'))
    expect(loginCall).toBeDefined()
    const options = loginCall?.[1] as RequestInit | undefined
    expect(JSON.parse(String(options?.body))).toEqual({ code: 'temporary-auth-code', corpId: 'test-corp' })
    expect(options?.method).toBe('POST')
    expect(options?.body).not.toContain('clientId')
    expect(options?.body).not.toContain('secret')
  })
})
