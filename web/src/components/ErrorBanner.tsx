import type { ApiError } from '../api/client'

export function ErrorBanner({ error, onRetry }: { error: ApiError | string; onRetry?: () => void }) {
  const message = typeof error === 'string' ? error : error.message || 'Something went wrong'
  return (
    <div className="banner banner-error" role="alert">
      <span>{message}</span>
      {onRetry && (
        <button type="button" className="button button-quiet" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  )
}
