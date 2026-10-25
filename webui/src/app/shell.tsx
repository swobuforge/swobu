import { Link, Outlet, useLocation } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { getNodeOverview, listConnections, listWorkspaces, queryKeys } from '../api/resources';
import { ResourcePage } from './resource-pages';
import { useEffect, useRef, useState } from 'react';
import { globalPages, pageContext, paths } from './routes';

const globalPaths = { Workspaces: paths.workspaces, Connections: paths.connections, Access: paths.principals, Activity: paths.activity, Settings: paths.history } as const;
function Icon({ name }: { name: string }) {
  const shapes = {
    Workspaces: <><rect x="3" y="3" width="7" height="7" /><rect x="14" y="3" width="7" height="7" /><rect x="3" y="14" width="7" height="7" /><rect x="14" y="14" width="7" height="7" /></>,
    Connections: <><circle cx="5" cy="12" r="2.5" /><circle cx="19" cy="5" r="2.5" /><circle cx="19" cy="19" r="2.5" /><path d="M7.4 10.8 16.5 6.2M7.4 13.2l9.1 4.6" /></>,
    Access: <><circle cx="9" cy="8" r="3" /><path d="M3 21v-2a6 6 0 0 1 12 0v2M17 9a3 3 0 1 0-1.8-5.4M19 21v-3a5 5 0 0 0-3-4.6" /></>,
    Activity: <path d="M3 12h4l3-7 4 14 3-7h4" />,
    Settings: <><circle cx="12" cy="12" r="3" /><path d="M12 2v3m0 14v3M4.9 4.9 7 7m10 10 2.1 2.1M2 12h3m14 0h3M4.9 19.1 7 17M17 7l2.1-2.1" /></>,
    Search: <><circle cx="10.5" cy="10.5" r="6.5" /><path d="m15.5 15.5 5 5" /></>,
  };
  return <svg aria-hidden="true" viewBox="0 0 24 24">{shapes[name as keyof typeof shapes]}</svg>;
}

/** B01 owns navigation and transient finder state, never daemon resource truth. */
export function Shell() {
  const pathname = useLocation({ select: location => location.pathname });
  const { global } = pageContext(pathname);
  const dialog = useRef<HTMLDialogElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState('');
  const [finderOpen, setFinderOpen] = useState(false);
  const node = useQuery({ queryKey: queryKeys.node, queryFn: getNodeOverview, retry: false });
  const workspaces = useQuery({ queryKey: queryKeys.workspaces, queryFn: listWorkspaces, enabled: finderOpen });
  const connections = useQuery({ queryKey: queryKeys.connections, queryFn: listConnections, enabled: finderOpen });
  function openFinder() {
    setFinderOpen(true);
    dialog.current?.showModal();
    searchInput.current?.focus();
  }
  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault(); openFinder();
      }
    }
    document.addEventListener('keydown', shortcut);
    return () => document.removeEventListener('keydown', shortcut);
  }, []);
  return <>
    <a className="skip" href="#main">Skip to content</a>
    <div className="shell">
      <aside className="rail">
        <div className="brand"><img src="/swobu.png" alt="" /><b>swobu</b></div>
        <nav className="nav" aria-label="Primary">
          {globalPages.map(page => <Link key={page} to={globalPaths[page]}
            aria-current={global === page ? 'page' : undefined}><Icon name={page} />{page}</Link>)}
        </nav>
        <div className="rail-foot">Local administration<br />{node.data ? `${node.data.workspace_count} Workspaces · ${node.data.connection_count} Connections` : node.isError ? 'Node unavailable' : 'Connecting to Node…'}</div>
      </aside>
      <div className="app">
        <header className="top"><div /><button className="findbtn" onClick={openFinder} aria-label="Find anything">
          <span className="searchlabel"><Icon name="Search" /><span>Find anything</span></span><kbd>Ctrl K</kbd>
        </button></header>
        <main id="main" className="content" tabIndex={-1}><Outlet /></main>
      </div>
    </div>
    <dialog ref={dialog} aria-labelledby="finder-title" onClose={() => { setSearch(''); setFinderOpen(false); }}>
      <div className="modalhead"><h2 id="finder-title">Find anything</h2><button aria-label="Close finder" onClick={() => dialog.current?.close()}>×</button></div>
      <div className="modalbody"><label htmlFor="find">Find a page or resource</label><input ref={searchInput} id="find" value={search} onChange={event => setSearch(event.target.value)} />
        <nav aria-label="Search results">
          {globalPages.filter(page => page.toLowerCase().includes(search.toLowerCase())).map(page =>
            <Link key={page} to={globalPaths[page]} onClick={() => dialog.current?.close()}>{page}</Link>)}
          {(workspaces.data ?? []).filter(w => (w.display_name + ' ' + w.slug).toLowerCase().includes(search.toLowerCase())).slice(0, 6).map(w =>
            <a key={`w:${w.slug}`} href={`/workspaces/${encodeURIComponent(w.slug)}/overview`} onClick={() => dialog.current?.close()}>{w.display_name || w.slug} <span className="small muted">· Workspace</span></a>)}
          {(connections.data ?? []).filter(c => c.display_name.toLowerCase().includes(search.toLowerCase())).slice(0, 6).map(c =>
            <a key={`c:${c.id}`} href={`/connections/${encodeURIComponent(c.id)}`} onClick={() => dialog.current?.close()}>{c.display_name} <span className="small muted">· Connection</span></a>)}
        </nav>
      </div>
    </dialog>
  </>;
}

export function RouteContent() {
  const pathname = useLocation({ select: location => location.pathname });
  return <ResourcePage pathname={pathname} />;
}

export function NotFound() {
  return <><header className="head"><h1>Page not found</h1></header><p>This address does not identify an Operator page.</p><Link className="textlink" to="/workspaces">Open Workspaces</Link></>;
}
