import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App.tsx'
import { applyDisplay, loadDisplay } from './settings/display'
import { applyTheme, loadTheme } from './settings/theme'
import './styles/base.css'
import './styles/card.css'
import './styles/study.css'

applyDisplay(loadDisplay())
applyTheme(loadTheme())

const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
