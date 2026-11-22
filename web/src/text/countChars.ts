/** Most code points allowed on one side of a card (matches the server). */
export const MAX_SIDE = 6000

/** Length in Unicode code points, as the server counts it. */
export function countChars(s: string): number {
  return [...s].length
}
