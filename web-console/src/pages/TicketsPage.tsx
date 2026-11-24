import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { TicketDialog } from '../components/TicketDialog'
import { Badge, EmptyState, ErrorNotice, formatTime, formData, PageHeader, SubmitButton } from '../components/UI'
import { normalizeCustomer, type Ticket } from '../types'

export function TicketsPage({ initialTickets = [] }: { initialTickets?: Ticket[] }) {
  const [tickets, setTickets] = useState<Ticket[]>(initialTickets)
  const [selectedId, setSelectedId] = useState<string>()
  const [createOpen, setCreateOpen] = useState(false)
  const [customerId, setCustomerId] = useState('')
  const [loading, setLoading] = useState(false)
  const [customerHint, setCustomerHint] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const selected = tickets.find(ticket => ticket.id === selectedId)
  useEffect(() => { setSelectedId(current => tickets.some(ticket => ticket.id === current) ? current : tickets[0]?.id) }, [tickets])
  useEffect(() => { void api.tickets().then(result => setTickets(result.items ?? [])).catch(reason => setError(reason instanceof Error ? reason.message : '工单加载失败')) }, [])
  useEffect(() => {
    if (!selectedId) return
    void api.ticketEvents(selectedId).then(result => setTickets(current => current.map(ticket => ticket.id === selectedId ? { ...ticket, events: result.items.map(event => ({ id: event.id, type: event.type, detail: event.payload, createdAt: event.createdAt })) } : ticket))).catch(() => undefined)
  }, [selectedId])

  async function loadCustomerTickets() {
    if (!customerId.trim()) return
    setLoading(true)
    setError('')
    try {
      const result = normalizeCustomer(await api.customer(customerId.trim()), customerId.trim())
      setTickets(result.tickets ?? [])
      setCustomerHint(`已加载客户 ${result.nickname ?? result.id} 的工单`)
    } catch (reason) { setError(reason instanceof Error ? reason.message : '工单加载失败') }
    finally { setLoading(false) }
  }

  function created(id: string) {
    setCreateOpen(false)
    setNotice(`工单 ${id} 已创建。可从工单列表继续跟进。`)
    void api.tickets().then(result => setTickets(result.items ?? []))
  }

  return <div className="standard-page">
    <PageHeader title="工单中心" description="跨渠道问题统一跟进，明确优先级、负责人和处理时限。" actions={<button className="button primary" onClick={() => setCreateOpen(true)}>+ 创建工单</button>} />
    <form className="toolbar" onSubmit={event => { event.preventDefault(); void loadCustomerTickets() }}><input aria-label="按客户 ID 查询工单" required placeholder="输入客户 ID 加载关联工单" value={customerId} onChange={event => setCustomerId(event.target.value)} /><button className="button" disabled={loading}>{loading ? '加载中…' : '加载工单'}</button>{customerHint && <span className="muted">{customerHint}</span>}</form>
    <ErrorNotice message={error} />{notice && <div className="notice success" role="status">{notice}</div>}
    <div className="split-content">
      <section className="card list-card"><div className="card-heading"><h2>工单列表</h2><Badge>{tickets.length}</Badge></div>{tickets.length ? tickets.map(ticket => <button key={ticket.id} className={`ticket-list-item ${selectedId === ticket.id ? 'selected' : ''}`} onClick={() => setSelectedId(ticket.id)}><div><strong>{ticket.title}</strong><Badge tone={ticket.priority === 'urgent' || ticket.priority === 'high' ? 'red' : 'orange'}>{priorityLabel(ticket.priority)}</Badge></div><p>{ticket.id}</p><span>{ticket.status ?? '待处理'} · 截止 {formatTime(ticket.dueAt)}</span></button>) : <EmptyState title="暂无已加载工单" description="新建工单，或从客户 CRM、会话详情和评论中选择已有工单" />}<p className="list-hint">通过客户 CRM 加载该客户关联的工单。</p></section>
      <section className="card" key={selected?.id ?? 'empty'}>{selected ? <><div className="card-heading"><div><span className="eyebrow">工单 {selected.id}</span><h2>{selected.title}</h2></div><Badge tone="blue">{selected.status ?? '待处理'}</Badge></div><p className="ticket-description">{selected.description || '暂无问题描述'}</p><dl className="details-list ticket-details"><div><dt>优先级</dt><dd>{priorityLabel(selected.priority)}</dd></div><div><dt>负责人</dt><dd>{selected.assigneeId || '待分配'}</dd></div><div><dt>截止时间</dt><dd>{formatTime(selected.dueAt)}</dd></div><div><dt>关联客户</dt><dd>{selected.customerId || '—'}</dd></div><div><dt>关联会话</dt><dd>{selected.conversationId || '—'}</dd></div><div><dt>关联评论</dt><dd>{selected.commentId || '—'}</dd></div></dl><div className="workflow"><h3>流转时间线</h3>{selected.events?.length ? selected.events.map((event, index) => <div className="timeline-item" key={event.id ?? index}><span className="timeline-dot" /><div><strong>{event.eventType ?? event.type ?? '工单更新'}</strong><p>{event.description ?? event.detail}</p><span>{formatTime(event.createdAt)}</span></div></div>) : <div className="timeline-item"><span className="timeline-dot" /><div><strong>工单已创建</strong><p>{formatTime(selected.createdAt)}</p></div></div>}</div><form className="ticket-update form-stack" onSubmit={event => { const data = formData(event); setBusy(true); setError(''); void api.updateTicket(selected.id, { status: String(data.get('status') ?? ''), priority: String(data.get('priority') ?? ''), assigneeId: String(data.get('assigneeId') ?? '') || undefined, dueAt: data.get('dueAt') ? new Date(String(data.get('dueAt'))).toISOString() : undefined }).then(() => { setTickets(current => current.map(ticket => ticket.id === selected.id ? { ...ticket, status: String(data.get('status')), priority: String(data.get('priority')), assigneeId: String(data.get('assigneeId')), dueAt: data.get('dueAt') ? new Date(String(data.get('dueAt'))).toISOString() : ticket.dueAt } : ticket)); setNotice('工单已更新，流转记录由后端写入。') }).catch(reason => setError(reason instanceof Error ? reason.message : '工单更新失败')).finally(() => setBusy(false)) }}><h3>更新工单</h3><div className="form-grid"><label>状态<select name="status" defaultValue={selected.status ?? 'open'}><option value="open">待处理</option><option value="processing">处理中</option><option value="resolved">已解决</option><option value="closed">已关闭</option></select></label><label>优先级<select name="priority" defaultValue={selected.priority ?? 'normal'}><option value="low">低</option><option value="normal">普通</option><option value="high">高</option><option value="urgent">紧急</option></select></label><label>负责人 ID<input name="assigneeId" defaultValue={selected.assigneeId ?? ''} /></label><label>处理截止时间<input name="dueAt" type="datetime-local" /></label></div><SubmitButton busy={busy}>保存工单变更</SubmitButton></form></> : <EmptyState title="选择一个工单" description="查看详情、流转时间线并更新处理进度" />}</section>
    </div>
    {createOpen && <TicketDialog title={customerId ? `客户 ${customerId}` : '独立工单'} customerId={customerId.trim() || undefined} onClose={() => setCreateOpen(false)} onCreated={created} />}
  </div>
}

function priorityLabel(priority?: string) {
  return ({ low: '低', normal: '普通', high: '高', urgent: '紧急' } as Record<string, string>)[priority ?? 'normal'] ?? priority ?? '普通'
}
