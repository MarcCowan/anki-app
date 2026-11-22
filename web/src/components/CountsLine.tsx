import type { Counts } from '../api/types'

/** The learning / review / new counts, each labelled in words. */
export function CountsLine({ counts }: { counts: Counts }) {
  return (
    <span className="counts">
      <span className="count count-learning">{counts.learning} learning</span>
      <span className="count count-review">{counts.review} review</span>
      <span className="count count-new">{counts.new} new</span>
    </span>
  )
}

export function totalDue(c: Counts): number {
  return c.learning + c.review + c.new
}
