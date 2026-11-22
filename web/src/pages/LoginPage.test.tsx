import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { makeApiMock } from '../test/mockApi'
import { LoginPage, safeNext } from './LoginPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
})

function Where() {
  return <p>at {useLocation().pathname}</p>
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  )
}

async function submit(p: string) {
  const field = screen.getByLabelText('Passphrase')
  expect(field).toHaveAttribute('type', 'password')
  expect(field).toHaveAttribute('autocomplete', 'current-password')
  await userEvent.type(field, p)
  await userEvent.click(screen.getByRole('button', { name: 'Log in' }))
}

describe('LoginPage', () => {
  it('Login success navigates to next', async () => {
    mockApi.current.login.mockResolvedValue(undefined)
    renderAt('/login?next=%2Fdecks%2F3')
    await submit('correct horse battery staple')
    expect(mockApi.current.login).toHaveBeenCalledWith('correct horse battery staple')
    expect(await screen.findByText('at /decks/3')).toBeInTheDocument()
  })

  it('Login rejects open-redirect next', async () => {
    mockApi.current.login.mockResolvedValue(undefined)
    renderAt('/login?next=%2F%2Fevil.example')
    await submit('p')
    expect(await screen.findByText('at /')).toBeInTheDocument()
  })

  it('safeNext accepts only same-origin paths', () => {
    expect(safeNext('/decks/1?x=2')).toBe('/decks/1?x=2')
    for (const bad of [null, '', '//evil.example', '/\\evil.example', 'https://evil.example', 'javascript:alert(1)', 'decks']) {
      expect(safeNext(bad)).toBe('/')
    }
  })

  it('shows wrong passphrase on 401', async () => {
    mockApi.current.login.mockRejectedValue(new ApiError(401, 'bad_passphrase', 'wrong passphrase'))
    renderAt('/login')
    await submit('nope')
    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong passphrase')
  })

  it('Login shows rate-limit message on 429', async () => {
    mockApi.current.login.mockRejectedValue(new ApiError(429, 'rate_limited', 'x'))
    renderAt('/login')
    await submit('nope')
    expect(await screen.findByRole('alert')).toHaveTextContent('Too many attempts, wait a minute')
  })
})
