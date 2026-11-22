/**
 * Placeholder while a page's data loads. The spinner fades in after a short
 * delay (CSS), so a fast response shows nothing at all instead of a flash.
 */
export function Loading() {
  return (
    <div className="loading" role="status" aria-label="Loading">
      <span className="spinner" />
    </div>
  )
}
