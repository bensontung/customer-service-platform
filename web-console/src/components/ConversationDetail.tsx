import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { TicketDialog } from './TicketDialog'
import { Badge, Dialog, EmptyState, ErrorNotice, formatTime, formData, Loading, sourceLabel, statusLabels, SubmitButton } from './UI'
import { normalizeDetail, type ConversationDetail as Detail, type Message } from '../types'

const pendingStatuses = new Set(['queued', 'retrying', 'pending', 'pending_send'])

export function ConversationDetail({ id, onChanged }: { id: string; onChanged: () => Promise<void> }) {
  const { user } = useAuth()
  const [detail, setDetail] = useState<Detail>()
  const [text, setText] = useState('')
  const [loading, setLoading] = useState(true)
  const [sending, setSending] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [mediaError, setMediaError] = useState<{ file: File; message: string }>()
  const [error, setError] = useState('')
  const [transferOpen, setTransferOpen] = useState(false)
  const [ticketOpen, setTicketOpen] = useState(false)
  const [clock, setClock] = useState(() => Date.now())
  const requestVersion = useRef(0)
  const messages = detail?.messages ?? []

  const load = useCallback(async () => {
    const version = ++requestVersion.current
    try {
      const result = normalizeDetail(await api.conversation(id))
      if (version !== requestVersion.current) return null
      const remoteIds = new Set(result.messages.map(message => message.id))
      setDetail(previous => ({
        ...result,
        messages: [...result.messages, ...(previous?.messages.filter(message => message.localId && !remoteIds.has(message.id)) ?? [])],
      }))
      return result
    } catch (reason) {
      if (version === requestVersion.current) setError(reason instanceof Error ? reason.message : '会话详情加载失败')
      return null
    } finally {
      if (version === requestVersion.current) setLoading(false)
    }
  }, [id])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 30000)
    return () => window.clearInterval(timer)
  }, [])
  const hasPending = messages.some(message => pendingStatuses.has(message.status))
  useEffect(() => {
    if (!hasPending) return
    let cancelled = false
    const poll = async () => {
      const result = await load()
      if (!result || cancelled) return
      setDetail(previous => {
        if (!previous) return previous
        const remoteIds = new Set(result.messages.map(message => message.id))
        const localPending = previous.messages.filter(message => message.localId && !remoteIds.has(message.id) && pendingStatuses.has(message.status))
        return { ...previous, messages: [...result.messages, ...localPending] }
      })
    }
    const timer = window.setInterval(() => void poll(), 2000)
    void poll()
    return () => { cancelled = true; window.clearInterval(timer) }
  }, [hasPending, load])

  if (loading) return <Loading />
  if (!detail) return <div className="detail-error"><ErrorNotice message={error || '无法加载会话'} /><button className="button" onClick={() => void load()}>重新加载</button></div>

  const { conversation, customer } = detail
  const deadline = new Date(conversation.replyDeadlineAt).getTime()
  const expired = !Number.isFinite(deadline) || deadline <= clock
  const unclaimed = !conversation.assigneeId
  const assignedToMe = conversation.assigneeId === user?.agentId
  const replyLimitReached = conversation.repliesInWindow >= 5
  const canReply = !unclaimed && assignedToMe && !expired && !replyLimitReached && conversation.status !== 'closed'
  const remainingReplies = Math.max(0, 5 - conversation.repliesInWindow)
  const replyDisabledReason = unclaimed ? '请先领取会话，再回复客户' : !assignedToMe ? '仅当前负责人可以回复此会话' : conversation.status === 'closed' ? '会话已关闭，无法继续回复' : expired ? '微信客服回复窗口已过期，无法主动发送消息' : replyLimitReached ? '当前回复窗口已达到平台回复条数上限' : ''

  async function claim() {
    setError('')
    try {
      const claimed = await api.claim(id)
      setDetail(previous => previous ? { ...previous, conversation: claimed } : previous)
      await onChanged()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '领取失败') }
  }

  async function send(body: { type: string; text?: string; mediaId?: string }, content: string, fromUpload = false) {
    if (!canReply || sending || (uploading && !fromUpload)) return
    setSending(true)
    setError('')
    const localId = `local-${Date.now()}`
    const local: Message = { id: localId, localId, direction: 'outbound', type: body.type, content, mediaId: body.mediaId, status: 'pending_send', errorMessage: '', createdAt: new Date().toISOString() }
    setDetail(previous => previous ? { ...previous, messages: [...previous.messages, local] } : previous)
    setText('')
    try {
      const result = await api.sendMessage(id, body)
      setDetail(previous => previous ? { ...previous, messages: previous.messages.map(message => message.id === localId ? { ...message, id: result.id, status: 'queued' } : message) } : previous)
      const latest = await load()
      if (latest?.messages.some(message => message.id === result.id)) {
        setDetail(previous => previous ? { ...previous, messages: previous.messages.filter(message => message.localId !== localId) } : previous)
      }
    } catch (reason) {
      setDetail(previous => previous ? { ...previous, messages: previous.messages.map(message => message.id === localId ? { ...message, status: 'failed_final', errorMessage: reason instanceof Error ? reason.message : '发送失败' } : message) } : previous)
      if (body.type === 'text') setText(body.text ?? content)
    } finally { setSending(false) }
  }

  async function retryMessage(message: Message) {
    if (!message.localId || sending || uploading || !canReply) return
    setDetail(previous => previous ? { ...previous, messages: previous.messages.filter(item => item.localId !== message.localId) } : previous)
    await send({ type: message.type, ...(message.type === 'text' ? { text: message.content } : { mediaId: message.mediaId }) }, message.content)
  }

  async function upload(file?: File) {
    if (!file || !canReply) return
    setUploading(true)
    setError('')
    setMediaError(undefined)
    try {
      const mediaId = await api.uploadMedia(file)
      const type = file.type.startsWith('image/') ? 'image' : file.type.startsWith('audio/') ? 'voice' : 'file'
      await send({ type, mediaId }, `[${file.name}]`, true)
    } catch (reason) {
      setMediaError({ file, message: reason instanceof Error ? reason.message : '上传失败' })
    } finally {
      setUploading(false)
    }
  }

  async function closeConversation() {
    if (!window.confirm('确认关闭此会话？关闭后不能继续回复。')) return
    try {
      const result = await api.close(id)
      setDetail(previous => previous ? { ...previous, conversation: result } : previous)
      await onChanged()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '关闭会话失败') }
  }

  return <>
    <div className="conversation-header">
      <div><div className="detail-title"><h2>客户 {customer.nickname ?? conversation.customerId.slice(0, 8)}</h2><Badge tone="blue">{sourceLabel(conversation.source)}</Badge><Badge tone={conversation.status === 'closed' ? 'neutral' : 'green'}>{statusLabels[conversation.status] ?? conversation.status}</Badge></div><p>会话编号：{conversation.id}</p></div>
      <div className="header-actions">{unclaimed ? <button className="button primary" onClick={() => void claim()}>领取会话</button> : <button className="button" onClick={() => setTransferOpen(true)}>转派</button>}{conversation.status !== 'closed' && <button className="button" onClick={() => void closeConversation()}>关闭会话</button>}</div>
    </div>
    <div className={`reply-window ${expired ? 'expired' : ''}`}><span className="window-icon">{expired ? '!' : '◷'}</span><div>{expired ? <strong>回复窗口已过期</strong> : <strong>微信回复窗口有效</strong>}<p>{expired ? '仅可在客户再次发起消息后回复，平台不允许主动触达。' : `可回复至 ${formatTime(conversation.replyDeadlineAt)} · 本窗口剩余 ${remainingReplies} 条（以微信平台返回为准）`}</p></div></div>
    <ErrorNotice message={error} />
    {mediaError && <div className="notice error" role="alert">{mediaError.file.name}：{mediaError.message}<button className="button small" onClick={() => void upload(mediaError.file)} disabled={uploading || !canReply}>重新上传</button></div>}
    <div className="messages" aria-live="polite">
      {messages.length === 0 ? <EmptyState title="还没有消息" description="客户的新消息会显示在这里" /> : messages.map(message => <div key={message.localId ?? message.id} className={`message-row ${message.direction === 'outbound' ? 'outbound' : 'inbound'}`}>
        {message.direction !== 'outbound' && <span className="message-avatar">客</span>}
        <div className="message-content"><div className={`message-bubble ${message.type !== 'text' ? 'media-message' : ''}`}>{message.content || message.text || `[${message.type}消息]`}</div><div className="message-meta">{formatTime(message.createdAt)}{message.direction === 'outbound' && <span className={`message-status status-${message.status}`}>{statusLabels[message.status] ?? message.status}</span>}</div>{message.errorMessage && <div className="message-error">{message.errorMessage}{message.localId && <button className="button text" onClick={() => void retryMessage(message)} disabled={sending || uploading || !canReply}>重试</button>}</div>}</div>
      </div>)}
    </div>
    <form className="reply-box" onSubmit={event => { event.preventDefault(); void send({ type: 'text', text }, text) }}>
      <div className="reply-tools"><span>回复客户</span><label className={`button small ${!canReply || sending || uploading ? 'disabled' : ''}`}>上传附件<input type="file" accept="image/jpeg,image/png,audio/amr,audio/mpeg" disabled={!canReply || sending || uploading} onChange={event => { void upload(event.target.files?.[0]); event.target.value = '' }} hidden />{uploading ? '上传中…' : '附件'}</label></div>
      <textarea aria-label="回复内容" value={text} onChange={event => setText(event.target.value)} rows={3} maxLength={2000} disabled={!canReply || sending || uploading} placeholder={replyDisabledReason || '请输入回复内容，发送后将由微信通道异步送达…'} />
      <div className="reply-bottom"><span className="muted">{canReply ? `${text.length}/2000 · 提交后不代表微信已送达` : replyDisabledReason}</span><button className="button primary" disabled={!canReply || !text.trim() || sending || uploading}>{sending ? '提交中…' : '发送回复'}</button></div>
    </form>
    <section className="customer-sidebar">
      <div className="customer-profile"><span className="large-avatar">{customer.nickname?.slice(0, 1) ?? '客'}</span><h3>{customer.nickname ?? '微信客户'}</h3><p>ID: {customer.id}</p><div className="tags">{customer.tags.length ? customer.tags.map(tag => <Badge key={tag}>{tag}</Badge>) : <span className="muted">暂无客户标签</span>}</div></div>
      <div className="customer-info"><h3>客户资料</h3><dl><dt>来源渠道</dt><dd>{sourceLabel(conversation.source)}</dd><dt>最近联系</dt><dd>{formatTime(conversation.lastInboundAt)}</dd><dt>负责人</dt><dd>{conversation.assigneeId || '待领取'}</dd></dl>{customer.note && <div className="customer-note"><strong>客户备注</strong><p>{customer.note}</p></div>}</div>
      <div className="related-tickets"><div className="section-title"><h3>关联工单 <span>{detail.tickets.length}</span></h3><button className="button text" onClick={() => setTicketOpen(true)}>+ 新建</button></div>{detail.tickets.length ? detail.tickets.map(ticket => <div className="related-ticket" key={ticket.id}><strong>{ticket.title}</strong><p>{statusLabels[ticket.status ?? ''] ?? ticket.status ?? '待处理'} · {ticket.id.slice(0, 8)}</p></div>) : <p className="muted">还没有关联工单</p>}</div>
    </section>
    {transferOpen && <TransferDialog id={id} onClose={() => setTransferOpen(false)} onTransferred={conversationView => { setDetail(previous => previous ? { ...previous, conversation: conversationView } : previous); setTransferOpen(false); void onChanged() }} />}
    {ticketOpen && <TicketDialog title={`会话 ${conversation.id}`} customerId={customer.id} conversationId={conversation.id} onClose={() => setTicketOpen(false)} onCreated={() => { setTicketOpen(false); void load(); void onChanged() }} />}
  </>
}

function TransferDialog({ id, onClose, onTransferred }: { id: string; onClose: () => void; onTransferred: (conversation: import('../types').Conversation) => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  return <Dialog title="转派会话" onClose={onClose}><form className="form-stack" onSubmit={event => {
    const data = formData(event)
    setBusy(true)
    void api.transfer(id, String(data.get('assigneeId') ?? ''), String(data.get('teamId') ?? '')).then(onTransferred).catch(reason => setError(reason instanceof Error ? reason.message : '转派失败')).finally(() => setBusy(false))
  }}><p className="muted">转派后由新负责人继续处理，操作将记录到审计日志。</p><label>负责人 ID<input name="assigneeId" placeholder="填写客服 ID" /></label><label>团队 ID<input name="teamId" placeholder="或填写团队 ID" /></label><ErrorNotice message={error} /><div className="dialog-actions"><button type="button" className="button" onClick={onClose}>取消</button><SubmitButton busy={busy}>确认转派</SubmitButton></div></form></Dialog>
}
