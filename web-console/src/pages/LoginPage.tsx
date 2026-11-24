import { useEffect, useRef, useState } from 'react'
import dd from 'dingtalk-jsapi'
import { api } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { ErrorNotice } from '../components/UI'

const corpId = import.meta.env.VITE_DINGTALK_CORP_ID as string | undefined
const clientId = import.meta.env.VITE_DINGTALK_CLIENT_ID as string | undefined

export function LoginPage() {
  const { signIn } = useAuth()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(() => /DingTalk/i.test(window.navigator.userAgent) ? '' : '当前仅支持钉钉内免登，请从钉钉工作台打开；浏览器 OAuth 尚未启用。')
  const inFlight = useRef(false)
  const isDingTalk = /DingTalk/i.test(window.navigator.userAgent)

  async function login() {
    if (inFlight.current) return
    if (!isDingTalk) {
      setError('当前仅支持钉钉内免登，请从钉钉工作台打开；浏览器 OAuth 尚未启用。')
      return
    }
    if (!corpId || !clientId) {
      setError('未配置 VITE_DINGTALK_CORP_ID / VITE_DINGTALK_CLIENT_ID，请联系管理员。')
      return
    }
    inFlight.current = true
    setBusy(true)
    setError('')
    try {
      const { code } = await dd.requestAuthCode({ corpId, clientId })
      if (!code) throw new Error('钉钉未返回有效授权码，请重试')
      const session = await api.login(code, corpId)
      signIn(session)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '免登失败，请重新授权')
    } finally {
      inFlight.current = false
      setBusy(false)
    }
  }

  useEffect(() => {
    if (isDingTalk && corpId && clientId) void login()
  }, [])

  return <main className="login-page">
    <section className="login-brand">
      <div className="brand-mark">客</div>
      <div className="brand-name">云桥客服</div>
      <h1>连接每一次沟通，<br />做好每一次服务。</h1>
      <p>微信服务号、小程序客服统一工作台。<br />让团队协作更高效，让客户问题有回应。</p>
      <div className="login-brand-footer">安全 · 协同 · 可追溯</div>
    </section>
    <section className="login-panel">
      <div className="login-card">
        <div className="mobile-brand"><div className="brand-mark">客</div>云桥客服</div>
        <span className="eyebrow">WELCOME BACK</span>
        <h2>欢迎回来</h2>
        <p className="login-subtitle">使用企业钉钉身份登录客服工作台</p>
        <div className="login-security"><span className="security-icon">✓</span><div><strong>企业身份安全认证</strong><p>授权信息仅用于本次登录，不会保存您的钉钉密码</p></div></div>
        <button className="button primary dingtalk-button" onClick={() => void login()} disabled={busy}>
          <span className="dingtalk-symbol" aria-hidden="true">D</span>{busy ? '正在验证身份…' : '使用钉钉登录'}
        </button>
        <ErrorNotice message={error} />
        {isDingTalk && !busy && <p className="muted center">若免登未自动完成，请点击上方按钮重试。</p>}
        <p className="login-help">首次使用或无法登录？请联系企业管理员开通权限</p>
        <div className="login-terms">登录即代表您同意遵守企业信息安全与隐私保护规范</div>
      </div>
    </section>
  </main>
}
