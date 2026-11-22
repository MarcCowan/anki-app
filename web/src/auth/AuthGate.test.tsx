import { act, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { makeApiMock } from '../test/mockApi'
import { AuthGate } from './AuthGate'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
const unauthorized = vi.hoisted(() => ({ handler: () => {} }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return {
    ...orig,
    get api() { return mockApi.current },
    onUnauthorized: (h: () => void) => { unauthorized.handler = h },
  }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
})

function LoginProbe() {
  const loc = useLocation()
  return <p>login page {loc.search}</p>
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/login" element={<LoginProbe />} />
        <Route path="*" element={<AuthGate><p>secret content</p></AuthGate>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('AuthGate', () => {
  it('renders children when auth disabled', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: false, authenticated: true })
    renderAt('/decks/1')
    expect(await screen.findByText('secret content')).toBeInTheDocument()
  })

  it('renders children when logged in', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: true })
    renderAt('/')
    expect(await screen.findByText('secret content')).toBeInTheDocument()
  })

  it('redirects to /login when required', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: false })
    renderAt('/decks/1?x=2')
    expect(await screen.findByText(`login page ?next=${encodeURIComponent('/decks/1?x=2')}`)).toBeInTheDocument()
    expect(screen.queryByText('secret content')).toBeNull()
  })

  it('redirects on any later 401', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: true })
    renderAt('/settings')
    await screen.findByText('secret content')
    act(() => unauthorized.handler())
    expect(await screen.findByText(`login page ?next=${encodeURIComponent('/settings')}`)).toBeInTheDocument()
  })
})
