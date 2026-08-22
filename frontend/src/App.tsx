import {useEffect, useState} from 'react';
import {AnimatePresence, motion} from 'motion/react';
import {CheckCircle2, KeyRound, LoaderCircle, X} from 'lucide-react';
import {TopBar} from '@/components/TopBar';
import {GlobalPlayer} from '@/components/GlobalPlayer';
import {APIError, apiReadMode, getSystem, setAuthToken} from '@/api';
import {HistoryPage} from '@/pages/HistoryPage';
import {JobsPage} from '@/pages/JobsPage';
import {LibraryPage} from '@/pages/LibraryPage';
import {ReviewPage} from '@/pages/ReviewPage';
import {SettingsPage} from '@/pages/SettingsPage';
import type {PageID, Track} from '@/types';

export function App() {
  const [page, setPage] = useState<PageID>('library');
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [reviewJobId, setReviewJobId] = useState<string>();
  const [dark, setDark] = useState(() => localStorage.getItem('tagger-theme') === 'dark');
  const [notice, setNotice] = useState<string | null>(null);
  const [playerTrack, setPlayerTrack] = useState<Track | null>(null);
  const [playerPlaying, setPlayerPlaying] = useState(false);
  const [showGeneratedCovers, setShowGeneratedCovers] = useState(() => localStorage.getItem('tagger-generated-covers') === 'true');
  const [authState, setAuthState] = useState<'checking' | 'ready' | 'required'>(apiReadMode === 'mock' ? 'ready' : 'checking');
  const [authError, setAuthError] = useState('');

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    localStorage.setItem('tagger-theme', dark ? 'dark' : 'light');
  }, [dark]);

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

  const openReview = (ids: string[]) => {
    setBatchIds(ids);
    setReviewJobId(undefined);
    setPage('review');
  };

  const openReviewJob = (jobId: string) => {
    setBatchIds([]);
    setReviewJobId(jobId);
    setPage('review');
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
    <div className={`app-shell${playerTrack ? ' has-player' : ''}`}>
      <TopBar page={page} onNavigate={setPage} dark={dark} onToggleTheme={() => setDark((value) => !value)} />
      <GlobalPlayer track={playerTrack} playing={playerPlaying} onPlayingChange={setPlayerPlaying} onClose={() => { setPlayerPlaying(false); setPlayerTrack(null); }} />
      <main className="app-main">
        <AnimatePresence mode="wait">
          <motion.div
            key={page}
            className="page-motion"
            initial={{opacity: 0, y: 8}}
            animate={{opacity: 1, y: 0}}
            exit={{opacity: 0, y: -5}}
            transition={{duration: 0.22, ease: [0.22, 1, 0.36, 1]}}
          >
            {page === 'library' && <LibraryPage onOpenReview={openReview} onNotice={setNotice} playerTrackId={playerTrack?.id} playerPlaying={playerPlaying} onPlayTrack={playTrack} onTogglePlayer={() => setPlayerPlaying((value) => !value)} showGeneratedCovers={showGeneratedCovers} />}
            {page === 'review' && (
              <ReviewPage
                trackIds={batchIds}
                matchJobId={reviewJobId}
                showGeneratedCovers={showGeneratedCovers}
                onBack={() => setPage('library')}
                onComplete={() => {
                  setNotice('批量写入任务已创建，正在等待安全写入');
                  setPage('jobs');
                }}
              />
            )}
            {page === 'jobs' && <JobsPage onOpenReview={openReviewJob} />}
            {page === 'history' && <HistoryPage onNotice={setNotice} showGeneratedCovers={showGeneratedCovers} />}
            {page === 'settings' && <SettingsPage onNotice={setNotice} showGeneratedCovers={showGeneratedCovers} onShowGeneratedCoversChange={setShowGeneratedCovers} />}
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
