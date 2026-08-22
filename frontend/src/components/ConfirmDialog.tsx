import {useEffect, useRef} from 'react';
import {createPortal} from 'react-dom';
import {AlertTriangle, LoaderCircle, X} from 'lucide-react';

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel?: string;
  busy?: boolean;
  error?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({open, title, description, confirmLabel, cancelLabel = '取消', busy = false, error, onConfirm, onCancel}: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onCancel();
    };
    document.addEventListener('keydown', onKeyDown);
    cancelRef.current?.focus();
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [busy, onCancel, open]);

  if (!open) return null;
  return createPortal(
    <div className="confirm-overlay">
      <button className="confirm-backdrop" type="button" aria-label="关闭确认框" onClick={() => { if (!busy) onCancel(); }} />
      <section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-description">
        <div className="confirm-dialog-icon"><AlertTriangle size={20} /></div>
        <div className="confirm-dialog-copy">
          <div className="confirm-dialog-head"><h2 id="confirm-dialog-title">{title}</h2><button className="icon-button" type="button" title="关闭确认框" onClick={onCancel} disabled={busy}><X size={17} /></button></div>
          <p id="confirm-dialog-description">{description}</p>
          {error && <div className="confirm-dialog-error">{error}</div>}
          <div className="confirm-dialog-actions">
            <button className="secondary-button" type="button" ref={cancelRef} onClick={onCancel} disabled={busy}>{cancelLabel}</button>
            <button className="danger-button" type="button" onClick={onConfirm} disabled={busy}>{busy && <LoaderCircle size={14} className="spin" />}{busy ? '处理中…' : confirmLabel}</button>
          </div>
        </div>
      </section>
    </div>,
    document.body,
  );
}
