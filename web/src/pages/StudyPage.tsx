import { useCallback, useEffect, useReducer, useRef } from 'react'
import { Link, useParams } from 'react-router'
import { api } from '../api/client'
import type { Rating, RatingKey } from '../api/types'
import { CountsLine } from '../components/CountsLine'
import { ErrorBanner } from '../components/ErrorBanner'
import { Loading } from '../components/Loading'
import { Markdown } from '../components/Markdown'
import { toApiError } from '../hooks/useAsync'
import { faces, formatNextDue, shouldReloadAfterError, studyReducer } from '../study/studyReducer'
import '../styles/study.css'

const RATINGS: { rating: Rating; key: RatingKey; label: string }[] = [
  { rating: 1, key: 'again', label: 'Again' },
  { rating: 2, key: 'hard', label: 'Hard' },
  { rating: 3, key: 'good', label: 'Good' },
  { rating: 4, key: 'easy', label: 'Easy' },
]

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)
}

export function StudyPage() {
  const deckId = Number(useParams().id)
  const [state, dispatch] = useReducer(studyReducer, { kind: 'loading' })
  // Client-side half of the double-submit guard (the server's 409 is the other).
  const busy = useRef(false)

  const load = useCallback(async () => {
    try {
      const res = await api.next(deckId)
      dispatch({ type: 'loaded', res, now: Date.now() })
    } catch (e) {
      dispatch({ type: 'failed', error: toApiError(e) })
    }
  }, [deckId])

  useEffect(() => {
    void load()
  }, [load])

  const rate = useCallback(
    async (rating: Rating) => {
      if (state.kind !== 'back' || busy.current) return
      busy.current = true
      const { next, shownAt } = state
      try {
        const res = await api.review(next.stateId, { rating, expectedReps: next.reps, durationMs: Date.now() - shownAt })
        dispatch({ type: 'answered', stateId: next.stateId, repsAfter: res.state.reps })
        await load()
      } catch (e) {
        const err = toApiError(e)
        if (shouldReloadAfterError(err)) await load()
        else dispatch({ type: 'failed', error: err })
      } finally {
        busy.current = false
      }
    },
    [state, load],
  )

  const undo = useCallback(async () => {
    const last = state.last
    if (!last || busy.current) return
    busy.current = true
    try {
      await api.undo(last.stateId, last.repsAfter)
      dispatch({ type: 'undone' })
      await load()
    } catch (e) {
      const err = toApiError(e)
      if (shouldReloadAfterError(err)) {
        // The answer can no longer be undone (changed or deleted elsewhere).
        dispatch({ type: 'undone' })
        await load()
      } else {
        dispatch({ type: 'failed', error: err })
      }
    } finally {
      busy.current = false
    }
  }, [state, load])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey || isTyping(e.target)) return
      if (e.key === ' ') {
        if (state.kind === 'front' || state.kind === 'back') e.preventDefault()
        if (state.kind === 'front') dispatch({ type: 'reveal' })
        else if (state.kind === 'back') void rate(3)
      } else if (['1', '2', '3', '4'].includes(e.key)) {
        void rate(Number(e.key) as Rating)
      } else if (e.key === 'z' || e.key === 'Z') {
        void undo()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [state, rate, undo])

  const remaining = state.kind === 'front' || state.kind === 'back' ? state.next.remaining : null

  return (
    <div className="page study-page">
      <div className="study-head">
        {state.kind !== 'done' && (
          <Link to={`/decks/${deckId}`} className="button button-quiet">
            Back to deck
          </Link>
        )}
        {remaining && <CountsLine counts={remaining} />}
        {state.last && (
          <button type="button" className="button button-quiet" onClick={() => void undo()} title="Undo the last answer (Z)">
            Undo
          </button>
        )}
      </div>

      {state.kind === 'loading' && <Loading />}

      {state.kind === 'error' && <ErrorBanner error={state.message} onRetry={() => void load()} />}

      {state.kind === 'done' && (
        <div className="study-card study-done">
          <h1>Done for now</h1>
          {state.nextDueAt !== null && <p>{formatNextDue(state.nextDueAt)}</p>}
          <p>
            <Link to={`/decks/${deckId}`}>Back to deck</Link>
          </p>
        </div>
      )}

      {(state.kind === 'front' || state.kind === 'back') && (
        <>
          <article className="study-card" aria-label={state.next.reverse ? 'Card, reversed' : 'Card'}>
            {state.next.reverse && <p className="reverse-tag">Reverse: back → front</p>}
            <div className="card-face">
              <Markdown source={faces(state.next).question} className="card-side" />
            </div>
            {state.kind === 'back' && (
              <div className="card-face card-face-back">
                <Markdown source={faces(state.next).answer} className="card-side" />
              </div>
            )}
          </article>

          <div className="study-actions">
            {state.kind === 'front' ? (
              <button type="button" className="button button-primary reveal" onClick={() => dispatch({ type: 'reveal' })}>
                Show answer
              </button>
            ) : (
              <div className="rate-row">
                {RATINGS.map((r) => (
                  <button
                    key={r.rating}
                    type="button"
                    className={`button rate rate-${r.key}`}
                    aria-label={`${r.label} · ${state.next.previews[r.key].intervalLabel}`}
                    onClick={() => void rate(r.rating)}
                  >
                    <span className="rate-name">{r.label}</span>
                    <span className="rate-ivl">{state.next.previews[r.key].intervalLabel}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
