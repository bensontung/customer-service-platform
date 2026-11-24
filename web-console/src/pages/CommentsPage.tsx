import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { TicketDialog } from '../components/TicketDialog'
import { Badge, EmptyState, ErrorNotice, formatTime, PageHeader, statusLabels, SubmitButton } from '../components/UI'
import { listOf, type Comment } from '../types'

export function CommentsPage() {
  const [comments, setComments] = useState<Comment[]>([])
  const [selectedId, setSelectedId] = useState<string>()
  const [status, setStatus] = useState('')
  const [accountId, setAccountId] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [ticketOpen, setTicketOpen] = useState(false)
  const [replyOpen, setReplyOpen] = useState(false)
  const selected = comments.find(comment => comment.id === selectedId)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const result = listOf<Comment>(await api.comments(status, accountId))
      setComments(result)
      setSelectedId(current => result.some(comment => comment.id === current) ? current : result[0]?.id)
    } catch (reason) { setError(reason instanceof Error ? reason.message : '评论加载失败') }
    finally { setLoading(false) }
  }, [accountId, status])
  useEffect(() => { void load() }, [load])

  async function action(comment: Comment, action: string, content?: string) {
    if (action === 'delete' && !window.confirm('确认删除这条文章评论？')) return
    setBusy(true)
    setError('')
    try {
      await api.commentAction(comment.id, action, content)
      setReplyOpen(false)
      await load()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '评论操作失败') }
    finally { setBusy(false) }
  }

  return <div className="standard-page">
    <PageHeader title="评论管理" description="独立处理微信服务号文章评论，审核与互动操作将完整留痕。" actions={<button className="button" onClick={() => void load()}>刷新列表</button>} />
    <div className="notice info"><strong>文章评论与私信相互独立</strong><span>这里仅支持微信平台授权的评论操作，不提供、也不承诺私信回复能力。</span></div>
    <div className="toolbar"><select aria-label="评论状态" value={status} onChange={event => setStatus(event.target.value)}><option value="">全部审核状态</option><option value="normal">普通评论</option><option value="featured">已精选</option><option value="replied">已回复</option><option value="deleted">已删除</option></select><input aria-label="公众号账户 ID" placeholder="公众号账户 ID（可选）" value={accountId} onChange={event => setAccountId(event.target.value)} /><button className="button primary" onClick={() => void load()}>查询</button></div>
    <ErrorNotice message={error} />
    <div className="split-content comment-content">
      <section className="card list-card"><div className="card-heading"><h2>评论列表</h2><Badge>{comments.length} 条</Badge></div>{loading ? <div className="loading"><span className="spinner" />加载评论…</div> : comments.length === 0 ? <EmptyState title="暂无评论" description="当前筛选条件下没有文章评论" /> : comments.map(comment => <button className={`comment-list-item ${selectedId === comment.id ? 'selected' : ''}`} key={comment.id} onClick={() => setSelectedId(comment.id)}><div><strong>{comment.articleTitle ?? comment.title ?? '服务号文章'}</strong><Badge tone={comment.status === 'pending_review' ? 'orange' : 'neutral'}>{statusLabels[comment.status] ?? comment.status}</Badge></div><p>{comment.content}</p><span>{comment.authorName ?? comment.nickname ?? '微信用户'} · {formatTime(comment.createdAt)}</span></button>)}</section>
      <section className="card comment-detail">{selected ? <><div className="card-heading"><div><span className="eyebrow">文章评论</span><h2>{selected.articleTitle ?? selected.title ?? '服务号文章'}</h2></div><Badge tone="blue">{statusLabels[selected.status] ?? selected.status}</Badge></div><div className="comment-author"><span className="avatar">{(selected.authorName ?? selected.nickname ?? '微').slice(0, 1)}</span><div><strong>{selected.authorName ?? selected.nickname ?? '微信用户'}</strong><p>{formatTime(selected.createdAt)} · 评论 ID: {selected.id}</p></div></div><blockquote>{selected.content}</blockquote><div className="comment-actions"><h3>评论操作</h3><p className="muted">以下操作需后端确认当前公众号具备相应权限。</p><div><button className="button" disabled={busy} onClick={() => void action(selected, selected.status === 'featured' ? 'unfeature' : 'feature')}>{selected.status === 'featured' ? '取消精选' : '设为精选'}</button><button className="button" disabled={busy} onClick={() => setReplyOpen(value => !value)}>回复评论</button><button className="button danger" disabled={busy} onClick={() => void action(selected, 'delete')}>删除评论</button></div>{replyOpen && <form className="comment-reply" onSubmit={event => { event.preventDefault(); const data = new FormData(event.currentTarget); void action(selected, 'reply', String(data.get('content') ?? '')) }}><label>评论公开回复<textarea name="content" required rows={3} placeholder="回复将显示在文章评论区，不是私信" /></label><SubmitButton busy={busy}>提交评论回复</SubmitButton></form>}</div><div className="comment-history"><div className="section-title"><h3>操作历史</h3><button className="button text" onClick={() => setTicketOpen(true)}>+ 创建工单</button></div>{(selected.history ?? selected.actions ?? []).length ? (selected.history ?? selected.actions ?? []).map((event, index) => <div className="timeline-item" key={`${event.createdAt}-${index}`}><span className="timeline-dot" /><div><strong>{event.action}</strong><p>{event.content}</p><span>{formatTime(event.createdAt)}</span></div></div>) : <p className="muted">暂无操作记录，后续操作将显示在这里。</p>}</div></> : <EmptyState title="选择一条评论" description="查看评论详情、审核状态与操作历史" />}</section>
    </div>
    {selected && ticketOpen && <TicketDialog title={`文章评论 ${selected.id}`} commentId={selected.id} onClose={() => setTicketOpen(false)} onCreated={() => setTicketOpen(false)} />}
  </div>
}
