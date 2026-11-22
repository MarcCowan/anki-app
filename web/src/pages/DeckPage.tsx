import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router'
import { api } from '../api/client'
import type { Card } from '../api/types'
import { CardGrid, CardRows } from '../components/CardList'
import { CountsLine } from '../components/CountsLine'
import { ErrorBanner } from '../components/ErrorBanner'
import { Loading } from '../components/Loading'
import { useAsync } from '../hooks/useAsync'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { DeckSettings } from './DeckSettings'

/** From this width the deck view shows the card grid (2 columns, 3 from 900px) instead of one-line rows. */
export const GRID_QUERY = '(min-width: 600px)'

export function DeckPage() {
  const id = Number(useParams().id)
  const deck = useAsync(() => api.getDeck(id), [id])
  const cards = useAsync(() => api.listCards(id), [id])
  const [filter, setFilter] = useState('')
  const wide = useMediaQuery(GRID_QUERY)

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const all = cards.data ?? []
    if (!q) return all
    return all.filter((c) => c.front.toLowerCase().includes(q) || c.back.toLowerCase().includes(q))
  }, [cards.data, filter])

  const error = deck.error ?? cards.error
  if (error) {
    const retry = () => {
      deck.reload()
      cards.reload()
    }
    return (
      <div className="page page-narrow">
        <ErrorBanner error={error.status === 404 ? 'This deck no longer exists.' : error} onRetry={error.status === 404 ? undefined : retry} />
        <p>
          <Link to="/">Back to decks</Link>
        </p>
      </div>
    )
  }
  if (!deck.data || !cards.data) {
    return (
      <div className="page page-narrow">
        <Loading />
      </div>
    )
  }

  const d = deck.data
  return (
    <div className="page deck-page">
      <div className="page-head">
        <div>
          <h1>{d.name}</h1>
          <CountsLine counts={d.counts} />
        </div>
        <div className="actions">
          <Link to={`/decks/${d.id}/cards/new`} className="button">
            Add card
          </Link>
          <Link to={`/decks/${d.id}/study`} className="button button-primary">
            Study
          </Link>
        </div>
      </div>

      <section aria-labelledby="cards-heading">
        <h2 id="cards-heading">
          Cards <span className="muted">{cards.data.length}</span>
        </h2>
        <div className="field">
          <label htmlFor="card-filter">Filter cards</label>
          <input id="card-filter" type="search" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </div>
        <CardList cards={shown} wide={wide} empty={cards.data.length === 0 ? 'No cards yet. Add the first one.' : 'No cards match.'} />
      </section>

      <DeckSettings deck={d} onSaved={deck.setData} />
    </div>
  )
}

function CardList({ cards, wide, empty }: { cards: Card[]; wide: boolean; empty: string }) {
  if (cards.length === 0) return <p className="muted">{empty}</p>
  return wide ? <CardGrid cards={cards} /> : <CardRows cards={cards} />
}
