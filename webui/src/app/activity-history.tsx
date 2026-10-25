import { useMutation, useQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { downloadActivity, type ActivityDownload, type ActivityExportFormat, type ActivityFilter, type ActivityKind } from '../api/activation';
import { listPrincipals } from '../api/authority';
import { listWorkspaces } from '../api/resources';
import { ControlRequestError } from '../api/transport';
import { activitySearch, paths } from './routes';
import { errorMessage } from './control-ui';

export function ActivityFilters({ filter, change, live = true }: { filter: ActivityFilter; change: (update: Partial<ActivityFilter>) => void; live?: boolean }) {
  const workspaces = useQuery({ queryKey: ['activity-workspaces'], queryFn: listWorkspaces, retry: false });
  return <>
    <div className="filterbar"><label>Workspace<select value={filter.workspace} onChange={event => change({ workspace: event.target.value })}>
      <option value="">All Workspaces</option>{workspaces.data?.map(w => <option value={w.slug} key={w.slug}>{w.display_name || w.slug}</option>)}</select></label>
      <label>Evidence<select value={filter.kind} onChange={event => change({ kind: event.target.value as ActivityKind })}>
        {live && <option value="all">Recent / live snapshot</option>}<option value="exchange">Retained Exchanges</option><option value="mcp_operation">Retained MCP operations</option></select></label></div>
    {workspaces.isError && <p className="fieldhint">Workspace filter unavailable: {errorMessage(workspaces.error)}. The current scope can still be read.</p>}
    {filter.kind === 'exchange' && <ExchangeFilters key={JSON.stringify(filter)} filter={filter} change={change}/>}
  </>;
}

// Edits are local until Apply; only committed URL filters select Ledger rows.
function ExchangeFilters({ filter, change }: { filter: ActivityFilter; change: (update: Partial<ActivityFilter>) => void }) {
  const principals = useQuery({ queryKey: ['principals'], queryFn: listPrincipals, retry: false });
  const [from, setFrom] = useState(filter.from || '');
  const [to, setTo] = useState(filter.to || '');
  const [principal, setPrincipal] = useState(filter.principal || '');
  const [result, setResult] = useState(filter.result || '');
  return <form onSubmit={event => { event.preventDefault(); change({ from: from || undefined, to: to || undefined, principal: principal || undefined, result: result || undefined }); }}>
    <div className="filterbar">
      <label>Settled from (inclusive)<input type="text" value={from} placeholder="2026-10-20T12:00:00Z" onChange={event => setFrom(event.target.value)}/></label>
      <label>Settled to (exclusive)<input type="text" value={to} placeholder="2026-10-21T12:00:00Z" onChange={event => setTo(event.target.value)}/></label>
      <label>Principal<input list="activity-principals" value={principal} placeholder="All Principals" onChange={event => setPrincipal(event.target.value)}/>
        <datalist id="activity-principals"><option value="local">Local host</option>{principals.data?.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}</datalist></label>
      <label>Result<select value={result} onChange={event => setResult(event.target.value)}>
        <option value="">All results</option><option value="succeeded">Succeeded</option><option value="exchange_failed">Exchange failed</option>
        <option value="client_cancelled">Client cancelled</option><option value="client_write_failed">Client write failed</option>
        <option value="provider_stream_failed">Provider stream failed</option><option value="checkpoint_commit_failed">Checkpoint commit failed</option>
      </select></label>
    </div>
    <p className="fieldhint">Settlement times use RFC3339 with a timezone, such as 2026-10-20T12:00:00Z. Filters apply together to retained Exchanges.</p>
    {principals.isError && <p className="fieldhint">Principal suggestions unavailable. Enter an exact retained access ID or local.</p>}
    <div className="actions activity-filter-actions"><button className="btn" type="submit">Apply filters</button>
      <button className="btn" type="button" onClick={() => change({ from: undefined, to: undefined, principal: undefined, result: undefined })}>Clear Exchange filters</button></div>
  </form>;
}

export function ActivityExport({ filter }: { filter: ActivityFilter }) {
  const [format, setFormat] = useState<ActivityExportFormat>('ndjson');
  const [rows, setRows] = useState('500');
  const [saved, setSaved] = useState<Pick<ActivityDownload, 'rows' | 'next'>>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => { request.current?.abort(); }, []);
  const download = useMutation({
    retry: false,
    gcTime: 0,
    mutationFn: async (cursor?: string) => {
      request.current = new AbortController();
      const file = await downloadActivity(filter, format, Number(rows), cursor, request.current.signal);
      const href = URL.createObjectURL(file.blob);
      try {
        const anchor = document.createElement('a');
        anchor.href = href; anchor.download = file.filename; anchor.click();
      } finally { URL.revokeObjectURL(href); }
      // MutationCache retains successful results independently of local state.
      // Return only continuation metadata so earlier Blob bodies are released.
      return { rows: file.rows, next: file.next };
    },
    onSuccess: setSaved,
  });
  const reset = () => { setSaved(undefined); download.reset(); };
  const validRows = /^[1-9][0-9]*$/.test(rows) && Number(rows) <= 2000;
  const failure = download.error instanceof ControlRequestError ?
    download.error.status === 413 ? 'Reduce rows per file and try again.' :
      download.error.status === 503 ? 'Recorded history is unavailable or the export is busy. No file was saved; try this page again.' : errorMessage(download.error) : errorMessage(download.error);
  return <section className="panel section" aria-label="Download recorded history"><div className="panelhead"><h2>Download recorded history</h2></div>
    <form className="panelbody" onSubmit={event => { event.preventDefault(); if (validRows && !download.isPending) download.mutate(undefined); }}>
      <p className="fieldhint">Content-free retained {filter.kind === 'exchange' ? 'Exchange' : 'MCP'} evidence for {filter.workspace || 'all Workspaces'}. Each file is an observed page, not a complete audit or an atomic backup. Missing records do not prove that nothing executed.</p>
      <div className="filterbar"><label>File format<select disabled={download.isPending} value={format} onChange={event => { setFormat(event.target.value as ActivityExportFormat); reset(); }}>
        <option value="ndjson">NDJSON</option><option value="csv">CSV</option></select></label>
        <label>Rows per file<input type="number" required min="1" max="2000" step="1" disabled={download.isPending} value={rows} onChange={event => { setRows(event.target.value); reset(); }}/></label></div>
      <div className="actions"><button className="btn primary" type="submit" disabled={!validRows || download.isPending}>{download.isPending ? 'Preparing download…' : 'Download first page'}</button>
        {saved?.next && <button className="btn" type="button" disabled={!validRows || download.isPending} onClick={() => download.mutate(saved.next)}>Download next page</button>}</div>
      <p className="fieldhint" role="status">{saved && `Downloaded ${saved.rows} retained ${saved.rows === 1 ? 'record' : 'records'} in the last file. ${saved.next ? 'More recorded evidence is available.' : 'End of the observed retained slice; capture may be incomplete.'}`}</p>
      {download.isError && <p className="formerror" role="alert">{failure}</p>}
    </form>
  </section>;
}

export function HistoryExports() {
  const search = useSearch({ from: paths.history });
  const navigate = useNavigate();
  const filter: ActivityFilter = { ...search, workspace: search.workspace || '', kind: search.kind === 'mcp_operation' ? 'mcp_operation' : 'exchange' };
  const change = (update: Partial<ActivityFilter>) => void navigate({ to: paths.history, search: activitySearch({ ...filter, ...update, cursor: undefined }) });
  return <div className="section">
    <p className="fieldhint activity-explainer">Download the Ledger evidence currently retained by this Node. Retention and OpenTelemetry configuration are not editable here.</p>
    <ActivityFilters filter={filter} change={change} live={false}/>
    <ActivityExport key={JSON.stringify(filter)} filter={filter}/>
  </div>;
}
