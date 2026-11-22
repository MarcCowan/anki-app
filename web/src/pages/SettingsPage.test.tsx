import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { makeApiMock } from '../test/mockApi'
import { SettingsPage } from './SettingsPage'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

beforeEach(() => {
  mockApi.current = makeApiMock()
  mockApi.current.getSettings.mockResolvedValue({ desiredRetention: 0.9, dayRolloverHour: 4 })
  mockApi.current.authStatus.mockResolvedValue({ enabled: false, authenticated: true })
  mockApi.current.health.mockResolvedValue({ version: '3f2a9c1d0b7e', lastBackupAt: null })
  mockApi.current.getMcp.mockResolvedValue({ enabled: false, connectorUrl: '', grants: [], batches: [] })
})
afterEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('code-wrap')
})

function renderPage() {
  return render(
    <MemoryRouter>
      <SettingsPage />
    </MemoryRouter>,
  )
}

describe('SettingsPage', () => {
  it('loads and saves scheduling settings', async () => {
    mockApi.current.updateSettings.mockResolvedValue({ desiredRetention: 0.85, dayRolloverHour: 4 })
    renderPage()
    const retention = await screen.findByLabelText('Desired retention')
    expect(retention).toHaveValue(0.9)
    expect(screen.getByLabelText('New day starts at (hour)')).toHaveValue(4)
    await userEvent.clear(retention)
    await userEvent.type(retention, '0.85')
    await userEvent.click(screen.getByRole('button', { name: 'Save scheduling' }))
    expect(mockApi.current.updateSettings).toHaveBeenCalledWith({ desiredRetention: 0.85, dayRolloverHour: 4 })
    expect(await screen.findByText('Saved')).toBeInTheDocument()
  })

  it('shows server validation error', async () => {
    mockApi.current.updateSettings.mockRejectedValue(new ApiError(400, 'invalid_input', 'desiredRetention must be between 0.70 and 0.99'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Save scheduling' }))
    expect(await screen.findByText('desiredRetention must be between 0.70 and 0.99')).toBeInTheDocument()
  })

  it('saves display settings locally', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('radio', { name: 'Wrap' }))
    expect(JSON.parse(localStorage.getItem('display.v1')!)).toMatchObject({ wrapMode: 'wrap' })
    expect(document.documentElement).toHaveClass('code-wrap')
    await userEvent.click(screen.getByRole('radio', { name: 'Scroll' }))
    expect(document.documentElement).not.toHaveClass('code-wrap')

    const width = screen.getByLabelText('Code width (columns)')
    await userEvent.clear(width)
    await userEvent.type(width, '72')
    expect(JSON.parse(localStorage.getItem('display.v1')!)).toMatchObject({ codeCols: 72 })
    expect(document.documentElement.style.getPropertyValue('--code-cols')).toBe('72')
  })

  it('hides log out when passphrase mode is off', async () => {
    renderPage()
    await screen.findByLabelText('Desired retention')
    await Promise.resolve()
    expect(screen.queryByRole('button', { name: 'Log out' })).toBeNull()
  })

  it('logs out when passphrase mode is on', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: true })
    mockApi.current.logout.mockResolvedValue(undefined)
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <Routes>
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/login" element={<p>login page</p>} />
        </Routes>
      </MemoryRouter>,
    )
    await userEvent.click(await screen.findByRole('button', { name: 'Log out' }))
    expect(mockApi.current.logout).toHaveBeenCalledOnce()
    expect(await screen.findByText('login page')).toBeInTheDocument()
  })

  it('shows the version line after Account when health loads', async () => {
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: true })
    renderPage()
    const line = await screen.findByText('Version 3f2a9c1d0b7e · Last backup: none yet')
    const logout = await screen.findByRole('button', { name: 'Log out' })
    expect(logout.compareDocumentPosition(line) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('omits the version line when health fails', async () => {
    mockApi.current.health.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    renderPage()
    expect(await screen.findByLabelText('Desired retention')).toBeInTheDocument()
    await waitFor(() => expect(mockApi.current.health).toHaveBeenCalled())
    await Promise.resolve()
    expect(screen.queryByText(/^Version /)).toBeNull()
  })

  it('shows a sample code block', async () => {
    const { container } = renderPage()
    await screen.findByLabelText('Desired retention')
    const lines = [...container.querySelectorAll('pre.code-block .line')].map((l) => l.textContent ?? '')
    expect(lines.some((l) => l.length > 100)).toBe(true)
  })
})

describe('SettingsPage loading', () => {
  it('renders nothing below the heading until the settings arrive, then everything at once', async () => {
    let resolve!: (v: { desiredRetention: number; dayRolloverHour: number }) => void
    mockApi.current.getSettings.mockReturnValue(new Promise((r) => (resolve = r)))
    mockApi.current.authStatus.mockResolvedValue({ enabled: true, authenticated: true })
    renderPage()
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Display' })).toBeNull()
    resolve({ desiredRetention: 0.9, dayRolloverHour: 4 })
    await screen.findByLabelText('Desired retention')
    // Account and the version line arrive in the same render, so nothing shifts later.
    expect(screen.getByRole('button', { name: 'Log out' })).toBeInTheDocument()
    expect(screen.getByText('Version 3f2a9c1d0b7e · Last backup: none yet')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Display' })).toBeInTheDocument()
  })

  it('still shows the page, with a retry, when the settings fail to load', async () => {
    mockApi.current.getSettings.mockRejectedValueOnce(new ApiError(0, 'network', 'Could not reach the server'))
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server')
    expect(screen.getByRole('heading', { name: 'Display' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByLabelText('Desired retention')).toHaveValue(0.9)
  })

  it('waits for the connector status too, then shows it in the same render', async () => {
    let resolve!: (v: Awaited<ReturnType<typeof mockApi.current.getMcp>>) => void
    mockApi.current.getMcp.mockReturnValue(new Promise((r) => (resolve = r)))
    renderPage()
    // Settings, auth and health have resolved; the page still waits for the connector.
    await waitFor(() => expect(mockApi.current.health).toHaveBeenCalled())
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Display' })).toBeNull()
    resolve({ enabled: false, connectorUrl: '', grants: [], batches: [] })
    await screen.findByLabelText('Desired retention')
    expect(screen.getByText('The Claude connector is off. See the VM runbook to turn it on.')).toBeInTheDocument()
    expect(mockApi.current.getMcp).toHaveBeenCalledOnce()
  })

  it('shows only the connector section failing when its status fails to load', async () => {
    mockApi.current.getMcp.mockRejectedValueOnce(new ApiError(0, 'network', 'Could not reach the server'))
    renderPage()
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server')
    expect(screen.getByLabelText('Desired retention')).toHaveValue(0.9)
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('The Claude connector is off. See the VM runbook to turn it on.')).toBeInTheDocument()
  })
})
