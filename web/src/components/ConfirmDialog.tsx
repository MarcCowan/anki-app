import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

interface Props {
  open: boolean
  title: string
  body: ReactNode
  confirmLabel: string
  /** When set, the confirm button stays disabled until this exact text is typed. */
  requireText?: string
  busy?: boolean
  /** Confirm button style: danger (default) for destructive actions, primary otherwise. */
  tone?: 'danger' | 'primary'
  /** An optional second, non-destructive choice shown between Cancel and confirm. */
  alt?: { label: string; onChoose: () => void }
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog(props: Props) {
  if (!props.open) return null
  return <OpenDialog {...props} />
}

function OpenDialog({ title, body, confirmLabel, requireText, busy, tone = 'danger', alt, onConfirm, onCancel }: Props) {
  const [typed, setTyped] = useState('')
  const titleId = useId()
  const inputId = useId()
  const firstRef = useRef<HTMLInputElement & HTMLButtonElement>(null)

  useEffect(() => {
    firstRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onCancel])

  const ready = !busy && (requireText === undefined || typed === requireText)

  return (
    <div className="dialog-backdrop" onClick={onCancel}>
      <div
        className="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId}>{title}</h2>
        <div className="dialog-body">{body}</div>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (ready) onConfirm()
          }}
        >
          {requireText !== undefined && (
            <div className="field">
              <label htmlFor={inputId}>Type {requireText} to confirm</label>
              <input
                id={inputId}
                ref={firstRef}
                value={typed}
                autoComplete="off"
                spellCheck={false}
                onChange={(e) => setTyped(e.target.value)}
              />
            </div>
          )}
          <div className="dialog-actions">
            <button
              type="button"
              className="button"
              onClick={onCancel}
              ref={requireText === undefined ? firstRef : undefined}
            >
              Cancel
            </button>
            {alt && (
              <button type="button" className="button" onClick={alt.onChoose} disabled={busy}>
                {alt.label}
              </button>
            )}
            <button type="submit" className={`button button-${tone}`} disabled={!ready}>
              {confirmLabel}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
