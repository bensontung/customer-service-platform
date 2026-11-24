import type { JsonObject, Session, UploadCredentials, User } from '../types'

let accessToken: string | null = null
let onUnauthorized: (() => void) | undefined

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

export function setAccessToken(token: string | null) {
  accessToken = token || null
}

export function setUnauthorizedHandler(handler: () => void) {
  onUnauthorized = handler
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  if (options.body) headers.set('Content-Type', 'application/json')
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  const response = await fetch(`/api/v1${path}`, { ...options, headers })
  const text = await response.text()
  let payload: unknown
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = text
    }
  }
  if (!response.ok) {
    if (response.status === 401) {
      setAccessToken(null)
      onUnauthorized?.()
    }
    const data = payload && typeof payload === 'object' ? payload as JsonObject : {}
    const message = data.message ?? data.error ?? data.msg
    throw new ApiError(response.status, typeof message === 'string' ? message : `请求失败 (${response.status})`)
  }
  return payload as T
}

export const api = {
  setAccessToken,
  setUnauthorizedHandler,
  request,
  login: (code: string, corpId: string) => request<Session>('/auth/dingtalk/login', {
    method: 'POST',
    body: JSON.stringify({ code, corpId }),
  }),
  me: () => request<User>('/me'),
  conversations: (params: Record<string, string>) => {
    const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value !== ''))
    return request<{ items: import('../types').Conversation[] }>(`/conversations?${query}`)
  },
  conversation: (id: string) => request<import('../types').ConversationDetail>(`/conversations/${encodeURIComponent(id)}`),
  claim: (id: string) => request<import('../types').Conversation>(`/conversations/${encodeURIComponent(id)}/claim`, { method: 'POST' }),
  transfer: (id: string, assigneeId: string, teamId: string) => request<import('../types').Conversation>(`/conversations/${encodeURIComponent(id)}/transfer`, {
    method: 'POST',
    body: JSON.stringify({ ...(assigneeId ? { assigneeId } : {}), ...(teamId ? { teamId } : {}) }),
  }),
  sendMessage: (id: string, body: { type: string; text?: string; mediaId?: string }) => request<{ id: string }>(`/conversations/${encodeURIComponent(id)}/messages`, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: { 'Idempotency-Key': crypto.randomUUID() },
  }),
  close: (id: string) => request<import('../types').Conversation>(`/conversations/${encodeURIComponent(id)}/close`, { method: 'POST' }),
  comments: (status: string, accountId: string) => request<unknown>(`/comments?${new URLSearchParams({ ...(status ? { status } : {}), ...(accountId ? { accountId } : {}) })}`),
  commentAction: (id: string, action: string, content?: string) => request<{ id: string }>(`/comments/${encodeURIComponent(id)}/actions`, {
    method: 'POST',
    body: JSON.stringify({ action, ...(content ? { content } : {}) }),
  }),
  syncComments: (body: { accountKey: string; msgDataId: string; articleIndex: number; begin: number }) => request<{ id: string }>('/admin/comments/sync', { method: 'POST', body: JSON.stringify(body) }),
  customer: (id: string) => request<unknown>(`/customers/${encodeURIComponent(id)}`),
  updateCustomer: (id: string, tags: string[], note: string) => request<unknown>(`/customers/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify({ tags, note }),
  }),
  createTicket: (body: import('../types').TicketDraft) => request<{ id: string }>('/tickets', { method: 'POST', body: JSON.stringify(body) }),
  tickets: () => request<{ items: import('../types').Ticket[] }>('/tickets'),
  ticketEvents: (id: string) => request<{ items: Array<{ id: string; type: string; payload: string; createdAt: string }> }>(`/tickets/${encodeURIComponent(id)}/events`),
  updateTicket: (id: string, body: { status?: string; priority?: string; assigneeId?: string; dueAt?: string }) => request<{ id: string }>(`/tickets/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(body) }),
  shifts: () => request<unknown>('/admin/shifts'),
  saveShift: (body: { agentId?: string; teamId?: string; startAt: string; endAt: string; state?: string }) => request<{ id: string }>('/admin/shifts', { method: 'POST', body: JSON.stringify(body) }),
  reports: () => request<unknown>('/admin/reports'),
  auditLogs: () => request<unknown>('/admin/audit-logs'),
  uploadCredentials: (file: File) => request<UploadCredentials>('/media/upload-credentials', {
    method: 'POST',
    body: JSON.stringify({ filename: file.name, contentType: file.type || 'application/octet-stream', size: file.size }),
  }),
  uploadMedia: async (file: File) => {
    const credentials = await api.uploadCredentials(file)
    const response = await fetch(credentials.uploadUrl, { method: 'PUT', headers: credentials.headers, body: file })
    if (!response.ok) throw new ApiError(response.status, '文件上传失败，请重试')
    return credentials.objectKey
  },
}
