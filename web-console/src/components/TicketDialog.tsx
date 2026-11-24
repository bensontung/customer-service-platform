import { useState } from 'react'
import { api } from '../api/client'
import type { TicketDraft } from '../types'
import { Dialog, ErrorNotice, formData, SubmitButton } from './UI'

interface Props {
  title: string
  customerId?: string
  conversationId?: string
  commentId?: string
  onClose: () => void
  onCreated: (id: string) => void
}

export function TicketDialog({ title, customerId, conversationId, commentId, onClose, onCreated }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(data: FormData) {
    setBusy(true)
    setError('')
    const draft: TicketDraft = {
      title: String(data.get('title') ?? ''),
      description: String(data.get('description') ?? ''),
      priority: String(data.get('priority') ?? 'normal'),
      assigneeId: String(data.get('assigneeId') ?? '') || undefined,
      dueAt: data.get('dueAt') ? new Date(String(data.get('dueAt'))).toISOString() : undefined,
      customerId,
      conversationId,
      commentId,
    }
    try {
      const result = await api.createTicket(draft)
      onCreated(result.id)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '工单创建失败')
    } finally {
      setBusy(false)
    }
  }

  return <Dialog title="创建工单" onClose={onClose}>
    <form className="form-stack" onSubmit={event => void submit(formData(event))}>
      <p className="muted">来源：{title}。工单将保留关联对象，便于后续追溯。</p>
      <label>工单标题<input name="title" required maxLength={200} placeholder="简要说明需要处理的问题" /></label>
      <label>问题描述<textarea name="description" rows={4} placeholder="补充问题背景与处理要求" /></label>
      <div className="form-grid">
        <label>优先级<select name="priority" defaultValue="normal"><option value="low">低</option><option value="normal">普通</option><option value="high">高</option><option value="urgent">紧急</option></select></label>
        <label>负责人 ID<input name="assigneeId" placeholder="可选" /></label>
        <label>处理截止时间<input name="dueAt" type="datetime-local" /></label>
      </div>
      <ErrorNotice message={error} />
      <div className="dialog-actions"><button type="button" className="button" onClick={onClose}>取消</button><SubmitButton busy={busy}>创建工单</SubmitButton></div>
    </form>
  </Dialog>
}
