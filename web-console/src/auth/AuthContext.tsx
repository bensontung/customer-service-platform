import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, setAccessToken, setUnauthorizedHandler } from '../api/client'
import type { Session, User } from '../types'

interface AuthContextValue {
  user: User | null
  signIn: (session: Session) => void
  signOut: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)

  useEffect(() => {
    setUnauthorizedHandler(() => setUser(null))
    return () => setUnauthorizedHandler(() => undefined)
  }, [])

  const signIn = (session: Session) => {
    setAccessToken(session.token)
    setUser({ agentId: session.agentId, role: session.role, name: session.name })
    void api.me().then(setUser).catch(() => undefined)
  }

  const signOut = () => {
    setAccessToken(null)
    setUser(null)
  }

  return <AuthContext.Provider value={{ user, signIn, signOut }}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}
