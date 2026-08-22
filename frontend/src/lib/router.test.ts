import {describe, expect, it} from 'vitest';
import {pageRoute, readRoute, routePath} from '@/lib/router';

describe('app router', () => {
  it('maps top-level paths and unknown paths to the library', () => {
    expect(readRoute('/')).toEqual({page: 'library', batchIds: []});
    expect(readRoute('/settings')).toEqual({page: 'settings', batchIds: []});
    expect(readRoute('/unknown')).toEqual({page: 'library', batchIds: []});
  });

  it('round-trips review job and track routes', () => {
    const route = readRoute('/review', '?job=job%2F1&tracks=trk-1,trk-2');
    expect(route).toEqual({page: 'review', reviewJobId: 'job/1', batchIds: ['trk-1', 'trk-2']});
    expect(routePath(route)).toBe('/review?job=job%2F1&tracks=trk-1%2Ctrk-2');
  });

  it('creates clean page routes without stale review state', () => {
    expect(routePath(pageRoute('history'))).toBe('/history');
  });
});
