import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConversationDetail } from '../components/ConversationDetail'
import { authenticated, conversationDetail, jsonResponse, user } from '../test/testUtils'

function renderConversation(assigneeId: string, replyDeadlineAt: string) {
  const detail = conversationDetail({ assigneeId, replyDeadlineAt })
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.endsWith('/me')) return jsonResponse(user)
    if (url.includes('/conversations/conversation-1')) return jsonResponse(detail)
    return jsonResponse({})
  })
  vi.stubGlobal('fetch', fetchMock)
  render(authenticated(<ConversationDetail id="conversation-1" onChanged={vi.fn()} />))
  return fetchMock
}

describe('会话回复限制', () => {
  it('回复窗口过期时禁用回复并展示平台限制', async () => {
    renderConversation('agent-1', new Date(Date.now() - 60000).toISOString())
    expect(await screen.findByLabelText('回复内容')).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeDisabled()
    expect(screen.getAllByText(/微信客服回复窗口已过期/).length).toBeGreaterThan(0)
  })

  it('发送后先展示 pending，再轮询微信最终状态且不移除其他本地消息', async () => {
    let remoteStatus = 'queued'
    let resolveSend: ((value: Response) => void) | undefined
    const initial = conversationDetail({})
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/me')) return jsonResponse(user)
      if (url.endsWith('/messages')) {
        if (!resolveSend) {
          return new Promise<Response>(resolve => { resolveSend = resolve })
        }
        return jsonResponse({ id: 'message-2' })
      }
      if (url.includes('/conversations/conversation-1')) return jsonResponse({
        ...initial,
        messages: [
          { id: 'message-1', direction: 'outbound', type: 'text', content: '第一条', status: remoteStatus, errorMessage: '', createdAt: new Date().toISOString() },
          ...(remoteStatus === 'sent' ? [{ id: 'message-2', direction: 'outbound', type: 'text', content: '第二条', status: 'sent', errorMessage: '', createdAt: new Date().toISOString() }] : []),
        ],
      })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    render(authenticated(<ConversationDetail id="conversation-1" onChanged={vi.fn()} />))
    const input = await screen.findByLabelText('回复内容')
    await userEvent.type(input, '第一条')
    await userEvent.click(screen.getByRole('button', { name: '发送回复' }))
    expect(screen.getByText('本地待发送')).toBeInTheDocument()
    resolveSend?.(await jsonResponse({ id: 'message-1' }))
    await waitFor(() => expect(screen.getAllByText('排队发送中').length).toBeGreaterThan(0))
    await userEvent.type(input, '第二条')
    await userEvent.click(screen.getByRole('button', { name: '发送回复' }))
    await waitFor(() => expect(screen.getByText('第二条')).toBeInTheDocument())
    remoteStatus = 'sent'
    expect(await screen.findAllByText('已送达微信', {}, { timeout: 4000 })).toHaveLength(2)
    expect(screen.getByText('第一条')).toBeInTheDocument()
    expect(screen.getByText('第二条')).toBeInTheDocument()
  })

  it('未领取会话禁用回复，只能先领取', async () => {
    renderConversation('', new Date(Date.now() + 3600000).toISOString())
    expect(await screen.findByLabelText('回复内容')).toBeDisabled()
    expect(screen.getByRole('button', { name: '发送回复' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '领取会话' })).toBeEnabled()
    expect(screen.getByText('请先领取会话，再回复客户')).toBeInTheDocument()
  })
})
