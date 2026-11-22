import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { ErrorBanner } from '../components/ErrorBanner'
import { Loading } from '../components/Loading'
import { Markdown } from '../components/Markdown'
import { SavedFlash } from '../components/SavedFlash'
import { toApiError } from '../hooks/useAsync'
import '../styles/editor.css'
import { countChars, MAX_SIDE } from '../text/countChars'

type Mode = 'new' | 'edit'
type Tab = 'edit' | 'preview'
type Flash = { text: string; seq: number }

/** How long the "Saved" / "Card added" note stays up. */
export const SAVED_FLASH_MS = 2500

export function CardEditorPage({ mode }: { mode: Mode }) {
  const id = Number(useParams().id)
  const navigate = useNavigate()
  const [front, setFront] = useState('')
  const [back, setBack] = useState('')
  const [deckId, setDeckId] = useState<number | null>(mode === 'new' ? id : null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [flash, setFlash] = useState<Flash | null>(null)
  const [loaded, setLoaded] = useState(false)
  // Two-sided: the editor's choice, and what the server last saved (null for a new card).
  const [twoSided, setTwoSided] = useState(false)
  const [savedTwoSided, setSavedTwoSided] = useState<boolean | null>(null)
  const [confirmingOff, setConfirmingOff] = useState(false)
  const [saving, setSaving] = useState(false)
  // An existing card opens read-only (Preview); a new one opens ready to type.
  const [tab, setTab] = useState<Tab>(mode === 'edit' ? 'preview' : 'edit')
  const [confirming, setConfirming] = useState(false)
  const frontRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    let cancelled = false
    const failed = (gone: string) => (e: unknown) => {
      if (cancelled) return
      const ae = toApiError(e)
      setLoadError(ae.status === 404 ? gone : ae.message)
    }
    if (mode === 'new') {
      // A new card starts with the deck's two-sided default.
      api.getDeck(id).then((d) => {
        if (cancelled) return
        setTwoSided(d.twoSided)
        setLoaded(true)
      }, failed('This deck no longer exists.'))
    } else {
      api.getCard(id).then((c) => {
        if (cancelled) return
        setFront(c.front)
        setBack(c.back)
        setDeckId(c.deckId)
        setTwoSided(c.twoSided)
        setSavedTwoSided(c.twoSided)
        setLoaded(true)
      }, failed('This card no longer exists.'))
    }
    return () => {
      cancelled = true
    }
  }, [mode, id])

  // Each save restarts the timer (seq changes even when the text does not).
  useEffect(() => {
    if (!flash) return
    const t = setTimeout(() => setFlash(null), SAVED_FLASH_MS)
    return () => clearTimeout(t)
  }, [flash])
  const showFlash = (text: string) => setFlash((f) => ({ text, seq: (f?.seq ?? 0) + 1 }))

  const sideOk = (s: string) => s.trim() !== '' && countChars(s) <= MAX_SIDE
  const canSave = sideOk(front) && sideOk(back) && !saving && deckId !== null

  // Turning a saved card's two-sided off deletes its reverse history: ask first.
  function save() {
    if (!canSave) return
    if (savedTwoSided === true && !twoSided) setConfirmingOff(true)
    else void doSave()
  }

  async function doSave() {
    setConfirmingOff(false)
    setSaving(true)
    setSaveError(null)
    setFlash(null)
    try {
      if (mode === 'new') {
        await api.createCard(id, { front, back, twoSided })
        setFront('')
        setBack('')
        setTab('edit')
        showFlash('Card added')
        frontRef.current?.focus()
      } else {
        const c = await api.updateCard(id, { front, back, twoSided })
        setSavedTwoSided(c.twoSided)
        showFlash('Saved')
      }
    } catch (e) {
      setSaveError(toApiError(e).message)
    } finally {
      setSaving(false)
    }
  }

  async function remove() {
    try {
      await api.deleteCard(id)
      navigate(`/decks/${deckId}`)
    } catch (e) {
      setConfirming(false)
      setSaveError(toApiError(e).message)
    }
  }

  if (loadError) {
    return (
      <div className="page page-narrow">
        <ErrorBanner error={loadError} />
        <p>
          <Link to="/">Back to decks</Link>
        </p>
      </div>
    )
  }

  if (!loaded) {
    return (
      <div className="page page-narrow">
        <Loading />
      </div>
    )
  }

  const edited = (setter: (v: string) => void) => (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setter(e.target.value)
    setFlash(null)
  }

  return (
    <div className="page editor-page">
      <div className="page-head">
        <h1>{mode === 'new' ? 'New card' : 'Card'}</h1>
        {deckId !== null && (
          <Link to={`/decks/${deckId}`} className="button button-quiet">
            Back to deck
          </Link>
        )}
      </div>

      <div className="editor-tabs" role="tablist" aria-label="Editor view">
        {(['edit', 'preview'] as const).map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={tab === t}
            className="editor-tab"
            onClick={() => setTab(t)}
          >
            {t === 'edit' ? 'Edit' : 'Preview'}
          </button>
        ))}
      </div>

      <form
        className="editor"
        data-tab={tab}
        onSubmit={(e) => {
          e.preventDefault()
          save()
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault()
            save()
          }
        }}
      >
        <div className="editor-inputs">
          <SideInput id="card-front" label="Front" value={front} onChange={edited(setFront)} inputRef={frontRef} />
          <SideInput id="card-back" label="Back" value={back} onChange={edited(setBack)} />
        </div>
        <div className="editor-preview study-card">
          <section aria-label="Front preview" className="card-face">
            <Markdown source={front} className="card-side" />
          </section>
          <section aria-label="Back preview" className="card-face card-face-back">
            <Markdown source={back} className="card-side" />
          </section>
        </div>

        <div className="editor-actions">
          <label className="radio two-sided-check">
            <input
              type="checkbox"
              checked={twoSided}
              aria-describedby="two-sided-hint"
              onChange={(e) => {
                setTwoSided(e.target.checked)
                setFlash(null)
              }}
            />
            <span>
              <span className="radio-label">Two-sided</span>
              <span className="muted radio-hint" id="two-sided-hint">
                Also study it back → front, on its own schedule
              </span>
            </span>
          </label>
          {saveError && <ErrorBanner error={saveError} />}
          <div className="actions">
            <button type="submit" className="button button-primary" disabled={!canSave}>
              {mode === 'new' ? 'Add card' : 'Save changes'}
            </button>
            {flash && <SavedFlash key={flash.seq} text={flash.text} />}
            {mode === 'edit' && (
              <button type="button" className="button button-danger-quiet push-right" onClick={() => setConfirming(true)}>
                Delete card
              </button>
            )}
          </div>
        </div>
      </form>

      <ConfirmDialog
        open={confirming}
        title="Delete this card?"
        body={<p>Its review history is deleted too. This cannot be undone.</p>}
        confirmLabel="Delete this card"
        onConfirm={remove}
        onCancel={() => setConfirming(false)}
      />
      <ConfirmDialog
        open={confirmingOff}
        title="Remove the reverse side?"
        body={<p>The back → front direction and its review history are deleted. The front → back direction is kept.</p>}
        confirmLabel="Remove reverse side"
        onConfirm={() => void doSave()}
        onCancel={() => setConfirmingOff(false)}
      />
    </div>
  )
}

function SideInput({ id, label, value, onChange, inputRef }: {
  id: string
  label: string
  value: string
  onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void
  inputRef?: React.Ref<HTMLTextAreaElement>
}) {
  const n = countChars(value)
  const over = n > MAX_SIDE
  return (
    <div className="field">
      <div className="side-label">
        <label htmlFor={id}>{label}</label>
        <span className={over ? 'char-count over' : 'char-count'} id={`${id}-count`}>
          {n} / {MAX_SIDE}
        </span>
      </div>
      <textarea
        id={id}
        ref={inputRef}
        value={value}
        onChange={onChange}
        rows={8}
        spellCheck={false}
        autoCapitalize="off"
        aria-invalid={over}
        aria-describedby={`${id}-count`}
      />
    </div>
  )
}
