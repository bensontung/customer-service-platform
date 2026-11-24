export type Role = 'agent' | 'supervisor' | 'admin'
export type JsonObject = Record<string, unknown>

export interface User {
  agentId: string
  role: Role
  name: string
  onShift?: boolean
}

export interface Session extends User {
  token: string
}

export interface Conversation {
  id: string
  channel?: string
  source: string
  customerId: string
  status: string
  assigneeId: string
  lastInboundAt: string
  replyDeadlineAt: string
  repliesInWindow: number
  unreadCount?: number
  lastMessage?: string
}

export interface Message {
  id: string
  direction: string
  type: string
  content: string
  status: string
  errorMessage: string
  createdAt: string
  localId?: string
  mediaId?: string
  text?: string
}

export interface Customer {
  id: string
  nickname?: string
  channel?: string
  openId?: string
  unionid?: string
  phone?: string
  tags: string[]
  note: string
  conversations?: Conversation[]
  tickets?: Ticket[]
}

export interface ConversationDetail {
  conversation: Conversation
  customer: Customer
  messages: Message[]
  tickets: Ticket[]
}

export interface Ticket {
  id: string
  customerId?: string
  conversationId?: string
  commentId?: string
  title: string
  description?: string
  priority?: string
  assigneeId?: string
  dueAt?: string
  status?: string
  createdAt?: string
  updatedAt?: string
  events?: TicketEvent[]
}

export interface TicketEvent {
  id?: string
  eventType?: string
  type?: string
  createdAt?: string
  description?: string
  detail?: string
}

export interface Comment {
  id: string
  msgDataId?: string
  articleIndex?: number
  commentId?: string
  replyContent?: string
  accountId?: string
  accountKey?: string
  articleTitle?: string
  title?: string
  authorName?: string
  nickname?: string
  content: string
  status: string
  createdAt?: string
  history?: Array<{ action: string; content?: string; createdAt?: string }>
  actions?: Array<{ action: string; content?: string; createdAt?: string }>
}

export interface Shift {
  id?: string
  agentId?: string
  agentName?: string
  teamId?: string
  startAt: string
  endAt: string
  state?: string
}

export interface AuditLog {
  id?: string
  actorId?: string
  actorName?: string
  action: string
  resourceType?: string
  resourceId?: string
  createdAt?: string
  detail?: string
}

export interface Reports {
  newConversations: number
  averageFirstResponseSeconds: number
  slaExpired: number
  closedConversations: number
}

export interface UploadCredentials {
  objectKey: string
  uploadUrl: string
  headers: Record<string, string>
  expiresAt: string
}

export interface TicketDraft {
  title: string
  description: string
  priority: string
  assigneeId?: string
  dueAt?: string
  customerId?: string
  conversationId?: string
  commentId?: string
}

export function listOf<T>(value: unknown): T[] {
  if (Array.isArray(value)) return value as T[]
  if (value && typeof value === 'object') {
    const data = value as JsonObject
    for (const key of ['items', 'data', 'comments', 'customers', 'tickets', 'shifts', 'logs']) {
      if (Array.isArray(data[key])) return data[key] as T[]
    }
  }
  return []
}

export function normalizeCustomer(value: unknown, fallbackId = ''): Customer {
  const data = value && typeof value === 'object' ? value as JsonObject : {}
  return {
    ...data,
    id: String(data.id ?? fallbackId),
    nickname: typeof data.nickname === 'string' ? data.nickname : undefined,
    tags: Array.isArray(data.tags) ? data.tags.filter((tag): tag is string => typeof tag === 'string') : [],
    note: typeof data.note === 'string' ? data.note : '',
    conversations: Array.isArray(data.conversations) ? data.conversations as Conversation[] : [],
    tickets: Array.isArray(data.tickets) ? data.tickets as Ticket[] : [],
  }
}

export function normalizeDetail(value: ConversationDetail): ConversationDetail {
  return {
    ...value,
    customer: normalizeCustomer(value.customer, value.conversation.customerId),
    messages: Array.isArray(value.messages) ? value.messages : [],
    tickets: Array.isArray(value.tickets) ? value.tickets : [],
  }
}
