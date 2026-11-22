import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { RouterProvider } from 'react-router/dom'
import { afterEach, expect, it } from 'vitest'
import { routes } from './App'

afterEach(() => {
  localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
})

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<RouterProvider router={router} />)
}

it('shows a not-found page with a link home for unknown routes', async () => {
  renderAt('/nope/nothing')
  expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  expect(screen.getByRole('link', { name: 'Back to decks' })).toHaveAttribute('href', '/')
  expect(screen.getByRole('navigation', { name: 'Main' })).toBeInTheDocument()
})

it('header has Home and Help links', async () => {
  renderAt('/nope')
  expect(await screen.findByRole('link', { name: 'Home' })).toHaveAttribute('href', '/')
  expect(screen.getByRole('link', { name: 'Help' })).toHaveAttribute('href', '/help')
})

it('serves the help page', async () => {
  renderAt('/help')
  expect(await screen.findByRole('heading', { level: 1, name: 'Help' })).toBeInTheDocument()
})

it('theme toggle flips light and dark and remembers the choice', async () => {
  localStorage.setItem('theme.v1', 'light')
  renderAt('/nope')
  await userEvent.click(await screen.findByRole('button', { name: 'Switch to dark theme' }))
  expect(document.documentElement.dataset.theme).toBe('dark')
  expect(localStorage.getItem('theme.v1')).toBe('dark')
  await userEvent.click(screen.getByRole('button', { name: 'Switch to light theme' }))
  expect(document.documentElement.dataset.theme).toBe('light')
  expect(localStorage.getItem('theme.v1')).toBe('light')
})
