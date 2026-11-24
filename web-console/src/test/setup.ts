import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'
import { setAccessToken, setUnauthorizedHandler } from '../api/client'

vi.stubEnv('VITE_DINGTALK_CORP_ID', 'test-corp')
vi.stubEnv('VITE_DINGTALK_CLIENT_ID', 'test-client')

afterEach(() => {
  cleanup()
  setAccessToken(null)
  setUnauthorizedHandler(() => undefined)
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
  vi.stubEnv('VITE_DINGTALK_CORP_ID', 'test-corp')
  vi.stubEnv('VITE_DINGTALK_CLIENT_ID', 'test-client')
})
