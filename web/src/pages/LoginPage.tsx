import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { api } from '../api/client'
import { ErrorBanner } from '../components/ErrorBanner'
import { toApiError } from '../hooks/useAsync'

/** Only same-origin absolute paths are allowed as a post-login destination. */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return '/'
  return next
}

export function LoginPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [passphrase, setPassphrase] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (busy || passphrase === '') return
    setBusy(true)
    setError(null)
    try {
      await api.login(passphrase)
      navigate(safeNext(params.get('next')), { replace: true })
    } catch (err) {
      const ae = toApiError(err)
      if (ae.status === 401) setError('Wrong passphrase')
      else if (ae.status === 429) setError('Too many attempts, wait a minute')
      else setError(ae.message)
      setBusy(false)
    }
  }

  return (
    <main className="app-main">
      <div className="page login-page">
        <h1>Cards</h1>
        <form onSubmit={submit}>
          <div className="field">
            <label htmlFor="passphrase">Passphrase</label>
            <input
              id="passphrase"
              type="password"
              autoComplete="current-password"
              value={passphrase}
              onChange={(e) => setPassphrase(e.target.value)}
              autoFocus
            />
          </div>
          {error && <ErrorBanner error={error} />}
          <button type="submit" className="button button-primary" disabled={busy || passphrase === ''}>
            Log in
          </button>
        </form>
      </div>
    </main>
  )
}
