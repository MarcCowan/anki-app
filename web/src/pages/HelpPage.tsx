import type { ReactNode } from 'react'

const STUDY_KEYS: [ReactNode, string][] = [
  [<kbd key="s">Space</kbd>, 'Show the answer; once it is shown, rate Good'],
  [<><kbd>1</kbd> – <kbd>4</kbd></>, 'Rate Again, Hard, Good or Easy'],
  [<kbd key="z">Z</kbd>, 'Undo the last answer'],
]

const EDITOR_KEYS: [ReactNode, string][] = [
  [<><kbd>Ctrl</kbd> + <kbd>Enter</kbd></>, 'Save the card (⌘ + Enter on a Mac)'],
]

export function HelpPage() {
  return (
    <div className="page page-narrow help-page">
      <h1>Help</h1>

      <section aria-labelledby="help-study">
        <h2 id="help-study">Studying</h2>
        <p>
          Each card shows its front first. Reveal the answer, then rate how well you remembered it. The interval under
          each rating is when you will see the card again.
        </p>
        <p>
          A two-sided card is also studied back → front, marked <em>Reverse</em>, on its own schedule. The two directions
          never come up on the same day. New cards per day is a target: nothing stops you studying more.
        </p>
        <KeyTable rows={STUDY_KEYS} />
      </section>

      <section aria-labelledby="help-edit">
        <h2 id="help-edit">Editing cards</h2>
        <p>
          Both sides are Markdown. Use <code>`backticks`</code> for inline code and fenced blocks (<code>```cpp</code>)
          for highlighted code. Preview shows the card as it will look when you study it.
        </p>
        <KeyTable rows={EDITOR_KEYS} />
      </section>
    </div>
  )
}

function KeyTable({ rows }: { rows: [ReactNode, string][] }) {
  return (
    <table className="key-table">
      <thead>
        <tr>
          <th scope="col">Key</th>
          <th scope="col">Action</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(([keys, action]) => (
          <tr key={action}>
            <td>{keys}</td>
            <td>{action}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
