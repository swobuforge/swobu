import { useMutation, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { useState } from 'react';
import { ControlRequestError } from '../api/transport';
import {
  activityPageSize, applyClient, discoverClients, newIntentKey, previewClient,
  readActivity, readOperation,
  type ActivityExchange, type ActivityFilter, type ActivityKind, type ActivityMCP, type ClientPlan, type OperationDetail,
} from '../api/activation';
import { listPrincipals, readPublication } from '../api/authority';
import { previewModelMatch, type Workspace } from '../api/resources';
import { errorMessage, EmptyState, ErrorState, Header, LoadState } from './control-ui';
import { activitySearch, paths } from './routes';
import { ActivityExport, ActivityFilters } from './activity-history';
import { assessExchangeRecovery } from './activity-recovery';
import { workspaceEndpoints, type WorkspaceEndpoint } from './workspace-endpoints';
import { ManualWorkspaceSetup } from './remote-client-setup';

function formatTime(time: string): string {
  const value = new Date(time);
  return Number.isNaN(value.getTime()) ? 'Timestamp unavailable' : value.toLocaleString();
}
function LinkToActivity({ workspace }: { workspace: string }) {
  return <Link className="textlink" to={paths.activity} search={{ workspace, kind: 'exchange', cursor: undefined }}>Inspect Activity ↗</Link>;
}

// Addresses are public routing metadata. They do not carry Workspace bearer
// credentials, provider secrets, or claims that a remote client is reachable.
function EndpointList({ endpoints, onCopy }: { endpoints: WorkspaceEndpoint[]; onCopy: (endpoint: WorkspaceEndpoint) => void }) {
  return <dl className="metadata">{endpoints.map(endpoint => <div key={endpoint.id} style={{ display: 'contents' }}>
    <dt>{endpoint.label}</dt><dd><div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', minWidth: 0 }}>
      <code style={{ overflowWrap: 'anywhere', flex: '1 1 230px', minWidth: 0 }}>{endpoint.url}</code>
      <button type="button" className="btn" aria-label={`Copy ${endpoint.label} URL`} onClick={() => onCopy(endpoint)}>Copy</button>
    </div></dd></div>)}</dl>;
}

function WorkspaceAddresses({ workspace, localOrigin }: { workspace: Workspace; localOrigin: string }) {
  // Deliberately the same query identity as the publication control below.
  // Publishing/unpublishing invalidates both views without another state owner.
  const publication = useQuery({ queryKey: ['workspace-publications', workspace.slug],
    queryFn: () => readPublication(workspace.slug), retry: false });
  const [copied, setCopied] = useState<{ url: string; ok: boolean } | null>(null);
  const enabled = workspace.enabled_services;
  const local = workspaceEndpoints(localOrigin, workspace.slug, enabled);
  // Failed refreshes retain cached data; only a successful current read can
  // supply remote addresses to either endpoint rows or manual client examples.
  const remote = publication.isSuccess && !publication.isFetching && publication.data.published && publication.data.workspace === workspace.slug
    ? workspaceEndpoints(publication.data.url ?? '', workspace.slug, enabled, true) : [];
  const copy = async (endpoint: WorkspaceEndpoint) => {
    try {
      await navigator.clipboard.writeText(endpoint.url);
      setCopied({ url: endpoint.url, ok: true });
    } catch { setCopied({ url: endpoint.url, ok: false }); }
  };
  return <div className="section" aria-label="Workspace connection addresses">
    <div className="sectionhead"><h2>Client endpoints</h2><span className="small muted">Models · MCP · Search</span></div>
    <p className="fieldhint">These are addresses, not credentials. Use a Principal granted the relevant Workspace Service and issue its Workspace credential from <Link to={paths.workspaceAccess} params={{ slug: workspace.slug }}>Access</Link>.</p>
    {!enabled.length ? <EmptyState title="No Services enabled" detail="Enable a Service before connecting a client."/> : <>
      <div style={{ display: 'grid', gap: 16 }}>
      <div className="panel"><div className="panelbody">
        <h3>On this host</h3>
        <p className="fieldhint">Uses the Node's local listener. Localhost is not automatically reachable from a different machine, WSL instance or container.</p>
        <EndpointList endpoints={local} onCopy={endpoint => void copy(endpoint)}/>
      </div></div>
      <div className="panel"><div className="panelbody">
        <h3>Another device · Swobu relay</h3>
        {publication.isPending ? <p className="fieldhint">Checking Workspace publication…</p> :
          publication.isError ? <div role="alert" className="formerror">Publication status unavailable. No remote address can be confirmed. <button type="button" className="btn" onClick={() => void publication.refetch()}>Check again</button></div> :
          !publication.data?.published ? <p className="fieldhint">This Workspace is not published. Use “Publish via relay” below to make its enabled Services addressable over HTTPS.</p> :
          remote.length ? <><p className="fieldhint">Published Workspace endpoint. Use the same Workspace credential and Service grants as locally. Publication does not guarantee that a particular client can reach the Node.</p>
            <EndpointList endpoints={remote} onCopy={endpoint => void copy(endpoint)}/></> :
          <div role="alert" className="formerror">The Workspace is published, but a valid HTTPS relay address is not available. Inspect publication below; do not use the local address for a remote client.</div>}
      </div></div>
      </div>
      <p className="fieldhint">Use the Workspace root for Codex and Claude Code; use <code>/v1</code> for OpenAI-compatible APIs. MCP uses Streamable HTTP; Search uses its direct endpoint. After configuration, run a real request and inspect <LinkToActivity workspace={workspace.slug}/>—copying an address is not proof of successful use.</p>
      {copied && (local.some(item => item.url === copied.url) || remote.some(item => item.url === copied.url)) &&
        <p aria-live="polite" className="fieldhint">{copied.ok ? 'Endpoint URL copied.' : 'Clipboard access failed. Select the address to copy it manually.'}</p>}
      <ManualWorkspaceSetup workspace={workspace} local={local} remote={remote}/>
    </>}
  </div>;
}

/** Connect configures local applications only. Remote clients use the
 * published Workspace addresses below and an independently issued credential. */
export function WorkspaceConnect({ workspace }: { workspace: Workspace }) {
  const clients = useQuery({ queryKey: ['local-clients', workspace.slug], queryFn: () => discoverClients(workspace.slug), retry: false });
  const [selected, setSelected] = useState('');
  const [principal, setPrincipal] = useState('');
  const [modelID, setModelID] = useState('');
  const matchedModel = useQuery({ queryKey: ['connect-model', workspace.slug, modelID],
    queryFn: () => previewModelMatch(workspace.slug, modelID), enabled: !!modelID, retry: false });
  const exactModels = workspace.routes.map(route => route.name).filter(name => !/[*?[\]\\]/.test(name));
  const principals = useQuery({ queryKey: ['connect-principals', workspace.slug], queryFn: listPrincipals, retry: false });
  const modelsEnabled = workspace.enabled_services.includes('models');
  const eligible = principals.data?.filter(p => p.status === 'active' && p.permissions.some(g => g.workspace === workspace.slug && g.action === 'use' && (!g.services || g.services.includes('models')) && (!g.expires_at || Date.parse(g.expires_at) > Date.now()))) ?? [];
  const [review, setReview] = useState<ClientPlan | null>(null);
  const [intent, setIntent] = useState(newIntentKey);
  const preview = useMutation({ mutationFn: (client: string) => previewClient(workspace.slug, client, principal, modelID), onSuccess: plan => {
    setReview(plan);
    setIntent(newIntentKey());
  } });
  const apply = useMutation({ mutationFn: () => applyClient(review!, intent) });
  const busy = preview.isPending || apply.isPending;
  const origin = typeof location === 'undefined' ? '' : location.origin;
  const resetReview = () => { setReview(null); preview.reset(); apply.reset(); };
  const unresolved = apply.isError && !(apply.error instanceof ControlRequestError && apply.error.status === 412);

  return <>
    <div className="sectionhead"><h2>Connect an application</h2><span className="small muted">On this host · reviewed local file changes</span></div>
    <div className="panel"><div className="panelbody connect-flow">
      <p className="fieldhint">Swobu checks applications actually installed on this Node. A review shows the exact changes to their local configuration; nothing is written until you approve.</p>
      {clients.isPending ? <LoadState/> : clients.isError ? <ErrorState error={clients.error} reload={() => void clients.refetch()}/> :
        clients.data.length ? <label>Installed application
          <select value={selected} disabled={busy || !!apply.data || unresolved} onChange={event => { setSelected(event.target.value); resetReview(); }}>
            <option value="">Choose an installed client…</option>
            {clients.data.map(client => <option key={client.id} value={client.id}>{client.name}</option>)}
          </select></label> : <EmptyState title="No supported local applications detected" detail="You can still connect another device or application manually using the Workspace addresses below."/>}
      {principals.isPending ? <LoadState/> : principals.isError ? <ErrorState error={principals.error} reload={() => void principals.refetch()}/> :
        eligible.length ? <label>Principal
          <select value={principal} disabled={busy || !!apply.data || unresolved} onChange={event => { setPrincipal(event.target.value); resetReview(); }}>
            <option value="">Choose an existing Principal…</option>
            {eligible.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}
          </select></label> : <p className="fieldhint">Grant an existing Principal Models access to this Workspace before connecting. <Link to={paths.workspaceAccess} params={{ slug: workspace.slug }}>Manage Workspace access</Link></p>}
      <label>Model ID
        <input type="text" list="connect-model-ids" value={modelID} disabled={busy || !!apply.data || unresolved} placeholder="e.g. coding or claude-sonnet-4" onChange={event => { setModelID(event.target.value); resetReview(); }}/> 
        <datalist id="connect-model-ids">{exactModels.map(id => <option key={id} value={id}/>)}</datalist>
      </label>
      {modelID && (matchedModel.isPending ? <p className="fieldhint">Checking model route…</p> : matchedModel.isError ? <p className="formerror">Cannot validate model route.</p> : !matchedModel.data?.matched ? <p className="formerror">No route matches this model ID.</p> : <p className="fieldhint">Matched route: <code>{matchedModel.data.pattern}</code></p>)}
      {!modelsEnabled && <p className="fieldhint">Enable this Workspace’s Models service before connecting an application.</p>}
      {!review && modelsEnabled && selected && principal && modelID && matchedModel.data?.matched && <div className="actions"><button className="btn primary" disabled={busy} type="button" onClick={() => preview.mutate(selected)}>
        {preview.isPending ? 'Inspecting…' : 'Review configuration'}</button></div>}
      {preview.isError && <div className="formerror" role="alert">{errorMessage(preview.error)}<div className="actions"><button className="btn" type="button" onClick={() => { preview.reset(); void clients.refetch(); }}>Check installation again</button></div></div>}
      {review && <section className="connect-review" role="group" aria-label="Local configuration review">
        <div className="sectionhead"><h2>Configuration review · {review.client.name}</h2></div>
        <dl className="metadata"><dt>Workspace</dt><dd>{review.workspace}</dd><dt>Principal</dt><dd className="mono">{review.authentication.principal}</dd><dt>Model ID</dt><dd className="mono">{review.model_id}</dd><dt>Authentication</dt><dd>{review.authentication.issue_credential ? 'Issue one Workspace credential on approval and install it in this application' : 'Reuse the configured Workspace credential'}</dd><dt>Local file(s)</dt><dd className="mono">{review.config_paths.map(path => <div key={path}>{path}</div>)}</dd>
          <dt>Changes</dt><dd>{review.changes.length}</dd><dt>Outcome if approved</dt><dd>{review.already_configured ? 'Already configured; no write needed' : review.requires_replace ? 'Replace reviewed fields in existing configuration' : 'Configure missing fields'}</dd></dl>
        {review.changes.length > 0 && <div className="connect-changes" aria-label="Exact local file changes">{review.changes.map((change, i) => <div className="connect-change" key={`${change.field}:${i}`}>
          <strong className="mono">{change.field}</strong>
          <div className="connect-diff"><div><span className="small muted">Before</span><pre>{change.before_exists ? change.before : '(not set)'}</pre></div>
            <div><span className="small muted">After</span><pre>{change.after}</pre></div></div>
        </div>)}</div>}
        <p className="fieldhint">Credential values are hidden. This review stays in this browser session and expires when the configuration or reviewed permission changes.</p>
        {review.post_apply_hint && <p className="fieldhint">{review.post_apply_hint}</p>}
        {apply.data || review.already_configured ? <div className="callout">
          <strong>{apply.data ? 'Client configuration observed.' : 'Already configured at review time.'}</strong>
          {apply.data && (apply.data.replayed ? ' This acknowledges an earlier completed attempt, not the current file state.' : ' Swobu read the configuration back and confirmed that its credential is accepted for this Workspace’s Models service.')}
          <p>To verify actual use, run this application and inspect the resulting Exchange in Activity. No request or provider charge was generated by Connect.</p>
          <div className="actions"><LinkToActivity workspace={workspace.slug}/><button type="button" className="btn" onClick={() => { setSelected(''); resetReview(); }}>Configure another application</button></div>
        </div> : <div className="actions"><button type="button" className="btn" disabled={busy || unresolved} onClick={resetReview}>Change selection</button>
          <button type="button" className="btn primary" disabled={busy || apply.isError} onClick={() => apply.mutate()}>
            {apply.isPending ? 'Applying reviewed change…' : 'Apply reviewed configuration'}</button></div>}
        {apply.isError && <div role="alert" className="formerror"><strong>{unresolved ? "Apply was not acknowledged." : "Review is stale."}</strong> {errorMessage(apply.error)}
          <p>{unresolved ? 'Do not issue a new intent or blindly retry. Inspect the local client configuration and original attempt before recovery.' : 'The review is stale. Obtain a fresh review before applying anything.'}</p>
          {unresolved && <p className="mono">Original attempt identity: {intent}</p>}
          {!unresolved && <button type="button" className="btn" onClick={resetReview}>Discard stale review</button>}
        </div>}
      </section>}
    </div></div>
    <WorkspaceAddresses workspace={workspace} localOrigin={origin}/>
  </>;
}

function Outcome({ value }: { value: string }) {
  const state = value.toLowerCase();
  const kind = state.includes('succeed') || state === 'occurred' ? 'good' : state.includes('fail') || state === 'rejected' ? 'bad' : 'neutral';
  return <span className={`activity-outcome ${kind}`}>{value.replaceAll('_', ' ')}</span>;
}
function ActivityExchangeRow({ row, filter }: { row: ActivityExchange; filter: ActivityFilter }) {
  return <Link className="activity-row" to={paths.exchange} params={{ operationId: row.exchange_id }} search={activitySearch(filter)}>
    <div className="mono">{formatTime(row.started_at)}<div className="small muted">{row.exchange_id}</div></div>
    <div><strong>{row.workspace} · {row.route || row.operation || 'Models'}</strong>
      <div className="small muted">{row.provider || 'Upstream not recorded'} {row.model && `· ${row.model}`}</div></div>
    <div><Outcome value={row.state}/><div className="small muted">{row.attempt_count} attempt{row.attempt_count === 1 ? '' : 's'}{row.duration_ms == null ? '' : ` · ${row.duration_ms} ms`}</div></div>
    <span className="textlink">Inspect ↗</span>
  </Link>;
}
function ActivityMCPRow({ row, filter }: { row: ActivityMCP; filter: ActivityFilter }) {
  return <Link className="activity-row" to={paths.exchange} params={{ operationId: row.operation_id }} search={activitySearch(filter)}>
    <div className="mono">{formatTime(row.started_at)}<div className="small muted">{row.operation_id}</div></div>
    <div><strong>{row.workspace} · {row.source}.{row.tool}</strong><div className="small muted">Direct MCP operation</div></div>
    <div><Outcome value={row.outcome || 'In progress / uncertain'}/><div className="small muted">{row.execution_possibility || 'Not settled'}</div></div>
    <span className="textlink">Inspect ↗</span>
  </Link>;
}

export function ActivityPage() {
  const search = useSearch({ from: paths.activity });
  const navigate = useNavigate();
  const filter: ActivityFilter = { ...search, workspace: search.workspace || '', kind: search.kind as ActivityKind || 'all', cursor: search.cursor };
  const evidence = useQuery({ queryKey: ['activity', filter], queryFn: () => readActivity(filter), retry: false });
  const change = (update: Partial<ActivityFilter>) => void navigate({ to: paths.activity, search: activitySearch({ ...filter, ...update, cursor: update.cursor }) });
  const exchanges = evidence.data?.exchanges ?? [];
  const mcp = evidence.data?.mcp_operations ?? [];
  const rows = filter.kind === 'all' ? [
    ...exchanges.map(row => ({ id: row.exchange_id, at: row.started_at, exchange: row, mcp: null })),
    ...mcp.map(row => ({ id: row.operation_id, at: row.started_at, exchange: null, mcp: row })),
  ].sort((a, b) => b.at.localeCompare(a.at)) : [];
  const next = filter.kind === 'exchange' ? evidence.data?.page?.exchange_next : filter.kind === 'mcp_operation' ? evidence.data?.page?.mcp_next : undefined;
  return <><Header title="Activity" desc="Actual Node execution evidence. No synthetic probes, cost estimates or implied successful client runs."
    actions={<button type="button" className="btn" disabled={evidence.isFetching} onClick={() => void evidence.refetch()}>Refresh evidence</button>}/>
    <ActivityFilters filter={filter} change={change}/>
    {evidence.isPending ? <LoadState/> : evidence.isError ? <ErrorState error={evidence.error} reload={() => void evidence.refetch()}/> : <>
      {evidence.data.history && <div className="activity-health"><span className="state-tag">Ledger: {evidence.data.history.state}</span>
        <span>Queue {evidence.data.history.queue_depth}/{evidence.data.history.queue_capacity}</span>
        {(evidence.data.history.dropped_exchanges_total > 0 || evidence.data.history.dropped_mcp_operations_total > 0) && <strong>Capture gaps: {evidence.data.history.dropped_exchanges_total} Exchanges, {evidence.data.history.dropped_mcp_operations_total} MCP operations dropped</strong>}
      </div>}
      <div className="fieldhint activity-explainer">{filter.kind === 'all' ? 'Bounded current snapshot, including active operations; it is not a full history.' : `Retained ${filter.kind === 'exchange' ? 'Exchange' : 'MCP'} history · up to ${activityPageSize} rows per page.`}
        {filter.workspace && ' Workspace-scoped reads do not certify Node-wide capture health.'} An empty result does not prove that nothing executed.</div>
      {(filter.kind === 'all' ? rows.length : filter.kind === 'exchange' ? exchanges.length : mcp.length) === 0 ?
        <EmptyState title="No evidence in this view" detail="No matching records are currently available. This can also result from retention or capture gaps."/> :
        <div className="activity-list">{filter.kind === 'all' ? rows.map(row => row.exchange ? <ActivityExchangeRow key={row.id} row={row.exchange} filter={filter}/> : <ActivityMCPRow key={row.id} row={row.mcp!} filter={filter}/>) :
          filter.kind === 'exchange' ? exchanges.map(row => <ActivityExchangeRow key={row.exchange_id} row={row} filter={filter}/>) :
          mcp.map(row => <ActivityMCPRow key={row.operation_id} row={row} filter={filter}/>)}</div>}
      {filter.kind !== 'all' && <div className="activity-pager"><span className="small muted">{filter.cursor ? 'Retained history · continued' : 'Most recent retained page'}</span>
        <div className="actions"><button type="button" className="btn" disabled={!filter.cursor} onClick={() => change({})}>Newest</button>
          <button type="button" className="btn" disabled={!next || evidence.isFetching} onClick={() => change({ cursor: next })}>Older →</button></div></div>}
    </>}
    {filter.kind !== 'all' && <ActivityExport key={JSON.stringify(filter)} filter={filter}/>}
  </>;
}

function ExchangeRecovery({ exchange }: { exchange: NonNullable<OperationDetail['exchange']> }) {
  const assessment = assessExchangeRecovery(exchange);
  const facts = exchange.upstream_execution;
  const count = (number: number, label: string) => `${number} ${label}`;
  return <section className="section" aria-label="Execution and delivery reconciliation">
    <div className="sectionhead"><h2>Execution and client delivery</h2></div>
    <dl className="metadata">
      <dt>Source execution</dt><dd><strong>{assessment.title}</strong>
        <div className="fieldhint">{assessment.explanation}</div>
        <div className="small muted">{[
          count(facts.recorded_invocations, 'recorded invocations'),
          count(facts.occurred, 'occurred'), count(facts.may_have_occurred, 'may have occurred'),
          count(facts.did_not_occur, 'did not occur'), count(facts.unsettled, 'unsettled'),
        ].join(' · ')}</div></dd>
      <dt>Client delivery</dt><dd>{exchange.delivery_result ? <Outcome value={exchange.delivery_result}/> : 'Not settled'}
        <div className="small muted">{exchange.http_status ? `Recorded HTTP ${exchange.http_status}` : 'No HTTP response status recorded'}</div>
        {exchange.error_code && <div className="small muted">Canonical error: <code>{exchange.error_code}</code></div>}</dd>
      <dt>Before recovery</dt><dd>{assessment.next}</dd>
    </dl>
  </section>;
}

export function ActivityDetailPage({ id }: { id: string }) {
  const search = useSearch({ from: paths.exchange });
  const detail = useQuery({ queryKey: ['operation-detail', id], queryFn: () => readOperation(id), retry: false });
  return <><div className="crumb"><Link to={paths.activity} search={search}>← Activity</Link></div><Header title={id} desc="Recorded causal evidence · without prompts, tool arguments or model responses"/>
    {detail.isPending ? <LoadState/> : detail.isError ? (detail.error instanceof ControlRequestError && detail.error.status === 404 ?
      <EmptyState title="Evidence unavailable" detail="The operation may have been evicted or never captured. Missing evidence is not proof that it never executed."/> :
      <ErrorState error={detail.error} reload={() => void detail.refetch()}/>) : <>
      <dl className="metadata"><dt>Evidence</dt><dd>{detail.data.evidence}</dd><dt>Kind</dt><dd>{detail.data.kind === 'exchange' ? 'Model Exchange' : 'Direct MCP operation'}</dd>
        <dt>Workspace</dt><dd>{detail.data.exchange?.workspace || detail.data.mcp_operation?.workspace}</dd>
        <dt>Started</dt><dd>{formatTime(detail.data.exchange?.started_at || detail.data.mcp_operation?.started_at || '')}</dd>
        {detail.data.mcp_operation?.outcome && <><dt>MCP outcome</dt><dd><Outcome value={detail.data.mcp_operation.outcome}/></dd></>}
        {detail.data.mcp_operation?.execution_possibility && <><dt>Execution possibility</dt><dd>{detail.data.mcp_operation.execution_possibility}</dd></>}
      </dl>
      {detail.data.exchange && <ExchangeRecovery exchange={detail.data.exchange}/>}
      {detail.data.mcp_operation && <section className="section" aria-label="Direct MCP recovery">
        <div className="sectionhead"><h2>Source and recovery</h2></div>
        <dl className="metadata"><dt>Tool</dt><dd><code>{detail.data.mcp_operation.source}.{detail.data.mcp_operation.tool}</code></dd>
          <dt>Source configuration</dt><dd><Link className="textlink" to={paths.workspaceMCP} params={{ slug: detail.data.mcp_operation.workspace }}>Inspect current MCP Service ↗</Link></dd>
          <dt>Replay boundary</dt><dd>Execution possibility is separate from client delivery. Check external effects before replaying a possibly executed or unsettled call.</dd></dl>
      </section>}
      {detail.data.exchange && <div className="section"><div className="sectionhead"><h2>Upstream attempts and effects</h2></div>
        {detail.data.exchange.invocations.length ? <div className="activity-timeline">{detail.data.exchange.invocations.map(item => <section key={item.invocation_id} className="activity-event">
          <div className="small muted mono">{formatTime(item.started_at)} · {item.invocation_id}</div>
          <b>{item.kind === 'model' ? `${item.provider || 'Provider'} / ${item.model || 'Model'}` : `${item.source || 'MCP'}.${item.tool || 'Tool'}`}</b>
          <div className="small muted">{item.target && `Target ${item.target}${item.target_version ? ` v${item.target_version}` : ''} · `}{item.connection_id && `Connection ${item.connection_id}${item.connection_revision ? ` rev ${item.connection_revision}` : ''} · `}{item.purpose || 'Invocation'}</div>
          <div className="actions">{item.route && <Link className="textlink" to={paths.workspaceModelRoute} params={{ slug: detail.data.exchange!.workspace, route: item.route }}>Inspect current route ↗</Link>}
            {item.kind === 'mcp_tool' && <Link className="textlink" to={paths.workspaceMCP} params={{ slug: detail.data.exchange!.workspace }}>Inspect MCP Service ↗</Link>}
            {item.connection_id && <Link className="textlink" to={paths.connection} params={{ id: item.connection_id }}>Inspect current Connection ↗</Link>}</div>
          <div className="activity-event-state"><Outcome value={item.outcome || 'Not settled'}/>{item.execution_possibility && <span className="small muted">Execution: {item.execution_possibility}</span>}{item.failure && <span className="small muted">Failure: {item.failure}</span>}</div>
        </section>)}</div> : <EmptyState title="No recorded invocations" detail="Activity does not establish whether an unrecorded upstream dispatch occurred."/>}
      </div>}
      <p className="fieldhint section">Recorded outcomes describe observations, not whether retrying an uncertain external effect is safe.</p>
    </>}
  </>;
}
