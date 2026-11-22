import { useState } from 'react'
import { useNavigate } from 'react-router'
import { api, type ApiError } from '../api/client'
import { ErrorBanner } from '../components/ErrorBanner'
import { Loading } from '../components/Loading'
import { Markdown } from '../components/Markdown'
import { toApiError, useAsync } from '../hooks/useAsync'
import {
  applyDisplay,
  clampCols,
  loadDisplay,
  MAX_COLS,
  MIN_COLS,
  saveDisplay,
  type DisplaySettings,
  type WrapMode,
} from '../settings/display'
import { versionLine } from '../settings/versionLine'
import type { Settings } from '../api/types'
import { ConnectorSection } from './ConnectorSection'

const SAMPLE = [
  'A sample card so you can see the effect:',
  '',
  '```cpp',
  '#include <vector>',
  '',
  'std::vector<std::pair<std::string, int>> scores = load_scores_from_file("results.txt", /*skip_header=*/true);',
  'for (const auto& [name, score] : scores) {',
  '    std::cout << name << ": " << score << "\\n";',
  '}',
  '```',
].join('\n')

const WRAP_OPTIONS: { value: WrapMode; label: string; hint: string }[] = [
  { value: 'auto', label: 'Auto', hint: 'Wrap on narrow screens, scroll on wide ones' },
  { value: 'scroll', label: 'Scroll', hint: 'Long lines scroll sideways' },
  { value: 'wrap', label: 'Wrap', hint: 'Long lines wrap, marked with ↪' },
]

type Loaded<T> = { ok: true; value: T } | { ok: false; error: ApiError }

const settle = <T,>(p: Promise<T>): Promise<Loaded<T>> =>
  p.then(
    (value): Loaded<T> => ({ ok: true, value }),
    (e: unknown): Loaded<T> => ({ ok: false, error: toApiError(e) }),
  )

/**
 * Loads everything the page shows before rendering any of it, so sections
 * never pop in and push the rest down. Only a scheduling failure is shown;
 * auth status and health just hide their parts when they fail.
 */
export function SettingsPage() {
  const page = useAsync(
    () => Promise.all([settle(api.getSettings()), settle(api.authStatus()), settle(api.health()), settle(api.getMcp())]),
    [],
  )

  if (!page.data) {
    return (
      <div className="page page-narrow">
        <h1>Settings</h1>
        <Loading />
      </div>
    )
  }

  const [settings, auth, health, mcp] = page.data
  return (
    <div className="page page-narrow">
      <h1>Settings</h1>
      <SchedulingSettings
        key={settings.ok ? 'loaded' : 'error'}
        initial={settings.ok ? settings.value : null}
        loadError={settings.ok ? null : settings.error.message}
        onRetry={page.reload}
      />
      <DisplaySection />
      <ConnectorSection
        key={mcp.ok ? 'loaded' : 'error'}
        initial={mcp.ok ? mcp.value : null}
        loadError={mcp.ok ? null : mcp.error.message}
      />
      {auth.ok && auth.value.enabled && <AccountSection />}
      {health.ok && <p className="muted version-line">{versionLine(health.value)}</p>}
    </div>
  )
}

