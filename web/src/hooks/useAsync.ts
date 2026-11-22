import { useCallback, useEffect, useState, type DependencyList } from 'react'
import { ApiError } from '../api/client'

export function toApiError(e: unknown): ApiError {
  if (e instanceof ApiError) return e
  return new ApiError(0, 'unknown', e instanceof Error ? e.message : 'Something went wrong')
}

interface AsyncState<T> {
  data?: T
  error?: ApiError
  loading: boolean
}

/** Run fn when deps change; reload() runs it again. setData replaces the result locally. */
export function useAsync<T>(fn: () => Promise<T>, deps: DependencyList) {
  const [state, setState] = useState<AsyncState<T>>({ loading: true })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    fn().then(
      (data) => {
        if (!cancelled) setState({ data, loading: false })
      },
      (e: unknown) => {
        if (!cancelled) setState((s) => ({ data: s.data, error: toApiError(e), loading: false }))
      },
    )
    return () => {
      cancelled = true
    }
    // fn is recreated every render; the caller's deps say when it really changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, attempt])

  const reload = useCallback(() => {
    setState((s) => ({ data: s.data, loading: true }))
    setAttempt((n) => n + 1)
  }, [])
  const setData = useCallback((data: T) => setState({ data, loading: false }), [])

  return { ...state, reload, setData }
}
