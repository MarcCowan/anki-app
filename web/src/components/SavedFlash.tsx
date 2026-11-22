/** A small "✓ Saved" note; the parent removes it after SAVED_FLASH_MS. */
export function SavedFlash({ text }: { text: string }) {
  return (
    <span role="status" className="saved-flash">
      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
        <path d="m5 12.5 4.5 4.5L19 7.5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {text}
    </span>
  )
}
