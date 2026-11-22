import { expect, it } from 'vitest'
import { countChars, MAX_SIDE } from './countChars'

it('countChars counts code points', () => {
  expect(countChars('→→')).toBe(2)
  expect(countChars('😀')).toBe(1)
  expect(countChars('')).toBe(0)
  expect(MAX_SIDE).toBe(6000)
})
