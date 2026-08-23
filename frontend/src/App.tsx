import {useEffect, useState} from 'react';
import {AnimatePresence, motion} from 'motion/react';
import {CheckCircle2, KeyRound, LoaderCircle, X} from 'lucide-react';
import {TopBar} from '@/components/TopBar';
import {APIError, apiReadMode, getSystem, setAuthToken} from '@/api';
import {HistoryPage} from '@/pages/HistoryPage';
import {JobsPage} from '@/pages/JobsPage';
import {LibraryPage} from '@/pages/LibraryPage';
import {ReviewPage} from '@/pages/ReviewPage';
import {SettingsPage} from '@/pages/SettingsPage';
import {pageRoute, readRoute, routePath, type AppRoute} from '@/lib/router';
import {normalizeFont, normalizeTheme, type FontID, type ThemeID} from '@/theme';
import type {PageID, RestoreDraftRequest, Track} from '@/types';

export function App() {
  const [route, setRoute] = useState<AppRoute>(() => readRoute());
  const [theme, setTheme] = useState<ThemeID>(() => normalizeTheme(localStorage.getItem('tagger-theme-v2') ?? localStorage.getItem('tagger-theme')));
  const [font, setFont] = useState<FontID>(() => normalizeFont(localStorage.getItem('tagger-font-v1')));
  const [notice, setNotice] = useState<string | null>(null);
  const [playerTrack, setPlayerTrack] = useState<Track | null>(null);
  const [playerPlaying, setPlayerPlaying] = useState(false);
  const [jobFocusId, setJobFocusId] = useState<string>();
  const [showGeneratedCovers, setShowGeneratedCovers] = useState(() => localStorage.getItem('tagger-generated-covers') === 'true');
  const [authState, setAuthState] = useState<'checking' | 'ready' | 'required'>(apiReadMode === 'mock' ? 'ready' : 'checking');
  const [authError, setAuthError] = useState('');
  const [restoreDraft, setRestoreDraft] = useState<RestoreDraftRequest>();

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem('tagger-theme-v2', theme);
  }, [theme]);

  useEffect(() => {
    document.documentElement.dataset.font = font;
    localStorage.setItem('tagger-font-v1', font);
  }, [font]);

  useEffect(() => {
    const handlePopState = () => setRoute(readRoute());
    window.addEventListener('popstate', handlePopState);
    const current = `${window.location.pathname}${window.location.search}`;
    const canonical = routePath(route);
    if (current !== canonical) window.history.replaceState({}, '', canonical);
    return () => window.removeEventListener('popstate', handlePopState);
    // Route is intentionally captured only on mount; browser navigation owns later updates.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    localStorage.setItem('tagger-generated-covers', String(showGeneratedCovers));
  }, [showGeneratedCovers]);

  useEffect(() => {
    if (apiReadMode === 'mock') return;
    void getSystem().then(() => setAuthState('ready')).catch((error) => {
      setAuthError(error instanceof APIError && error.status === 401 ? '当前服务已启用访问令牌保护' : (error instanceof Error ? error.message : '后台连接失败'));
      setAuthState('required');
    });
  }, []);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 3600);
    return () => window.clearTimeout(timer);
  }, [notice]);

  useEffect(() => {
    if (route.page !== 'jobs') setJobFocusId(undefined);
  }, [route.page]);

  const navigate = (nextRoute: AppRoute) => {
    const nextPath = routePath(nextRoute);
    const currentPath = `${window.location.pathname}${window.location.search}`;
    if (currentPath === nextPath) {
      setRoute(nextRoute);
      return;
    }
    window.history.pushState({}, '', nextPath);
    setRoute(nextRoute);
  };

  const navigatePage = (nextPage: PageID) => navigate(pageRoute(nextPage));

  const openReview = (ids: string[]) => navigate({page: 'review', batchIds: ids});

  const openReviewJob = (jobId: string) => navigate({page: 'review', batchIds: [], reviewJobId: jobId});

  const openJobs = (focusJobId?: string) => {
    setJobFocusId(focusJobId);
    navigatePage('jobs');
  };

  const playTrack = (track: Track) => {
    setPlayerTrack(track);
    setPlayerPlaying(true);
  };

  const authenticate = async (token: string) => {
    const normalized = token.trim();
    if (!normalized) {
      setAuthError('请输入访问令牌');
      return;
    }
    setAuthError('正在验证访问令牌…');
    setAuthToken(normalized);
    try {
      await getSystem();
      setAuthError('');
      setAuthState('ready');
    } catch (error) {
      setAuthError(error instanceof APIError && error.status === 401 ? '访问令牌不正确' : (error instanceof Error ? error.message : '令牌验证失败'));
      setAuthState('required');
    }
  };

  if (authState !== 'ready') {
    return <AuthGate checking={authState === 'checking'} error={authError} onSubmit={authenticate} />;
  }

  return (
    <div className="app-shell">
      <TopBar
        page={route.page}
        onNavigate={navigatePage}
        playerTrack={playerTrack}
        playerPlaying={playerPlaying}
        onPlayerPlayingChange={setPlayerPlaying}
        onPlayerClose={() => { setPlayerPlaying(false); setPlayerTrack(null); }}
      />
      <main className="app-main">
        <AnimatePresence mode="wait">
          <motion.div
            key={route.page}
            className="page-motion"
            initial={{opacity: 0, y: 8}}
            animate={{opacity: 1, y: 0}}
            exit={{opacity: 0, y: -5}}
            transition={{duration: 0.22, ease: [0.22, 1, 0.36, 1]}}
          >
            {route.page === 'library' && <LibraryPage onOpenReview={openReview} onOpenSettings={() => navigatePage('settings')} onNotice={setNotice} playerTrackId={playerTrack?.id} playerPlaying={playerPlaying} onPlayTrack={playTrack} onTogglePlayer={() => setPlayerPlaying((value) => !value)} showGeneratedCovers={showGeneratedCovers} restoreDraft={restoreDraft} onRestoreDraftConsumed={() => setRestoreDraft(undefined)} onDiscardRestoreDraft={() => setRestoreDraft(undefined)} />}
            {route.page === 'review' && (
              <ReviewPage
                trackIds={route.batchIds}
                matchJobId={route.reviewJobId}
                showGeneratedCovers={showGeneratedCovers}
                onBack={() => navigatePage('library')}
                onComplete={() => {
                  setNotice('批量写入任务已创建，正在等待安全写入');
                  openJobs();
                }}
                onJobQueued={(jobId) => openJobs(jobId)}
              />
            )}
            {route.page === 'jobs' && <JobsPage onOpenReview={openReviewJob} focusJobId={jobFocusId} />}
            {route.page === 'history' && <HistoryPage onNotice={setNotice} onLoadSnapshot={(request) => { setRestoreDraft(request); navigatePage('library'); }} showGeneratedCovers={showGeneratedCovers} />}
            {route.page === 'settings' && <SettingsPage onNotice={setNotice} showGeneratedCovers={showGeneratedCovers} onShowGeneratedCoversChange={setShowGeneratedCovers} theme={theme} onThemeChange={setTheme} font={font} onFontChange={setFont} />}
          </motion.div>
        </AnimatePresence>
      </main>

      <AnimatePresence>
        {notice && (
          <motion.div
            className="toast"
            initial={{opacity: 0, y: 18, scale: 0.98}}
            animate={{opacity: 1, y: 0, scale: 1}}
            exit={{opacity: 0, y: 8}}
          >
            <CheckCircle2 size={18} />
            <span>{notice}</span>
            <button title="关闭通知" onClick={() => setNotice(null)}><X size={15} /></button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function AuthGate({checking, error, onSubmit}: {checking: boolean; error: string; onSubmit: (token: string) => Promise<void>}) {
  const [token, setToken] = useState('');
  return (
    <main className="auth-gate">
      <section className="auth-gate-card">
        <div className="auth-gate-icon"><KeyRound size={23} /></div>
        <div className="eyebrow">SINGLE USER ACCESS</div>
        <h1>输入访问令牌</h1>
        <p>此 Tagger 实例启用了单用户鉴权。未配置令牌时服务不会要求登录。</p>
        {checking ? (
          <div className="auth-gate-status"><LoaderCircle className="spin" size={16} /> 正在连接后台…</div>
        ) : (
          <form onSubmit={(event) => { event.preventDefault(); void onSubmit(token); }}>
            <label><span>访问令牌</span><input aria-label="访问令牌" type="password" value={token} onChange={(event) => setToken(event.target.value)} placeholder="TAGGER_AUTH_TOKEN" autoFocus /></label>
            <button className="primary-button" type="submit">验证并进入</button>
          </form>
        )}
        {error && <small className="auth-gate-error">{error}</small>}
      </section>
    </main>
  );
}
