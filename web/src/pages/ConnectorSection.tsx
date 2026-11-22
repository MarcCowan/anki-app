import { useState } from 'react'
import { api } from '../api/client'
import type { McpBatch, McpStatus } from '../api/types'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { ErrorBanner } from '../components/ErrorBanner'
import { toApiError } from '../hooks/useAsync'
import { batchLine, cardsText, formatWhen, grantLine } from '../settings/connectorText'

type Pending = { kind: 'batch'; batch: McpBatch } | { kind: 'disconnect' } | null

/**
 * Settings → Claude connector: the connector URL, the connection, and recent
 * batches. The page loads the first status with everything else (so nothing
 * pops in); the section fetches again only after an action or a retry.
 */
export function ConnectorSection({ initial, loadError: initialError }: {
  initial: McpStatus | null
  loadError: string | null
}) {
  const [status, setStatus] = useState<McpStatus | null>(initial)
  const [loadError, setLoadError] = useState<string | null>(initialError)
  const [actionError, setActionError] = useState<string | null>(null)
  const [pending, setPending] = useState<Pending>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  async function reload() {
    try {
      setStatus(await api.getMcp())
      setLoadError(null)
    } catch (e) {
      setLoadError(toApiError(e).message)
    }
  }

  async function confirm() {
    if (!pending) return
    setBusy(true)
    try {
      if (pending.kind === 'batch') await api.deleteMcpBatch(pending.batch.id)
      else await api.disconnectMcp()
      setActionError(null)
      await reload()
    } catch (e) {
      setActionError(toApiError(e).message)
    } finally {
      setBusy(false)
      setPending(null)
    }
  }

  async function copy(url: string) {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <section aria-labelledby="connector-heading" className="connector">
      <h2 id="connector-heading">Claude connector</h2>
      {loadError && <ErrorBanner error={loadError} onRetry={() => void reload()} />}
      {actionError && <ErrorBanner error={actionError} />}
      {status && (
        <>
          {status.enabled ? (
            <>
              <div className="field">
                <span className="field-label">Connector URL</span>
                <div className="connector-url">
                  <code>{status.connectorUrl}</code>
                  <button type="button" className="button button-quiet" onClick={() => void copy(status.connectorUrl)}>
                    Copy
                  </button>
                  {copied && (
                    <span role="status" className="muted">
                      Copied
                    </span>
                  )}
                </div>
              </div>
              {status.grants.length === 0 ? (
                <p className="muted">Not connected</p>
              ) : (
                <>
                  <ul className="connector-list">
                    {status.grants.map((g) => (
                      <li key={g.id}>{grantLine(g)}</li>
                    ))}
                  </ul>
                  <button type="button" className="button" onClick={() => setPending({ kind: 'disconnect' })}>
                    Disconnect Claude
                  </button>
                </>
              )}
            </>
          ) : (
            <p className="muted">The Claude connector is off. See the VM runbook to turn it on.</p>
          )}
          {(status.enabled || status.batches.length > 0) && (
            <>
              <h3>Recent batches</h3>
              {status.batches.length === 0 ? (
                <p className="muted">No batches yet.</p>
              ) : (
                <ul className="connector-list">
                  {status.batches.map((b) => (
                    <li key={b.id} className="connector-batch">
                      <span>{batchLine(b)}</span>
                      <button type="button" className="button button-quiet" onClick={() => setPending({ kind: 'batch', batch: b })}>
                        Delete batch
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </>
      )}
      <ConfirmDialog
        open={pending?.kind === 'batch'}
        title="Delete batch"
        body={
          pending?.kind === 'batch'
            ? `Delete ${cardsText(pending.batch.remaining)} added by Claude on ${formatWhen(pending.batch.createdAt)}? ` +
              "This can't be undone. Cards you've edited since are deleted too."
            : ''
        }
        confirmLabel="Delete batch"
        busy={busy}
        onConfirm={() => void confirm()}
        onCancel={() => setPending(null)}
      />
      <ConfirmDialog
        open={pending?.kind === 'disconnect'}
        title="Disconnect Claude"
        body="Disconnect Claude? It will need to sign in again."
        confirmLabel="Disconnect"
        busy={busy}
        onConfirm={() => void confirm()}
        onCancel={() => setPending(null)}
      />
    </section>
  )
}
