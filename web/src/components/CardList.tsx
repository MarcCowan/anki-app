import { Link } from 'react-router'
import type { Card } from '../api/types'
import '../styles/card-list.css'
import { Markdown } from './Markdown'

/** The first non-blank line of a card side, used for one-line previews and link labels. */
export function firstLine(s: string): string {
  return s.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
}

const cardHref = (c: Card) => `/cards/${c.id}/edit`
const cardLabel = (c: Card) => firstLine(c.front) || '(empty)'

/*
 * Both layouts use a "stretched" link: an empty link positioned over the whole
 * row or tile. Card content can itself contain links, and nesting them inside
 * the card link would be invalid HTML.
 */

/** Narrow screens: one row per card, the front's first line with inline Markdown. */
export function CardRows({ cards }: { cards: Card[] }) {
  return (
    <ul className="card-rows">
      {cards.map((c) => (
        <li key={c.id} className="card-row">
          <Markdown source={firstLine(c.front) || '(empty)'} inline className="card-row-text" />
          <Link to={cardHref(c)} className="stretched-link" aria-label={cardLabel(c)} />
        </li>
      ))}
    </ul>
  )
}

/** Wide screens: fixed-size tiles, front above back, clipped with a fade. */
export function CardGrid({ cards }: { cards: Card[] }) {
  return (
    <ul className="card-grid" aria-label="Card previews">
      {cards.map((c) => (
        <li key={c.id} className="card-tile">
          <div className="card-tile-side card-tile-front">
            <Markdown source={c.front} />
          </div>
          <div className="card-tile-side card-tile-back">
            <Markdown source={c.back} />
          </div>
          <Link to={cardHref(c)} className="stretched-link" aria-label={cardLabel(c)} />
        </li>
      ))}
    </ul>
  )
}
