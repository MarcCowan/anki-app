import { useState } from 'react'
import { useNavigate } from 'react-router'
import { api } from '../api/client'
import type { Deck } from '../api/types'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { ErrorBanner } from '../components/ErrorBanner'
import { toApiError } from '../hooks/useAsync'

/** Which two-sided confirmation is open, if any (two-sided spec §1, §5). */
type TwoSidedConfirm = 'on' | 'off' | null

export function DeckSettings({ deck, onSaved }: { deck: Deck; onSaved: (d: Deck) => void }) {
  const navigate = useNavigate()
  const [name, setName] = useState(deck.name)
  const [perDay, setPerDay] = useState(String(deck.newPerDay))
  const [twoSided, setTwoSided] = useState(deck.twoSided)
  const [twoSidedConfirm, setTwoSidedConfirm] = useState<TwoSidedConfirm>(null)
  const [message, setMessage] = useState<{ error: boolean; text: string } | null>(null)
  const [saving, setSaving] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const perDayNum = Number(perDay)
  const perDayValid = perDay.trim() !== '' && Number.isInteger(perDayNum) && perDayNum >= 0 && perDayNum <= 9999

  // Changing two-sided affects every card, so it is confirmed first.
  function save(e: React.FormEvent) {
    e.preventDefault()
    if (!perDayValid || saving) return
    if (twoSided !== deck.twoSided) setTwoSidedConfirm(twoSided ? 'on' : 'off')
    else void send({})
  }

  async function send(extra: { twoSided?: boolean; removeReverses?: boolean }) {
    setTwoSidedConfirm(null)
    setSaving(true)
    setMessage(null)
    try {
      const updated = await api.updateDeck(deck.id, { name, newPerDay: perDayNum, ...extra })
      onSaved(updated)
      setName(updated.name)
      setTwoSided(updated.twoSided)
      setMessage({ error: false, text: 'Saved' })
    } catch (err) {
      const ae = toApiError(err)
      setMessage({ error: true, text: ae.code === 'conflict' ? 'A deck with that name already exists' : ae.message })
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    setDeleting(true)
    setDeleteError(null)
    try {
      await api.deleteDeck(deck.id)
      navigate('/')
    } catch (err) {
      setDeleteError(toApiError(err).message)
      setDeleting(false)
      setConfirming(false)
    }
  }

  return (
    <section aria-labelledby="deck-settings-heading" className="settings-section">
      <h2 id="deck-settings-heading">Deck settings</h2>
      <form onSubmit={save} className="stack">
        <div className="field">
          <label htmlFor="deck-name">Deck name</label>
          <input id="deck-name" value={name} maxLength={100} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="deck-per-day">New cards per day</label>
          <input
            id="deck-per-day"
            type="number"
            inputMode="numeric"
            min={0}
            max={9999}
            step={1}
            value={perDay}
            onChange={(e) => setPerDay(e.target.value)}
            aria-invalid={!perDayValid}
            aria-describedby="deck-per-day-hint"
          />
          <p className="muted field-hint" id="deck-per-day-hint">
            A daily target, not a limit. New today: {deck.counts.newToday ?? 0} of {deck.newPerDay}.
          </p>
        </div>
        <label className="radio">
          <input
            type="checkbox"
            checked={twoSided}
            aria-describedby="deck-two-sided-hint"
            onChange={(e) => setTwoSided(e.target.checked)}
          />
          <span>
            <span className="radio-label">Two-sided by default</span>
            <span className="muted radio-hint" id="deck-two-sided-hint">
              New cards are also studied back → front. You can still turn it off per card.
            </span>
          </span>
        </label>
        <div className="actions">
          <button type="submit" className="button" disabled={!perDayValid || !name.trim() || saving} aria-label="Save deck settings">
            Save
          </button>
          {message && (
            <span className={message.error ? 'field-error' : 'muted'} role="status">
              {message.text}
            </span>
          )}
        </div>
      </form>

      <div className="danger-zone">
        <button type="button" className="button button-danger-quiet" onClick={() => setConfirming(true)}>
          Delete deck
        </button>
        {deleteError && <ErrorBanner error={deleteError} />}
      </div>
      <ConfirmDialog
        open={twoSidedConfirm === 'on'}
        title="Make every card two-sided?"
        body={
          <p>
            Every card in {deck.name} gets a back → front direction, including cards you turned off one by one. Cards
            that already have one keep it and its history.
          </p>
        }
        confirmLabel="Make all two-sided"
        tone="primary"
        onConfirm={() => void send({ twoSided: true })}
        onCancel={() => setTwoSidedConfirm(null)}
      />
      <ConfirmDialog
        open={twoSidedConfirm === 'off'}
        title="Turn off two-sided by default?"
        body={
          <p>
            New cards will be one-sided. The cards already in {deck.name} can keep their reverse side, or lose it along
            with its review history.
          </p>
        }
        alt={{ label: 'Keep reverse sides', onChoose: () => void send({ twoSided: false }) }}
        confirmLabel="Remove all reverse sides"
        onConfirm={() => void send({ twoSided: false, removeReverses: true })}
        onCancel={() => setTwoSidedConfirm(null)}
      />
      <ConfirmDialog
        open={confirming}
        title={`Delete ${deck.name}?`}
        body={<p>This deletes the deck, all its cards and their review history. It cannot be undone.</p>}
        confirmLabel="Delete deck and its cards"
        requireText={deck.name}
        busy={deleting}
        onConfirm={remove}
        onCancel={() => setConfirming(false)}
      />
    </section>
  )
}
