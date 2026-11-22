import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router'
import { api, onUnauthorized } from '../api/client'

/**
 * Shows its children unless passphrase mode is on and there is no session,
 * in which case it sends the user to /login?next=<current path>. Any later
 * 401 from the API does the same.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  const location = useLocation()
  const here = location.pathname + location.search
  const [ready, setReady] = useState(false)
  const hereRef = useRef(here)

  useEffect(() => {
    hereRef.current = here
  }, [here])

  useEffect(() => {
    const toLogin = () => navigate(`/login?next=${encodeURIComponent(here)}`, { replace: true })
    onUnauthorized(toLogin)
    return () => onUnauthorized(() => {})
  }, [navigate, here])

  useEffect(() => {
    let cancelled = false
    api.authStatus().then(
      (s) => {
        if (cancelled) return
        if (s.enabled && !s.authenticated) {
          navigate(`/login?next=${encodeURIComponent(hereRef.current)}`, { replace: true })
        } else {
          setReady(true)
        }
      },
      // If the status cannot be read, show the app: its own requests report the error.
      () => {
        if (!cancelled) setReady(true)
      },
    )
    return () => {
      cancelled = true
    }
    // Checked once per mount; later 401s are handled by onUnauthorized.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return ready ? <>{children}</> : null
}
