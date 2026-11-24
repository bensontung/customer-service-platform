import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { authenticated, jsonResponse, user } from '../test/testUtils'
import { CommentsPage } from './CommentsPage'

describe('评论管理', () => {
  it('仅显示文章评论回复，不显示私信回复', async () => {
    vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/me')) return jsonResponse(user)
      return jsonResponse({ items: [{ id: 'comment-1', content: '这篇文章很有帮助', status: 'pending_review', articleTitle: '服务指南', authorName: '微信用户' }] })
    }))
    render(authenticated(<CommentsPage />))
    expect(await screen.findByRole('button', { name: '回复评论' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '私信回复' })).not.toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '私信回复' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '回复评论' }))
    expect(screen.getByRole('textbox', { name: '评论公开回复' })).toBeInTheDocument()
    expect(screen.getByPlaceholderText('回复将显示在文章评论区，不是私信')).toBeInTheDocument()
  })
})