function SchedulingSettings({ initial, loadError, onRetry }: {
  initial: Settings | null
  loadError: string | null
  onRetry: () => void
}) {
  const [retention, setRetention] = useState(initial ? String(initial.desiredRetention) : '')
  const [hour, setHour] = useState(initial ? String(initial.dayRolloverHour) : '')
  const [message, setMessage] = useState<{ error: boolean; text: string } | null>(null)
  const [saving, setSaving] = useState(false)

  async function save(e: React.FormEvent) {
    e.preventDefault()
    setSaving(true)
    setMessage(null)
    try {
      const s = await api.updateSettings({ desiredRetention: Number(retention), dayRolloverHour: Number(hour) })
      setRetention(String(s.desiredRetention))
      setHour(String(s.dayRolloverHour))
      setMessage({ error: false, text: 'Saved' })
    } catch (err) {
      setMessage({ error: true, text: toApiError(err).message })
    } finally {
      setSaving(false)
    }
  }

  return (
    <section aria-labelledby="scheduling-heading">
      <h2 id="scheduling-heading">Scheduling</h2>
      <p className="muted">Shared by all your devices.</p>
      {loadError && <ErrorBanner error={loadError} onRetry={onRetry} />}
      {initial && (
        <form onSubmit={save}>
          <div className="field">
            <label htmlFor="retention">Desired retention</label>
            <input
              id="retention"
              type="number"
              inputMode="decimal"
              min={0.7}
              max={0.99}
              step={0.01}
              value={retention}
              onChange={(e) => setRetention(e.target.value)}
            />
            <span className="muted field-hint">How likely you want to be to remember a card when it comes due (0.70 to 0.99). Higher means more reviews.</span>
          </div>
          <div className="field">
            <label htmlFor="rollover">New day starts at (hour)</label>
            <input
              id="rollover"
              type="number"
              inputMode="numeric"
              min={0}
              max={23}
              step={1}
              value={hour}
              onChange={(e) => setHour(e.target.value)}
            />
            <span className="muted field-hint">The daily new-card limit resets at this hour (0 to 23).</span>
          </div>
          <div className="actions">
            <button type="submit" className="button" disabled={saving || retention === '' || hour === ''} aria-label="Save scheduling">
              Save
            </button>
            {message && (
              <span role={message.error ? 'alert' : 'status'} className={message.error ? 'field-error' : 'muted'}>
                {message.text}
              </span>
            )}
          </div>
        </form>
      )}
    </section>
  )
}

function DisplaySection() {
  const [display, setDisplay] = useState<DisplaySettings>(loadDisplay)
  const [cols, setCols] = useState(String(display.codeCols))

  function update(next: DisplaySettings) {
    setDisplay(next)
    saveDisplay(next)
    applyDisplay(next)
  }

  return (
    <section aria-labelledby="display-heading">
      <h2 id="display-heading">Display</h2>
      <p className="muted">Only on this device.</p>
      <fieldset className="radio-group">
        <legend>Long code lines</legend>
        {WRAP_OPTIONS.map((o) => (
          <label key={o.value} className="radio">
            <input
              type="radio"
              name="wrap-mode"
              value={o.value}
              checked={display.wrapMode === o.value}
              aria-label={o.label}
              aria-describedby={`wrap-hint-${o.value}`}
              onChange={() => update({ ...display, wrapMode: o.value })}
            />
            <span>
              <span className="radio-label">{o.label}</span>
              <span className="muted radio-hint" id={`wrap-hint-${o.value}`}>
                {o.hint}
              </span>
            </span>
          </label>
        ))}
      </fieldset>
      <div className="field">
        <label htmlFor="code-cols">Code width (columns)</label>
        <input
          id="code-cols"
          type="number"
          inputMode="numeric"
          min={MIN_COLS}
          max={MAX_COLS}
          step={1}
          value={cols}
          onChange={(e) => {
            setCols(e.target.value)
            const n = Number(e.target.value)
            if (e.target.value !== '' && n >= MIN_COLS && n <= MAX_COLS) update({ ...display, codeCols: clampCols(n) })
          }}
          onBlur={() => setCols(String(display.codeCols))}
        />
      </div>
      <div className="study-card sample-card">
        <div className="card-face">
          <Markdown source={SAMPLE} className="card-side" />
        </div>
      </div>
    </section>
  )
}

function AccountSection() {
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)

  async function logout() {
    try {
      await api.logout()
      navigate('/login', { replace: true })
    } catch (e) {
      setError(toApiError(e).message)
    }
  }

  return (
    <section aria-labelledby="account-heading">
      <h2 id="account-heading">Account</h2>
      {error && <ErrorBanner error={error} />}
      <button type="button" className="button" onClick={() => void logout()}>
        Log out
      </button>
    </section>
  )
}
