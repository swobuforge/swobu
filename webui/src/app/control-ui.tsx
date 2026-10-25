import { Link } from '@tanstack/react-router';
import { useEffect, useRef, type ReactNode } from 'react';
import { ControlRequestError } from '../api/transport';

export function errorMessage(error: unknown): string {
  if (error instanceof ControlRequestError) {
    if (error.status === 401 || error.status === 403) return 'The local browser session is unavailable. Reopen the Operator from the Swobu launcher.';
    if (error.status === 412) return 'Configuration changed since this page was loaded. Reload the current state before making a new decision.';
    if (error.status === 409) return error.problem?.detail ?? 'This change conflicts with a current reference or policy.';
    if (error.status === 503) return 'The Node could not confirm the outcome. Inspect current state before submitting a new intent; for a creation, retry the original idempotency key.';
    return error.message;
  }
  return error instanceof Error ? error.message : 'Unable to read the current Node state.';
}
export function ErrorState({ error, reload }: { error: unknown; reload?: () => void }) {
  return <section className="empty" role="alert"><h2>Unable to load the current state</h2><p>{errorMessage(error)}</p>
    {reload && <button className="btn" type="button" onClick={reload}>Reload</button>}</section>;
}
export function LoadState() { return <section className="empty" role="status">Loading current Node state…</section>; }
export function EmptyState({ title, detail }: { title: string; detail: string }) {
  return <div className="empty"><h2>{title}</h2><p>{detail}</p></div>;
}
export function Modal({ id, title, close, children }: { id: string; title: string; close: () => void; children: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    dialog?.showModal();
    // React autofocus runs before showModal, while the controls are hidden.
    // Focus the requested control only after the native focus trap is active.
    dialog?.querySelector<HTMLElement>('[data-autofocus="true"]')?.focus();
    return () => {
      dialog?.close();
      opener?.focus();
    };
  }, []);
  return <dialog ref={ref} className="operator-dialog" aria-labelledby={id} onClose={event => {
    // StrictMode may reopen before a cleanup close event is delivered.
    if (!event.currentTarget.open) close();
  }}>
    <div className="modalhead"><h2 id={id}>{title}</h2><button type="button" aria-label="Close" onClick={() => ref.current?.close()}>×</button></div>
    {children}
  </dialog>;
}
export function Header({ title, desc, back, actions, className = '' }: { className?: string; title: string; desc?: string; back?: { href: string; label: string }; actions?: ReactNode }) {
  return <header className={`head ${className}`}><div>{back && <div className="crumb"><Link to={back.href}>← {back.label}</Link></div>}
    <h1>{title}</h1>{desc && <p className="sub">{desc}</p>}</div>{actions && <div className="actions">{actions}</div>}</header>;
}
