import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { expect, it } from 'vitest'
import { HelpPage } from './HelpPage'

it('lists the study and editor keyboard shortcuts', () => {
  render(
    <MemoryRouter>
      <HelpPage />
    </MemoryRouter>,
  )
  expect(screen.getByRole('heading', { level: 1, name: 'Help' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Studying' })).toBeInTheDocument()
  expect(screen.getByRole('heading', { name: 'Editing cards' })).toBeInTheDocument()
  for (const key of ['Space', '1', '4', 'Z', 'Enter']) expect(screen.getAllByText(key).length).toBeGreaterThan(0)
})
