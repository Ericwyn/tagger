import type {PageID} from '@/types';

export interface AppRoute {
  page: PageID;
  batchIds: string[];
  reviewJobId?: string;
}

const pagePaths: Record<Exclude<PageID, 'review'>, string> = {
  library: '/library',
  jobs: '/jobs',
  history: '/history',
  settings: '/settings',
};

const pathPages: Record<string, Exclude<PageID, 'review'>> = {
  '/': 'library',
  '/library': 'library',
  '/jobs': 'jobs',
  '/history': 'history',
  '/settings': 'settings',
};

export function readRoute(pathname = window.location.pathname, search = window.location.search): AppRoute {
  const normalizedPath = pathname.replace(/\/+$/, '') || '/';
  if (normalizedPath === '/review') {
    const params = new URLSearchParams(search);
    const tracks = (params.get('tracks') ?? '')
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean);
    const reviewJobId = params.get('job')?.trim() || undefined;
    return {page: 'review', batchIds: tracks, reviewJobId};
  }
  return {page: pathPages[normalizedPath] ?? 'library', batchIds: []};
}

export function routePath(route: AppRoute): string {
  if (route.page === 'review') {
    const params = new URLSearchParams();
    if (route.reviewJobId) params.set('job', route.reviewJobId);
    if (route.batchIds.length > 0) params.set('tracks', route.batchIds.join(','));
    const query = params.toString();
    return `/review${query ? `?${query}` : ''}`;
  }
  return pagePaths[route.page];
}

export function pageRoute(page: PageID): AppRoute {
  return {page, batchIds: []};
}
