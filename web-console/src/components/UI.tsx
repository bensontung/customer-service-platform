import type { FormEvent, ReactNode } from 'react'

export const sourceLabels: Record<string, string> = {
  official_account: '微信服务号',
  service_account: '微信服务号',
  mini_program: '微信小程序',
  miniprogram: '微信小程序',
}

export const statusLabels: Record<string, string> = {
  open: '进行中',
  active: '进行中',
  pending: '待处理',
  waiting: '待领取',
  unassigned: '待领取',
  closed: '已关闭',
  queued: '排队发送中',
  pending_send: '本地待发送',
  retrying: '发送重试中',
  sent: '已送达微信',
  delivered: '已送达微信',
  failed: '发送失败',
  failed_final: '最终发送失败',
  approved: '已通过',
  pending_review: '待审核',
  rejected: '未通过',
  featured: '已精选',
  deleted: '已删除',
  replied: '已回复',
}

export function sourceLabel(source: string) {
  return sourceLabels[source] ?? source
}

export function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

export function formatDuration(seconds: number) {
  if (!seconds) return '—'
  return seconds < 60 ? `${Math.round(seconds)} 秒` : `${Math.round(seconds / 60)} 分钟`
}

export function Badge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'blue' | 'green' | 'orange' | 'red' }) {
  return <span className={`badge badge-${tone}`}>{children}</span>
}

export function PageHeader({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return <div className="page-header"><div><h1>{title}</h1><p>{description}</p></div>{actions}</div>
}

export function ErrorNotice({ message }: { message: string }) {
  return message ? <div className="notice error" role="alert">{message}</div> : null
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return <div className="empty-state"><div className="empty-icon">○</div><h3>{title}</h3><p>{description}</p></div>
}

export function Loading() {
  return <div className="loading" role="status"><span className="spinner" />正在加载…</div>
}

export function Dialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  return <div className="dialog-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <section className="dialog" role="dialog" aria-modal="true" aria-label={title}>
      <div className="dialog-header"><h2>{title}</h2><button type="button" className="icon-button" onClick={onClose} aria-label="关闭">×</button></div>
      {children}
    </section>
  </div>
}

export function SubmitButton({ busy, children }: { busy?: boolean; children: ReactNode }) {
  return <button type="submit" className="button primary" disabled={busy}>{busy ? '提交中…' : children}</button>
}

export function formData(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  return new FormData(event.currentTarget)
}
