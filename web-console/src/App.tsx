import { useState } from 'react'
import { useAuth } from './auth/AuthContext'
import { Badge } from './components/UI'
import { AdminPage } from './pages/AdminPage'
import { CommentsPage } from './pages/CommentsPage'
import { ConversationsPage } from './pages/ConversationsPage'
import { CrmPage } from './pages/CrmPage'
import { LoginPage } from './pages/LoginPage'
import { TicketsPage } from './pages/TicketsPage'

export type Page = 'conversations' | 'comments' | 'crm' | 'tickets' | 'admin'

const navigation: Array<{ id: Page; label: string; icon: string }> = [
  { id: 'conversations', label: '会话工作台', icon: '▤' },
  { id: 'comments', label: '评论管理', icon: '◫' },
  { id: 'crm', label: '客户 CRM', icon: '◎' },
  { id: 'tickets', label: '工单中心', icon: '▧' },
]

export function App() {
  const { user, signOut } = useAuth()
  const [page, setPage] = useState<Page>('conversations')
  if (!user) return <LoginPage />

  return <div className="app-shell">
    <aside className="sidebar">
      <div className="sidebar-brand"><div className="brand-mark small">客</div><span>云桥客服</span></div>
      <div className="workspace-label">工作空间</div>
      <nav aria-label="主导航">
        {navigation.map(item => <button key={item.id} className={`nav-item ${page === item.id ? 'active' : ''}`} onClick={() => setPage(item.id)}><span className="nav-icon">{item.icon}</span>{item.label}</button>)}
      </nav>
      {user.role === 'admin' && <>
        <div className="workspace-label admin-label">管理与分析</div>
        <nav><button className={`nav-item ${page === 'admin' ? 'active' : ''}`} onClick={() => setPage('admin')}><span className="nav-icon">▦</span>排班与运营</button></nav>
      </>}
      <div className="sidebar-bottom"><div className="service-status"><span className="status-dot" />客服系统运行中</div><div className="sidebar-version">企业服务 · 安全连接</div></div>
    </aside>
    <div className="main-shell">
      <header className="topbar">
        <div className="topbar-title">客服工作台 <span>/</span> {navigation.find(item => item.id === page)?.label ?? '排班与运营'}</div>
        <div className="topbar-user">
          {user.onShift !== undefined && <Badge tone={user.onShift ? 'green' : 'neutral'}>{user.onShift ? '在班' : '未排班'}</Badge>}
          <div className="avatar">{user.name.slice(0, 1)}</div>
          <div className="user-info"><strong>{user.name}</strong><span>{user.role === 'admin' ? '管理员' : user.role === 'supervisor' ? '主管' : '客服'}</span></div>
          <button className="button text" onClick={signOut}>退出</button>
        </div>
      </header>
      <main className={`page-content ${page === 'conversations' ? 'conversation-page' : ''}`}>
        {page === 'conversations' && <ConversationsPage />}
        {page === 'comments' && <CommentsPage />}
        {page === 'crm' && <CrmPage />}
        {page === 'tickets' && <TicketsPage />}
        {page === 'admin' && user.role === 'admin' && <AdminPage />}
      </main>
    </div>
  </div>
}
