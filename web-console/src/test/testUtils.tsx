import { useEffect, type ReactNode } from 'react'
import { AuthProvider, useAuth } from '../auth/AuthContext'
import type { Conversation, ConversationDetail, User } from '../types'

export const user: User = { agentId: 'agent-1', role: 'agent', name: '测试客服', onShift: true }

function Authenticate({ children }: { children: ReactNode }) {
  const auth = useAuth()
  useEffect(() => {
    auth.signIn({ ...user, token: 'test-token' })
  }, [])
  return auth.user ? children : null
}

export function authenticated(children: ReactNode) {
  return <AuthProvider><Authenticate>{children}</Authenticate></AuthProvider>
}

export function conversationDetail(conversation: Partial<Conversation>): ConversationDetail {
  return {
    conversation: {
      id: 'conversation-1',
      source: 'official_account',
      customerId: 'customer-1',
      status: 'open',
      assigneeId: 'agent-1',
      lastInboundAt: new Date().toISOString(),
      replyDeadlineAt: new Date(Date.now() + 3600000).toISOString(),
      repliesInWindow: 0,
      ...conversation,
    },
    customer: { id: 'customer-1', tags: [], note: '' },
    messages: [],
    tickets: [],
  }
}

export function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
}
