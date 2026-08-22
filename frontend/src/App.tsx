import {useEffect, useState} from 'react';
import {AnimatePresence, motion} from 'motion/react';
import {CheckCircle2, X} from 'lucide-react';
import {TopBar} from '@/components/TopBar';
import {HistoryPage} from '@/pages/HistoryPage';
import {JobsPage} from '@/pages/JobsPage';
import {LibraryPage} from '@/pages/LibraryPage';
import {ReviewPage} from '@/pages/ReviewPage';
import {SettingsPage} from '@/pages/SettingsPage';
import type {PageID} from '@/types';

export function App() {
  const [page, setPage] = useState<PageID>('library');
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [dark, setDark] = useState(() => localStorage.getItem('tagger-theme') === 'dark');
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    localStorage.setItem('tagger-theme', dark ? 'dark' : 'light');
  }, [dark]);

  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 3600);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const openReview = (ids: string[]) => {
    setBatchIds(ids);
    setPage('review');
  };

  return (
    <div className="app-shell">
      <TopBar page={page} onNavigate={setPage} dark={dark} onToggleTheme={() => setDark((value) => !value)} />
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
            {page === 'library' && <LibraryPage onOpenReview={openReview} onNotice={setNotice} />}
            {page === 'review' && (
              <ReviewPage
                trackIds={batchIds}
                onBack={() => setPage('library')}
                onComplete={() => {
                  setNotice('批量写入任务已创建，正在等待安全写入');
                  setPage('jobs');
                }}
              />
            )}
            {page === 'jobs' && <JobsPage onOpenReview={() => setPage('review')} />}
            {page === 'history' && <HistoryPage onNotice={setNotice} />}
            {page === 'settings' && <SettingsPage onNotice={setNotice} />}
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
