import { useCallback, useEffect, useState } from 'react'
import { api } from '../api/client'
import { EmptyState, ErrorNotice, formatDuration, formatTime, formData, PageHeader, SubmitButton } from '../components/UI'
import { listOf, type AuditLog, type JsonObject, type Reports, type Shift } from '../types'

type Tab = 'shifts' | 'reports' | 'audit'

export function AdminPage() {
  const [tab, setTab] = useState<Tab>('shifts')
  return <div className="standard-page"><PageHeader title="排班与运营" description="管理客服排班，关注服务质量并追溯关键操作。" /><div className="tabs"><button className={tab === 'shifts' ? 'active' : ''} onClick={() => setTab('shifts')}>客服排班</button><button className={tab === 'reports' ? 'active' : ''} onClick={() => setTab('reports')}>基础报表</button><button className={tab === 'audit' ? 'active' : ''} onClick={() => setTab('audit')}>审计日志</button></div>{tab === 'shifts' ? <Shifts /> : tab === 'reports' ? <ReportsPanel /> : <Audit />}</div>
}

function Shifts() {
  const [items, setItems] = useState<Shift[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [syncBusy, setSyncBusy] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try { setItems(listOf<Shift>(await api.shifts())) } catch (reason) { setError(reason instanceof Error ? reason.message : '排班加载失败') } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  return <><div className="admin-columns"><section className="card"><div className="card-heading"><h2>新建排班</h2></div><form className="form-stack" onSubmit={event => { const form = event.currentTarget; const data = formData(event); if (!data.get('agentId') && !data.get('teamId')) { setError('客服 ID 与团队 ID 至少填写一项'); return } const startAt = new Date(String(data.get('startAt'))); const endAt = new Date(String(data.get('endAt'))); if (endAt <= startAt) { setError('结束时间必须晚于开始时间'); return } setBusy(true); setError(''); void api.saveShift({ agentId: String(data.get('agentId') ?? '') || undefined, teamId: String(data.get('teamId') ?? '') || undefined, startAt: startAt.toISOString(), endAt: endAt.toISOString(), state: String(data.get('state') ?? 'scheduled') }).then(() => { form.reset(); return load() }).catch(reason => setError(reason instanceof Error ? reason.message : '排班保存失败')).finally(() => setBusy(false)) }}><label>客服 ID<input name="agentId" placeholder="客服 ID 与团队 ID 至少填一项" /></label><label>团队 ID<input name="teamId" placeholder="可为整个团队排班" /></label><label>开始时间<input name="startAt" type="datetime-local" required /></label><label>结束时间<input name="endAt" type="datetime-local" required /></label><label>排班状态<select name="state" defaultValue="scheduled"><option value="scheduled">已排班</option><option value="active">生效中</option><option value="off">休息</option><option value="cancelled">已取消</option></select></label><SubmitButton busy={busy}>保存排班</SubmitButton></form></section><section className="card"><div className="card-heading"><h2>排班记录</h2><button className="button" onClick={() => void load()}>刷新</button></div>{loading ? <div className="loading"><span className="spinner" />加载排班…</div> : items.length ? <div className="table-wrap"><table><thead><tr><th>客服 / 团队</th><th>开始时间</th><th>结束时间</th><th>状态</th></tr></thead><tbody>{items.map((shift, index) => <tr key={shift.id ?? index}><td>{shift.agentName ?? shift.agentId ?? shift.teamId ?? '—'}</td><td>{formatTime(shift.startAt)}</td><td>{formatTime(shift.endAt)}</td><td>{shift.state ?? '已排班'}</td></tr>)}</tbody></table></div> : <EmptyState title="暂无排班" description="为客服或团队创建第一条排班" />}</section></div><ErrorNotice message={error} /><section className="card sync-card"><div><h2>同步文章评论</h2><p>按文章与游标同步服务号评论，由后端保证幂等，不会创建私信会话。</p></div><form className="sync-form" onSubmit={event => { const data = formData(event); setSyncBusy(true); setError(''); void api.syncComments({ accountKey: String(data.get('accountKey')), msgDataId: String(data.get('msgDataId')), articleIndex: Number(data.get('articleIndex') ?? 0), begin: Number(data.get('begin') ?? 0) }).then(() => load()).catch(reason => setError(reason instanceof Error ? reason.message : '评论同步失败')).finally(() => setSyncBusy(false)) }}><input name="accountKey" required aria-label="公众号账户标识" placeholder="公众号账户标识" /><input name="msgDataId" required aria-label="文章 msgDataId" placeholder="文章 msgDataId" /><input name="articleIndex" type="number" min="0" defaultValue="0" aria-label="文章序号" /><input name="begin" type="number" min="0" defaultValue="0" aria-label="评论起始游标" /><SubmitButton busy={syncBusy}>开始同步</SubmitButton></form></section></>
}

function ReportsPanel() {
  const [report, setReport] = useState<Reports>({ newConversations: 0, averageFirstResponseSeconds: 0, slaExpired: 0, closedConversations: 0 })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  useEffect(() => {
    void api.reports().then(result => {
      const data = (result && typeof result === 'object' ? result : {}) as JsonObject
      const nested = data.data && typeof data.data === 'object' && !Array.isArray(data.data) ? data.data as JsonObject : data
      setReport({ newConversations: Number(nested.newConversations ?? nested.newConversationCount ?? 0), averageFirstResponseSeconds: Number(nested.averageFirstResponseSeconds ?? nested.avgFirstResponseSeconds ?? 0), slaExpired: Number(nested.slaExpired ?? nested.slaTimeoutCount ?? 0), closedConversations: Number(nested.closedConversations ?? nested.closedConversationCount ?? 0) })
    }).catch(reason => setError(reason instanceof Error ? reason.message : '报表加载失败')).finally(() => setLoading(false))
  }, [])
  return <><ErrorNotice message={error} /><div className="metric-grid"><Metric label="新会话数" value={loading ? '…' : report.newConversations} detail="统计周期内新接入会话" /><Metric label="平均首次响应" value={loading ? '…' : formatDuration(report.averageFirstResponseSeconds)} detail="从客户消息到首次人工回复" /><Metric label="超时会话数" value={loading ? '…' : report.slaExpired} detail="超过 SLA 响应时限的会话" tone="orange" /><Metric label="已关闭会话" value={loading ? '…' : report.closedConversations} detail="统计周期内已完成会话" /></div><section className="card report-note"><h2>服务质量概览</h2><p>基础报表仅统计会话响应与处理情况。具体统计周期及口径由后端报表接口返回结果决定。</p></section></>
}

function Metric({ label, value, detail, tone }: { label: string; value: string | number; detail: string; tone?: string }) {
  return <section className={`card metric ${tone ?? ''}`}><span>{label}</span><strong>{value}</strong><p>{detail}</p></section>
}

function Audit() {
  const [logs, setLogs] = useState<AuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try { setLogs(listOf<AuditLog>(await api.auditLogs())) } catch (reason) { setError(reason instanceof Error ? reason.message : '审计日志加载失败') } finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  return <section className="card"><div className="card-heading"><div><h2>操作审计日志</h2><p>追溯领取、回复、转派、工单变更等关键操作</p></div><button className="button" onClick={() => void load()}>刷新</button></div><ErrorNotice message={error} />{loading ? <div className="loading"><span className="spinner" />加载日志…</div> : logs.length ? <div className="table-wrap"><table><thead><tr><th>时间</th><th>操作人</th><th>操作</th><th>资源类型</th><th>资源 ID</th><th>详情</th></tr></thead><tbody>{logs.map((log, index) => <tr key={log.id ?? index}><td>{formatTime(log.createdAt)}</td><td>{log.actorName ?? log.actorId ?? '—'}</td><td>{log.action}</td><td>{log.resourceType ?? '—'}</td><td>{log.resourceId ?? '—'}</td><td>{log.detail ?? '—'}</td></tr>)}</tbody></table></div> : <EmptyState title="暂无审计记录" description="关键业务操作将在这里留下记录" />}</section>
}
