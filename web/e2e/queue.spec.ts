// Unit tests of the pure queue functions (no browser needed).
import { expect, test } from '@playwright/test';
import {
  append,
  clearUpcoming,
  insertAfterCurrent,
  moveItem,
  nextIndex,
  prevIndex,
  removeAt,
  shuffleAround,
  unshuffle,
  type Entry,
} from '../src/lib/queue';

const entries = (...names: string[]): Entry<string>[] => names.map((track, i) => ({ qid: i + 1, track }));
const names = (items: Entry<string>[]) => items.map((e) => e.track);

test('next and previous follow the repeat mode', () => {
  expect(nextIndex(3, 0, 'off')).toBe(1);
  expect(nextIndex(3, 2, 'off')).toBeNull();
  expect(nextIndex(3, 2, 'all')).toBe(0);
  expect(nextIndex(0, -1, 'all')).toBeNull();
  expect(prevIndex(3, 2, 'off')).toBe(1);
  expect(prevIndex(3, 0, 'off')).toBeNull();
  expect(prevIndex(3, 0, 'all')).toBe(2);
});

test('shuffle keeps the current entry first and unshuffle restores the order', () => {
  const items = entries('a', 'b', 'c', 'd', 'e');
  const s = shuffleAround({ items, index: 2 }, () => 0);
  expect(s.index).toBe(0);
  expect(s.items[0].track).toBe('c');
  expect(names(s.items).sort()).toEqual(['a', 'b', 'c', 'd', 'e']);
  const back = unshuffle(items, s.items[0]);
  expect(names(back.items)).toEqual(['a', 'b', 'c', 'd', 'e']);
  expect(back.index).toBe(2);
});

test('insert, append, remove, move and clear keep the current entry', () => {
  let s = { items: entries('a', 'b', 'c'), index: 1 };
  s = insertAfterCurrent(s, [{ qid: 9, track: 'x' }]);
  expect(names(s.items)).toEqual(['a', 'b', 'x', 'c']);
  s = append(s, [{ qid: 10, track: 'y' }]);
  expect(names(s.items)).toEqual(['a', 'b', 'x', 'c', 'y']);

  const r = removeAt(s, 0);
  expect(names(r.items)).toEqual(['b', 'x', 'c', 'y']);
  expect(r.index).toBe(0);
  expect(r.removedCurrent).toBe(false);
  const rc = removeAt(r, 0);
  expect(rc.removedCurrent).toBe(true);
  expect(rc.items[rc.index].track).toBe('x');

  const m = moveItem({ items: entries('a', 'b', 'c', 'd'), index: 1 }, 3, 0);
  expect(names(m.items)).toEqual(['d', 'a', 'b', 'c']);
  expect(m.items[m.index].track).toBe('b');
  const m2 = moveItem({ items: entries('a', 'b', 'c', 'd'), index: 1 }, 1, 3);
  expect(m2.index).toBe(3);
  expect(m2.items[m2.index].track).toBe('b');

  expect(names(clearUpcoming({ items: entries('a', 'b', 'c'), index: 0 }).items)).toEqual(['a']);
  expect(append({ items: [], index: -1 }, entries('a')).index).toBe(0);
});
