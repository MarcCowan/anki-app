import { useState } from 'react'
import { Link } from 'react-router'
import { api } from '../api/client'
import { CountsLine, totalDue } from '../components/CountsLine'
import { ErrorBanner } from '../components/ErrorBanner'
import { Loading } from '../components/Loading'
import { toApiError, useAsync } from '../hooks/useAsync'

export function DecksPage() {
  const decks = useAsync(() => api.listDecks(), [])
  const [name, setName] = useState('')
  const [twoSided, setTwoSided] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  async function create(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim() || creating) return
    setCreating(true)
    setCreateError(null)
    try {
      await api.createDeck(name, twoSided)
      setName('')
      setTwoSided(false)
      decks.reload()
    } catch (err) {
      const ae = toApiError(err)
      setCreateError(ae.code === 'conflict' ? 'A deck with that name already exists' : ae.message)
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="page page-narrow">
      <h1>Decks</h1>
      {decks.error && <ErrorBanner error={decks.error} onRetry={decks.reload} />}
      {!decks.data && !decks.error && <Loading />}
      {decks.data && decks.data.length === 0 && <p className="muted">No decks yet. Create one below to start adding cards.</p>}
      {decks.data && decks.data.length > 0 && (
        <ul className="deck-list">
          {decks.data.map((d) => (
            <li key={d.id} className="deck-row">
              <div className="deck-row-main">
                <Link to={`/decks/${d.id}`} className="deck-name stretched-link-after">
                  {d.name}
                </Link>
                <CountsLine counts={d.counts} />
              </div>
              {totalDue(d.counts) > 0 ? (
                <Link to={`/decks/${d.id}/study`} className="button button-primary" aria-label={`Study ${d.name}`}>
                  Study
                </Link>
              ) : (
                <span className="button button-primary" aria-disabled="true" title="Nothing due right now">
                  Study
                </span>
              )}
            </li>
          ))}
        </ul>
      )}

      {decks.data && (
        <form className="inline-form" onSubmit={create}>
          <div className="field grow">
            <label htmlFor="new-deck">New deck name</label>
            <input
              id="new-deck"
              value={name}
              maxLength={100}
              autoComplete="off"
              onChange={(e) => {
                setName(e.target.value)
                setCreateError(null)
              }}
              aria-invalid={createError !== null}
              aria-describedby={createError ? 'new-deck-error' : undefined}
            />
            {createError && (
              <p id="new-deck-error" className="field-error">
                {createError}
              </p>
            )}
          </div>
          <label className="radio">
            <input type="checkbox" checked={twoSided} onChange={(e) => setTwoSided(e.target.checked)} />
            <span className="radio-label">Two-sided</span>
          </label>
          <button type="submit" className="button" disabled={!name.trim() || creating}>
            Create deck
          </button>
        </form>
      )}
    </div>
  )
}
