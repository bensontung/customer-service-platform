import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { ConversationDetail } from '../components/ConversationDetail'
import { Badge, EmptyState, ErrorNotice, formatTime, sourceLabel } from '../components/UI'
import type { Conversation } from '../types'

type Queue = 'mine' | 'waiting' | 'team'

export function ConversationsPage() {
  const { user } = useAuth()
  const [queue, setQueue] = useState<Queue>('mine')
  const [status, setStatus] = useState('open')
  const [source, setSource] = useState('')
  const [items, setItems] = useState<Conversation[]>([])
  const [selectedId, setSelectedId] = useState<string>()
  const deepLink = window.location.pathname.match(/^\/dingtalk\/conversations\/([^/]+)$/)
  const deepLinkId = deepLink ? decodeURIComponent(deepLink[1]) : undefined
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      if (deepLinkId) {
        await api.conversation(deepLinkId)
      }
      const result = await api.conversations({
        status: deepLinkId ? '' : status,
        source,
        ...(deepLinkId || queue !== 'mine' ? {} : { assignee: user!.agentId }),
        ...(!deepLinkId && queue === 'waiting' ? { assignee: 'unassigned' } : {}),
      })
      setItems(result.items ?? [])
      setSelectedId(current => deepLinkId ?? (result.items?.some(item => item.id === current) ? current : result.items?.[0]?.id))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '会话加载失败')
    } finally {
      setLoading(false)
    }
  }, [queue, source, status, user, deepLinkId])

  useEffect(() => { void load() }, [load])

  return <div className="workbench">
    <aside className="queue-panel">
      <div className="queue-heading"><div><h1>会话工作台</h1><p>及时回应，妥善处理</p></div><button className="icon-button refresh-button" onClick={() => void load()} title="刷新队列" aria-label="刷新队列">↻</button></div>
      <div className="segmented queue-tabs" aria-label="会话队列">
        <button className={queue === 'mine' ? 'active' : ''} onClick={() => setQueue('mine')}>我的会话</button>
        <button className={queue === 'waiting' ? 'active' : ''} onClick={() => { setQueue('waiting'); setStatus('') }}>待领取</button>
        <button className={queue === 'team' ? 'active' : ''} onClick={() => setQueue('team')}>团队队列</button>
      </div>
      <div className="queue-filters">
        <select aria-label="会话状态" value={status} onChange={event => setStatus(event.target.value)}><option value="">全部状态</option><option value="open">进行中</option><option value="waiting">待领取</option><option value="closed">已关闭</option></select>
        <select aria-label="会话来源" value={source} onChange={event => setSource(event.target.value)}><option value="">全部来源</option><option value="private_message">微信服务号/小程序私信</option></select>
      </div>
      <ErrorNotice message={error} />
      <div className="conversation-list" aria-busy={loading}>
        {loading ? <div className="list-loading"><span className="spinner" />加载会话…</div> : items.length === 0 ? <EmptyState title="暂时没有会话" description="当前筛选条件下没有需要处理的会话" /> : items.map(item => {
          const expired = item.replyDeadlineAt && new Date(item.replyDeadlineAt).getTime() <= Date.now()
          return <button key={item.id} className={`conversation-item ${selectedId === item.id ? 'selected' : ''}`} onClick={() => setSelectedId(item.id)}>
            <div className="conversation-item-top"><span className="customer-avatar">{item.customerId.slice(0, 1).toUpperCase()}</span><div className="conversation-item-name"><strong>客户 {item.customerId.slice(0, 8)}</strong><span>{formatTime(item.lastInboundAt)}</span></div>{!!item.unreadCount && <span className="unread">{item.unreadCount}</span>}</div>
            <div className="conversation-item-preview">{item.lastMessage ?? '查看客户最新咨询与历史消息'}</div>
            <div className="conversation-item-bottom"><Badge tone="blue">{sourceLabel(item.source)}</Badge>{!item.assigneeId ? <span className="waiting-text">待领取</span> : expired && item.status !== 'closed' ? <span className="danger-text">回复窗口已过期</span> : <span>{item.status === 'closed' ? '已关闭' : `可回复 ${formatTime(item.replyDeadlineAt)}`}</span>}</div>
          </button>
        })}
      </div>
      <div className="queue-footer">共 {items.length} 个会话<span>仅显示当前队列</span></div>
    </aside>
    <section className="detail-panel">
      {selectedId ? <ConversationDetail key={selectedId} id={selectedId} onChanged={load} /> : <EmptyState title="选择一个会话" description="从左侧队列选择会话，查看消息并回复客户" />}
    </section>
  </div>
}
