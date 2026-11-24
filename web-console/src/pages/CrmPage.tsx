import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { TicketDialog } from '../components/TicketDialog'
import { Badge, EmptyState, ErrorNotice, formatTime, PageHeader, sourceLabel, statusLabels } from '../components/UI'
import { normalizeCustomer, type Customer } from '../types'

export function CrmPage() {
  const [query, setQuery] = useState('')
  const [customer, setCustomer] = useState<Customer>()
  const [tags, setTags] = useState('')
  const [note, setNote] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [ticketOpen, setTicketOpen] = useState(false)

  async function search(id: string) {
    if (!id.trim()) return
    setLoading(true)
    setError('')
    setSaved(false)
    try {
      const result = normalizeCustomer(await api.customer(id.trim()), id.trim())
      setCustomer(result)
    } catch (reason) { setCustomer(undefined); setError(reason instanceof Error ? reason.message : '客户查询失败') }
    finally { setLoading(false) }
  }
  useEffect(() => {
    setTags(customer?.tags.join(', ') ?? '')
    setNote(customer?.note ?? '')
  }, [customer])

  async function save() {
    if (!customer) return
    setSaving(true)
    setError('')
    setSaved(false)
    const nextTags = tags.split(/[,，]/).map(tag => tag.trim()).filter(Boolean)
    try {
      await api.updateCustomer(customer.id, nextTags, note)
      setCustomer(previous => previous ? { ...previous, tags: nextTags, note } : previous)
      setSaved(true)
    } catch (reason) { setError(reason instanceof Error ? reason.message : '客户资料保存失败') }
    finally { setSaving(false) }
  }

  return <div className="standard-page">
    <PageHeader title="客户 CRM" description="集中查看客户身份、沟通历史与服务工单，维护统一的客户档案。" />
    <form className="toolbar search-toolbar" onSubmit={event => { event.preventDefault(); void search(query) }}><input aria-label="客户 ID" required placeholder="输入客户 ID 查询完整档案" value={query} onChange={event => setQuery(event.target.value)} /><button className="button primary" disabled={loading}>{loading ? '查询中…' : '查询客户'}</button></form>
    <ErrorNotice message={error} />{saved && <div className="notice success" role="status">客户标签与备注已保存。</div>}
    {customer ? <div className="crm-grid">
      <div><section className="card customer-card"><div className="customer-profile horizontal"><span className="large-avatar">{customer.nickname?.slice(0, 1) ?? '客'}</span><div><h2>{customer.nickname ?? '微信客户'}</h2><p>{customer.id}</p><Badge tone="blue">{sourceLabel(customer.channel ?? '')}</Badge></div></div><dl className="details-list"><div><dt>OpenID</dt><dd>{customer.openId ?? '—'}</dd></div><div><dt>UnionID</dt><dd>{customer.unionid ?? '—'}</dd></div><div><dt>手机号</dt><dd>{customer.phone ?? '未提供'}</dd></div></dl></section>
      <section className="card"><div className="card-heading"><h2>客户标签与备注</h2><button className="button primary small" onClick={() => void save()} disabled={saving}>{saving ? '保存中…' : '保存资料'}</button></div><div className="form-stack"><label>客户标签<input aria-label="客户标签" value={tags} onChange={event => setTags(event.target.value)} placeholder="例如：VIP, 售后, 重点关注" /><small>多个标签使用逗号分隔</small></label><label>服务备注<textarea aria-label="服务备注" rows={5} value={note} onChange={event => setNote(event.target.value)} maxLength={2000} placeholder="记录客户偏好、跟进事项等服务信息" /></label></div></section></div>
      <div><section className="card"><div className="card-heading"><h2>历史会话</h2><Badge>{customer.conversations?.length ?? 0}</Badge></div>{customer.conversations?.length ? <div className="compact-list">{customer.conversations.map(item => <div key={item.id}><div><strong>{sourceLabel(item.source)}</strong><Badge>{statusLabels[item.status] ?? item.status}</Badge></div><p>{formatTime(item.lastInboundAt)}</p><span className="muted">{item.id}</span></div>)}</div> : <EmptyState title="暂无历史会话" description="客户发起会话后将在此展示" />}</section>
      <section className="card"><div className="card-heading"><h2>客户工单</h2><button className="button text" onClick={() => setTicketOpen(true)}>+ 创建工单</button></div>{customer.tickets?.length ? <div className="compact-list">{customer.tickets.map(ticket => <div key={ticket.id}><strong>{ticket.title}</strong><p>{statusLabels[ticket.status ?? ''] ?? ticket.status ?? '待处理'} · {formatTime(ticket.createdAt)}</p></div>)}</div> : <EmptyState title="暂无工单" description="可从客户档案创建可追溯工单" />}</section></div>
    </div> : !loading && <div className="card"><EmptyState title="查询客户档案" description="输入会话中的客户 ID，查看并维护客户资料、历史会话和工单" /></div>}
    {customer && ticketOpen && <TicketDialog title={`客户 ${customer.nickname ?? customer.id}`} customerId={customer.id} onClose={() => setTicketOpen(false)} onCreated={() => { setTicketOpen(false); void search(customer.id) }} />}
  </div>
}
