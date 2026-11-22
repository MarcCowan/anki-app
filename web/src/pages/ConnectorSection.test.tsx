import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import type { McpStatus } from '../api/types'
import { makeApiMock } from '../test/mockApi'
import { ConnectorSection } from './ConnectorSection'

const mockApi = vi.hoisted(() => ({ current: null as unknown as ReturnType<typeof makeApiMock> }))
vi.mock('../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../api/client')>()
  return { ...orig, get api() { return mockApi.current } }
})

const ms = Date.UTC(2026, 10, 8, 14, 5)

const off: McpStatus = { enabled: false, connectorUrl: '', grants: [], batches: [] }
const on: McpStatus = {
  enabled: true,
  connectorUrl: 'https://anki.test:8443/mcp',
  grants: [{ id: 1, createdAt: ms, lastUsedAt: ms }],
  batches: [{ id: 5, deckId: 2, deckName: 'Go', createdAt: ms, cardCount: 12, remaining: 12 }],
}

beforeEach(() => {
  mockApi.current = makeApiMock()
})

function show(initial: McpStatus) {
  return render(<ConnectorSection initial={initial} loadError={null} />)
}

describe('ConnectorSection', () => {
  it('says when the connector is off, without fetching again', () => {
    show(off)
    expect(screen.getByText('The Claude connector is off. See the VM runbook to turn it on.')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Claude connector' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Disconnect Claude' })).not.toBeInTheDocument()
    expect(mockApi.current.getMcp).not.toHaveBeenCalled()
  })

  it('still lists batches when the connector is off', () => {
    show({ ...on, enabled: false, connectorUrl: '', grants: [] })
    expect(screen.getByRole('button', { name: 'Delete batch' })).toBeInTheDocument()
  })

  it('shows the URL, the connection and the batches', () => {
    show(on)
    expect(screen.getByText('https://anki.test:8443/mcp')).toBeInTheDocument()
    expect(screen.getByText(/^Connected /)).toBeInTheDocument()
    expect(screen.getByText(/ · Go · 12 cards$/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
  })

  it('says when nothing is connected and there are no batches', () => {
    show({ ...on, grants: [], batches: [] })
    expect(screen.getByText('Not connected')).toBeInTheDocument()
    expect(screen.getByText('No batches yet.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Disconnect Claude' })).not.toBeInTheDocument()
  })

  it('deletes a batch after confirming, then reloads', async () => {
    mockApi.current.getMcp.mockResolvedValue({ ...on, batches: [] })
    mockApi.current.deleteMcpBatch.mockResolvedValue(undefined)
    show(on)
    await userEvent.click(screen.getByRole('button', { name: 'Delete batch' }))
    const dialog = screen.getByRole('dialog')
    expect(
      within(dialog).getByText(
        /^Delete 12 cards added by Claude on .+\? This can't be undone\. Cards you've edited since are deleted too\.$/,
      ),
    ).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete batch' }))
    expect(mockApi.current.deleteMcpBatch).toHaveBeenCalledWith(5)
    expect(await screen.findByText('No batches yet.')).toBeInTheDocument()
  })

  it('does nothing when the delete is cancelled', async () => {
    show(on)
    await userEvent.click(screen.getByRole('button', { name: 'Delete batch' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(mockApi.current.deleteMcpBatch).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('disconnects Claude after confirming', async () => {
    mockApi.current.getMcp.mockResolvedValue({ ...on, grants: [] })
    mockApi.current.disconnectMcp.mockResolvedValue(undefined)
    show(on)
    await userEvent.click(screen.getByRole('button', { name: 'Disconnect Claude' }))
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Disconnect Claude? It will need to sign in again.')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }))
    expect(mockApi.current.disconnectMcp).toHaveBeenCalled()
    expect(await screen.findByText('Not connected')).toBeInTheDocument()
  })

  it('shows an API error from a delete', async () => {
    mockApi.current.deleteMcpBatch.mockRejectedValue(new ApiError(500, 'internal', 'internal error'))
    show(on)
    await userEvent.click(screen.getByRole('button', { name: 'Delete batch' }))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete batch' }))
    await waitFor(() => expect(screen.getByText('internal error')).toBeInTheDocument())
  })

  it('shows a load error with a retry that fetches the status', async () => {
    mockApi.current.getMcp.mockResolvedValue(off)
    render(<ConnectorSection initial={null} loadError="Could not reach the server" />)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not reach the server')
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('The Claude connector is off. See the VM runbook to turn it on.')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
