import { createBrowserRouter, Link, NavLink, Outlet, type RouteObject } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { AuthGate } from './auth/AuthGate'
import { ThemeToggle } from './components/ThemeToggle'
import { CardEditorPage } from './pages/CardEditorPage'
import { DeckPage } from './pages/DeckPage'
import { DecksPage } from './pages/DecksPage'
import { HelpPage } from './pages/HelpPage'
import { LoginPage } from './pages/LoginPage'
import { SettingsPage } from './pages/SettingsPage'
import { StudyPage } from './pages/StudyPage'

function Layout() {
  return (
    <>
      <header className="app-header">
        <div className="app-header-inner">
          <Link to="/" className="brand">
            Home
          </Link>
          <nav aria-label="Main">
            <NavLink to="/" end>
              Decks
            </NavLink>
            <NavLink to="/settings">Settings</NavLink>
            <NavLink to="/help">Help</NavLink>
            <ThemeToggle />
          </nav>
        </div>
      </header>
      <main className="app-main">
        <Outlet />
      </main>
    </>
  )
}

export function NotFoundPage() {
  return (
    <div className="page page-narrow">
      <h1>Page not found</h1>
      <p>
        <Link to="/">Back to decks</Link>
      </p>
    </div>
  )
}

export const routes: RouteObject[] = [
  {
    element: (
      <AuthGate>
        <Layout />
      </AuthGate>
    ),
    children: [
      { path: '/', element: <DecksPage /> },
      { path: '/decks/:id', element: <DeckPage /> },
      { path: '/decks/:id/study', element: <StudyPage /> },
      { path: '/decks/:id/cards/new', element: <CardEditorPage mode="new" /> },
      { path: '/cards/:id/edit', element: <CardEditorPage mode="edit" /> },
      { path: '/settings', element: <SettingsPage /> },
      { path: '/help', element: <HelpPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
  { path: '/login', element: <LoginPage /> },
]

const router = createBrowserRouter(routes)

export default function App() {
  return <RouterProvider router={router} />
}
