import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { ControlRequestError } from '../api/transport';
import { errorMessage, ErrorState, LoadState, EmptyState, Modal, Header } from './control-ui';
import { ModelRoutesAuthoring, MCPAuthoring, SearchAuthoring } from './service-authoring';
import { ActivityPage, ActivityDetailPage, WorkspaceConnect } from './activation-pages';
import { HistoryExports } from './activity-history';
import { EditConnectionDialog, ReplaceConnectionCredentialDialog, canReplaceConnectionCredential } from './connection-maintenance';
import { CreateConnectionDialog } from './connection-create';
import { readActivity } from '../api/activation';
import { listPrincipals, readPublication } from '../api/authority';
import { NetworkPublicationSummary, PrincipalPage, PrincipalsPage, WorkspaceAccess, WorkspaceLimits, WorkspacePublication } from './authority-pages';
import {
  changeWorkspaceService, checkConnection, createWorkspace, deleteConnection,
  getConnection, getConnectionReferences, getNodeOverview, getWorkspace,
  listConnections, listWorkspaces, listWorkspaceServices, newIntentKey,
  queryKeys, renameWorkspace,
  type Connection, type ConnectionKind, type Workspace, type WorkspaceService,
} from '../api/resources';
import { pageContext, paths, workspaceSections } from './routes';

const labels = { models: 'Models', mcp: 'MCP', search: 'Search' } as const;
function WorkspaceTabs({ slug, current }: { slug: string; current: string }) {
  const routes = { Overview: paths.workspaceOverview, Services: paths.workspaceServices, Access: paths.workspaceAccess, Limits: paths.workspaceLimits, Connect: paths.workspaceConnect } as const;
  return <nav className="tabs" aria-label="Workspace sections">{workspaceSections.map(label => <Link key={label} to={routes[label]}
    params={{ slug }} aria-current={current === label.toLowerCase() ? 'page' : undefined}>{label}</Link>)}</nav>;
}
function WorkspaceHeader({ workspace, section }: { workspace: Workspace; section: string }) {
  const managed = useManagedNode();
  const [editing, setEditing] = useState(false);
  return <><Header title={workspace.display_name || workspace.slug} desc={workspace.display_name ? `Workspace · ${workspace.slug}` : 'Workspace'}
    back={{ href: '/workspaces', label: 'Workspaces' }}
    actions={managed ? <button className="btn" type="button" onClick={() => setEditing(true)}>Rename</button> : undefined}/>
    <WorkspaceTabs slug={workspace.slug} current={section}/>
    {editing && <RenameWorkspaceDialog workspace={workspace} close={() => setEditing(false)}/>}
  </>;
}

function useManagedNode(): boolean {
  const node = useQuery({ queryKey: queryKeys.node, queryFn: getNodeOverview });
  return node.data?.configuration_mode === 'managed';
}
export function WorkspacesPage() {
  const managed = useManagedNode();
  const query = useQuery({ queryKey: queryKeys.workspaces, queryFn: listWorkspaces });
  const [creating, setCreating] = useState(false);
  return <><Header title="Workspaces" desc="AI environments you can use yourself or share with other clients."
    actions={<button className="btn primary" disabled={!managed} title={!managed ? "Node configuration is not writable" : undefined} type="button" onClick={() => setCreating(true)}>New Workspace</button>}/>
    {query.isPending ? <LoadState/> : query.isError ? <ErrorState error={query.error} reload={() => void query.refetch()}/> :
      query.data.length ? <div className="objectlist">{query.data.map(workspace => <Link key={workspace.slug}
        className="objectrow" to={paths.workspaceOverview} params={{ slug: workspace.slug }}>
        <div><strong className="objecttitle">{workspace.display_name || workspace.slug}</strong><div className="small muted mono">{workspace.slug}</div></div>
        <div><b>{workspace.route_count} model route{workspace.route_count === 1 ? '' : 's'}</b></div>
        <div className="small muted">{workspace.route_count ? 'Inspect Models routing' : 'No model routes'}</div>
        <span className="textlink">Open ↗</span></Link>)}</div> :
        <EmptyState title="No Workspaces yet" detail="Create a Workspace to configure client-facing Models, MCP or Search services."/>}
    {creating && <CreateWorkspaceDialog close={() => setCreating(false)}/>}
  </>;
}
function CreateWorkspaceDialog({ close }: { close: () => void }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [slug, setSlug] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [intent, setIntent] = useState(newIntentKey);
  const mutation = useMutation({ mutationFn: () => createWorkspace(slug, displayName, intent), onSuccess: async workspace => {
    await qc.invalidateQueries({ queryKey: queryKeys.workspaces });
    close();
    await navigate({ to: paths.workspaceOverview, params: { slug: workspace.slug } });
  } });
  const cleanSlug = /^[a-z][a-z0-9-]*$/.test(slug);
  return <Modal id="new-workspace-title" title="New Workspace" close={close}>
    <form onSubmit={event => { event.preventDefault(); mutation.mutate(); }}>
      <div className="modalbody formstack">
        <label>Workspace address (permanent)<input autoFocus required pattern="[a-z][a-z0-9-]*" value={slug}
          placeholder="engineering" onChange={event => { setSlug(event.target.value); setIntent(newIntentKey()); mutation.reset(); }}/></label>
        <label>Display name<input value={displayName} maxLength={120} placeholder="Engineering" onChange={event => { setDisplayName(event.target.value); setIntent(newIntentKey()); mutation.reset(); }}/></label>
        <p className="fieldhint">A new Workspace starts empty. Enable services and configure their upstream Connections afterward. The address cannot be renamed.</p>
        {mutation.isError && <p className="formerror" role="alert">{errorMessage(mutation.error)}</p>}
      </div>
      <div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
        <button className="btn primary" disabled={!cleanSlug || mutation.isPending} type="submit">{mutation.isPending ? 'Creating…' : mutation.isError ? 'Retry same creation' : 'Create Workspace'}</button></div>
    </form>
  </Modal>;
}

function WorkspaceOverview({ workspace }: { workspace: Workspace }) {
  const slug = workspace.slug;
  const services = useQuery({ queryKey: queryKeys.services(slug), queryFn: () => listWorkspaceServices(slug) });
  const principals = useQuery({ queryKey: ['principals'], queryFn: listPrincipals, retry: false });
  const publication = useQuery({ queryKey: ['workspace-publications', slug], queryFn: () => readPublication(slug), retry: false });
  const activity = useQuery({ queryKey: ['workspace-overview-activity', slug], queryFn: () => readActivity({ workspace: slug, kind: 'all' }), retry: false });
  // RFC3339 timestamps may use different fractional precision; order instants.
  const latest = [
    ...(activity.data?.exchanges ?? []).map(item => ({ at: item.started_at, label: `Models · ${item.route || item.operation || 'Exchange'} → ${item.provider || 'upstream'}` })),
    ...(activity.data?.mcp_operations ?? []).map(item => ({ at: item.started_at, label: `MCP · ${item.source}.${item.tool}` })),
  ].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))[0];
  const [now] = useState(() => Date.now());
  const granted = principals.data?.filter(principal => principal.status === 'active' && principal.permissions.some(grant =>
    grant.workspace === slug && grant.action === 'use' && (!grant.expires_at || Date.parse(grant.expires_at) > now))).length;
  const routeTargets = workspace.routes.reduce((sum, route) => sum + route.tiers.reduce((n, tier) => n + tier.targets.length, 0), 0);
  const destinations = { models: paths.workspaceModels, mcp: paths.workspaceMCP, search: paths.workspaceSearch } as const;
  const serviceDetails = { models: 'Model APIs and routing', mcp: 'Workspace MCP tool catalog', search: 'Direct Search endpoint' } as const;
  return <>
    <div className="sectionhead"><h2>Services</h2><Link className="textlink" to={paths.workspaceServices} params={{ slug }}>Manage services ↗</Link></div>
    {services.isPending ? <LoadState/> : services.isError ? <ErrorState error={services.error} reload={() => void services.refetch()}/> :
      <div className="serviceboard">{services.data.filter(service => service.enabled).map(service => {
        const mounts = workspace.mcp_mounts?.length ?? 0;
        const summary = service.kind === 'models' ? `${workspace.routes.length} route${workspace.routes.length === 1 ? '' : 's'} · ${routeTargets} target${routeTargets === 1 ? '' : 's'}` :
          service.kind === 'mcp' ? `${mounts} MCP source${mounts === 1 ? '' : 's'}` :
          service.configured ? 'Search source configured' : 'No Search source';
        return <Link className="servicerow" key={service.kind} to={destinations[service.kind]} params={{ slug }}>
          <span className={`sglyph ${service.kind}`}>{service.kind === 'models' ? 'M' : service.kind === 'mcp' ? '↯' : 'S'}</span>
          <div><b>{labels[service.kind]}</b><div className="small muted">{serviceDetails[service.kind]}</div></div>
          <div><b>{summary}</b><div className="small muted">{service.configured ? 'Configured' : 'Needs configuration'}</div></div>
          <span className="textlink">Open ↗</span>
        </Link>;
      })}{services.data.every(service => !service.enabled) && <EmptyState title="No services enabled" detail="Enable Models, MCP or Search before configuring routes and mounts."/>}</div>}
    <div className="workspace-recent" aria-label="Recent Workspace activity">
      <div><span className="small muted">Recent recorded activity</span>{activity.isPending ? <span className="small muted"> · Loading…</span> :
        activity.isError ? <span className="small muted"> · Unavailable</span> : latest ? <>
          <b> {latest.label}</b><span className="small muted"> · {new Date(latest.at).toLocaleString()}</span></> :
          <span className="small muted"> · No operations in the current snapshot</span>}</div>
      <Link className="textlink" to={paths.activity} search={{ workspace: slug, kind: 'all', cursor: undefined }}>Inspect Activity ↗</Link>
    </div>
    <div className="sectionhead section"><h2>Workspace policy</h2><span className="small muted">Access and reachability</span></div>
    <div className="panel"><div className="panelbody">
      <div className="workspace-policy-metrics">
        <Link to={paths.workspaceAccess} params={{ slug }} className="workspace-policy-metric"><strong>{principals.isPending ? '…' : principals.isError ? '—' : granted}</strong><span>Granted Principals</span></Link>
        <Link to={paths.workspaceServices} params={{ slug }} className="workspace-policy-metric"><strong>{workspace.enabled_services.length}</strong><span>Enabled Services</span></Link>
      </div>
      <div className="workspace-reachability"><div><b>Client reachability: </b>
        {publication.isPending ? 'Checking…' : publication.isError ? 'Unavailable' : publication.data?.published ?
          'Published via Swobu relay' : 'Local only'}
        {publication.isSuccess && publication.data.published && <span className="small"> · Availability not verified</span>}
      </div><Link className="textlink" to={paths.workspaceConnect} params={{ slug }}>Manage in Connect ↗</Link></div>
      {principals.isError && <p className="fieldhint">Cannot load Workspace access summary. <Link to={paths.workspaceAccess} params={{ slug }}>Inspect access</Link>.</p>}
      {publication.isError && <p className="fieldhint">Cannot load publication status. <Link to={paths.workspaceConnect} params={{ slug }}>Inspect publication</Link>.</p>}
    </div></div>
  </>;
}

function RenameWorkspaceDialog({ workspace, close }: { workspace: Workspace; close: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = useState(workspace.display_name);
  const mutation = useMutation({ mutationFn: () => renameWorkspace(workspace, name), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: queryKeys.workspaces }), qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace.slug) })]);
    close();
  } });
  return <Modal id="rename-workspace-title" title="Display name" close={close}>
    <form onSubmit={event => { event.preventDefault(); mutation.mutate(); }}>
      <div className="modalbody formstack"><label>Display name<input autoFocus value={name} maxLength={120} onChange={event => setName(event.target.value)}/></label>
        <p className="fieldhint">This does not change the Workspace address or client URLs.</p>
        {mutation.isError && <p className="formerror" role="alert">{errorMessage(mutation.error)}</p>}</div>
      <div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button><button className="btn primary" type="submit" disabled={mutation.isPending || name === workspace.display_name}>Save</button></div>
    </form>
  </Modal>;
}
function ServicesPage({ workspace }: { workspace: Workspace }) {
  const qc = useQueryClient();
  const managed = useManagedNode();
  const services = useQuery({ queryKey: queryKeys.services(workspace.slug), queryFn: () => listWorkspaceServices(workspace.slug) });
  const [reviewing, setReviewing] = useState<WorkspaceService | null>(null);
  const mutation = useMutation({ mutationFn: ({ service, enabled }: { service: WorkspaceService; enabled: boolean }) => changeWorkspaceService(workspace, service, enabled),
    onSuccess: async () => {
      setReviewing(null);
      await Promise.all([qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace.slug) }), qc.invalidateQueries({ queryKey: queryKeys.services(workspace.slug) }), qc.invalidateQueries({ queryKey: queryKeys.workspaces })]);
    } });
  const refresh = () => {
    mutation.reset();
    setReviewing(null);
    void Promise.all([qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace.slug) }), qc.invalidateQueries({ queryKey: queryKeys.services(workspace.slug) })]);
  };
  const change = (service: WorkspaceService, enabled: boolean) => {
    mutation.reset();
    if (enabled && service.activation_review?.requires_confirmation) setReviewing(service);
    else mutation.mutate({ service, enabled });
  };
  return <><div className="sectionhead"><h2>Workspace services</h2><span className="small muted">Services are client-facing; Connections supply their upstream targets.</span></div>
    {services.isPending ? <LoadState/> : services.isError ? <ErrorState error={services.error} reload={() => void services.refetch()}/> :
      <div className="servicecontrols">{services.data.map(service => <div className="servicecontrol" key={service.kind}>
        <div><b>{labels[service.kind]}</b><p>{service.configured ? 'Configured with upstream resources' : service.enabled ? 'Enabled · not configured' : 'Disabled'}</p></div>
        <div className="actions"><span className="state-tag">{service.configured ? 'Configured' : service.enabled ? 'Empty' : 'Off'}</span>
          {service.enabled && <Link className="btn" to={service.kind === 'models' ? paths.workspaceModels : service.kind === 'mcp' ? paths.workspaceMCP : paths.workspaceSearch}
            params={{ slug: workspace.slug }}>{service.configured ? 'Open' : 'Configure'}</Link>}
          {!service.configured && <button type="button" className="btn" disabled={!managed || mutation.isPending} onClick={() => change(service, !service.enabled)}>
            {service.enabled ? 'Disable' : 'Enable'}</button>}</div>
      </div>)}</div>}
    {mutation.isError && !reviewing && <div className="formerror" role="alert">{errorMessage(mutation.error)}
      {mutation.error instanceof ControlRequestError && (mutation.error.status === 409 || mutation.error.status === 428) ? ' Review current Service authority before retrying.' : ''}
      <div className="actions"><button type="button" className="btn" onClick={refresh}>Reload current state</button></div>
    </div>}
    <div className="section"><p className="fieldhint">Remove routes, mounts or Search policy before disabling a configured Service. Adding a Service may expand an existing broad Principal grant or published Workspace reachability.</p></div>
    {reviewing?.activation_review && <Modal id="service-activation-title" title={`Enable ${labels[reviewing.kind]}`} close={() => { setReviewing(null); mutation.reset(); }}>
      <div className="modalbody"><p className="fieldhint">This enables the Service for the current Workspace. Its configuration may be empty now; existing broad grants will also authorize future configured operations.</p>
        <div className="review-panel"><dl className="metadata">
          <dt>Workspace</dt><dd><b>{workspace.display_name || workspace.slug}</b><span className="mono small muted"> · {workspace.slug}</span></dd>
          <dt>Service</dt><dd>{labels[reviewing.kind]}</dd>
          <dt>Existing grants affected</dt><dd>{reviewing.activation_review.affected_principals.length ?
            <ul>{reviewing.activation_review.affected_principals.map(principal => <li key={principal.id}>{principal.label} <span className="mono muted">({principal.id})</span></li>)}</ul> : 'No current Principal grant gains this Service'}</dd>
          <dt>Relay publication</dt><dd>{reviewing.activation_review.published ? 'Published: this Service becomes addressable through the existing Workspace origin' : 'Local only'}</dd>
        </dl></div>
        {mutation.isError && <p className="formerror" role="alert">{errorMessage(mutation.error)} Reload the current grants and publication before reviewing again.</p>}
      </div>
      <div className="modalfoot">
        {mutation.isError && <button type="button" className="btn" onClick={refresh}>Reload current review</button>}
        <button type="button" className="btn" disabled={mutation.isPending} onClick={() => setReviewing(null)}>Cancel</button>
        <button type="button" className="btn primary" disabled={!managed || mutation.isPending || mutation.isError} onClick={() => mutation.mutate({ service: reviewing, enabled: true })}>
          {mutation.isPending ? 'Applying…' : `Confirm enabling ${labels[reviewing.kind]}`}</button>
      </div>
    </Modal>}
  </>;
}
function ModelRoutesPage({ workspace, selectedPattern }: { workspace: Workspace; selectedPattern?: string }) {
  return <ModelRoutesAuthoring workspace={workspace} selectedPattern={selectedPattern}/>;
}
function MCPPage({ workspace }: { workspace: Workspace }) { return <MCPAuthoring workspace={workspace}/>; }
function SearchPage({ workspace }: { workspace: Workspace }) { return <SearchAuthoring workspace={workspace}/>; }
export function WorkspacePage({ slug, section, pathname }: { slug: string; section: string; pathname: string }) {
  const query = useQuery({ queryKey: queryKeys.workspace(slug), queryFn: () => getWorkspace(slug) });
  if (query.isPending) return <><Header title={slug} back={{ href: '/workspaces', label: 'Workspaces' }}/><LoadState/></>;
  if (query.isError) return <><Header title={slug} back={{ href: '/workspaces', label: 'Workspaces' }}/><ErrorState error={query.error} reload={() => void query.refetch()}/></>;
  const ws = query.data;
  const child = pathname.split('/').filter(Boolean)[3];
  return <><WorkspaceHeader workspace={ws} section={section}/>{section === 'overview' ? <WorkspaceOverview workspace={ws}/> :
    section === 'services' ? child === 'models' ? <ModelRoutesPage workspace={ws} selectedPattern={pathname.split('/').filter(Boolean)[4] ? decodeURIComponent(pathname.split('/').filter(Boolean)[4]) : undefined}/> : child === 'mcp' ? <MCPPage workspace={ws}/> : child === 'search' ? <SearchPage workspace={ws}/> : <ServicesPage workspace={ws}/> :
    section === 'access' ? <WorkspaceAccess workspace={ws}/> :
    section === 'limits' ? <WorkspaceLimits workspace={ws}/> :
    section === 'connect' ? <><WorkspaceConnect workspace={ws}/><WorkspacePublication workspace={ws}/></> :
      <EmptyState title={`${section[0].toUpperCase()}${section.slice(1)} is not connected yet`} detail="The current Node state is preserved. This workflow is not implemented in the Web Operator yet."/>}</>;
}

function connectionEndpoint(connection: Connection): string {
  if (connection.kind === 'model') return 'endpoint' in connection.spec ? connection.spec.endpoint || connection.spec.provider_id : connection.spec.provider_id;
  if (connection.kind === 'search') return connection.spec.endpoint;
  return connection.spec.transport === 'streamable_http' ? connection.spec.endpoint : connection.spec.executable_name;
}
export function ConnectionsPage() {
  const managed = useManagedNode();
  const query = useQuery({ queryKey: queryKeys.connections, queryFn: listConnections });
  const [creating, setCreating] = useState(false);
  const [filter, setFilter] = useState('');
  const [kind, setKind] = useState<ConnectionKind | 'all'>('all');
  const rows = (query.data ?? []).filter(connection => (kind === 'all' || connection.kind === kind) &&
    (connection.display_name + ' ' + connectionEndpoint(connection)).toLowerCase().includes(filter.toLowerCase()));
  return <><Header title="Connections" desc="Reusable upstream accounts and servers referenced by Workspaces."
    actions={<button className="btn primary" disabled={!managed} type="button" onClick={() => setCreating(true)}>New Connection</button>}/>
    <div className="filterbar"><label>Find a Connection<input value={filter} onChange={event => setFilter(event.target.value)} placeholder="Name or endpoint"/></label>
      <label>Type<select value={kind} onChange={event => setKind(event.target.value as ConnectionKind | 'all')}>
        <option value="all">All types</option><option value="model">Model provider</option><option value="mcp">MCP server</option><option value="search">Search provider</option></select></label></div>
    {query.isPending ? <LoadState/> : query.isError ? <ErrorState error={query.error} reload={() => void query.refetch()}/> : rows.length ?
      <div className="objectlist">{rows.map(connection => <Link className="objectrow" key={connection.id} to={paths.connection} params={{ id: connection.id }}>
        <div><b className="objecttitle">{connection.display_name}</b><div className="small muted">{labels[connection.kind === 'model' ? 'models' : connection.kind]}</div></div>
        <div className="mono muted">{connectionEndpoint(connection)}</div><div className="small muted">Revision {connection.revision}</div>
        <span className="textlink">Inspect ↗</span></Link>)}</div> :
      <EmptyState title={query.data.length ? 'No matching Connections' : 'No Connections yet'} detail="Connections supply upstream capabilities. Create one to reuse it across Workspaces."/>}
    {creating && <CreateConnectionDialog close={() => setCreating(false)}/>}
  </>;
}
export function ConnectionPage({ id }: { id: string }) {
  const managed = useManagedNode();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const connection = useQuery({ queryKey: queryKeys.connection(id), queryFn: () => getConnection(id) });
  const references = useQuery({ queryKey: queryKeys.references(id), queryFn: () => getConnectionReferences(id) });
  const [confirming, setConfirming] = useState(false);
  const [editing, setEditing] = useState<Connection | null>(null);
  const [replacing, setReplacing] = useState<Connection | null>(null);
  const inspection = useMutation({ mutationFn: () => checkConnection(connection.data!) });
  const deletion = useMutation({ mutationFn: () => deleteConnection(connection.data!), onSuccess: async () => {
    await qc.invalidateQueries({ queryKey: queryKeys.connections });
    await navigate({ to: paths.connections });
  } });
  if (connection.isPending) return <><Header title={id} back={{ href: '/connections', label: 'Connections' }}/><LoadState/></>;
  if (connection.isError) return <><Header title={id} back={{ href: '/connections', label: 'Connections' }}/><ErrorState error={connection.error} reload={() => void connection.refetch()}/></>;
  const c = connection.data;
  return <><Header className="connection-head" title={c.display_name} desc={`${labels[c.kind === 'model' ? 'models' : c.kind]} · ${connectionEndpoint(c)}`} back={{ href: '/connections', label: 'Connections' }}
    actions={<><button className="btn" disabled={inspection.isPending} onClick={() => inspection.mutate()}>
      {inspection.isPending ? 'Checking…' : 'Check upstream'}</button>
      <button className="btn" disabled={!managed} onClick={() => setEditing(c)}>Edit configuration</button>
      <button className="btn" disabled={!managed || !canReplaceConnectionCredential(c)} onClick={() => setReplacing(c)}>Replace credential</button>
      <button className="btn danger" disabled={!managed || references.isPending || references.isError || !!references.data?.consumers.length}
        onClick={() => setConfirming(true)}>Delete Connection</button></>}/>
    <div className="sectionhead"><h2>Upstream identity</h2><span className="small muted">Redacted configuration</span></div>
    <dl className="metadata"><dt>Connection ID</dt><dd className="mono">{c.id}</dd><dt>Kind</dt><dd>{c.kind}</dd><dt>Endpoint / server</dt><dd className="mono">{connectionEndpoint(c)}</dd><dt>Configuration revision</dt><dd className="mono">{c.revision}</dd></dl>
    <div className="sectionhead section"><h2>Connectivity</h2><span className="small muted">Revision-bound upstream observation</span></div>
    {inspection.isError ? <div className="formerror" role="alert">{errorMessage(inspection.error)}
        <div className="actions"><button className="btn" onClick={() => { inspection.reset(); void connection.refetch(); }}>Reload Connection</button></div></div> :
      inspection.data ? <dl className="metadata">
        <dt>Checked at</dt><dd>{inspection.data.checked_at}</dd>
        <dt>Reachability</dt><dd>{inspection.data.reachability}</dd>
        <dt>Authentication</dt><dd>{inspection.data.authentication}</dd>
        <dt>Capabilities</dt><dd>{inspection.data.capability_summary?.join(', ') || 'No capabilities reported'}</dd>
        {inspection.data.sanitized_error && <><dt>Failure class</dt><dd>{inspection.data.sanitized_error}</dd></>}
      </dl> : <p className="fieldhint">Run a check to observe the actual upstream; no simulated “healthy” state is shown.</p>}
    <div className="sectionhead section"><h2>Consumers</h2></div>
    {references.isPending ? <LoadState/> : references.isError ? <ErrorState error={references.error} reload={() => void references.refetch()}/> :
      references.data.consumers.length ? <div className="objectlist">{references.data.consumers.map((ref, i) => <div className="objectrow" key={`${ref.workspace}-${i}`}>
        <div><Link className="textlink" to={paths.workspaceOverview} params={{ slug: ref.workspace }}>{ref.workspace}</Link></div>
        <div>{ref.kind}</div><div className="small muted">{ref.route || ref.mount || 'Workspace reference'}</div></div>)}</div> :
      <EmptyState title="No Workspace consumers" detail="This Connection is not currently referenced by a Workspace route or mount."/>}
    {editing && <EditConnectionDialog key={`${editing.id}:${editing.revision}`} connection={editing} close={() => setEditing(null)}/>}
    {replacing && <ReplaceConnectionCredentialDialog key={`${replacing.id}:${replacing.revision}`} connection={replacing} close={() => setReplacing(null)}/>}
    {confirming && <Modal id="delete-connection-title" title="Delete Connection?" close={() => setConfirming(false)}>
      <div className="modalbody"><p>This deletes <b>{c.display_name}</b>. Connections still used by Workspaces cannot be deleted. No other Connection or Workspace will be removed.</p>
        {deletion.isError && <div className="formerror" role="alert">{errorMessage(deletion.error)}
          <div className="actions"><button className="btn" type="button" onClick={() => {
            deletion.reset(); setConfirming(false); void Promise.all([qc.invalidateQueries({ queryKey: queryKeys.connection(id) }), qc.invalidateQueries({ queryKey: queryKeys.references(id) })]);
          }}>Review current Connection</button></div></div>}</div>
      <div className="modalfoot"><button className="btn" type="button" onClick={() => setConfirming(false)}>Cancel</button>
        <button className="btn danger" disabled={deletion.isPending} onClick={() => deletion.mutate()}>Confirm deletion</button></div></Modal>}
  </>;
}
export function NodeSettingsPage({ section }: { section: string }) {
  const node = useQuery({ queryKey: queryKeys.node, queryFn: getNodeOverview });
  const tabs = [['history', 'History & exports'], ['network', 'Network'], ['administration', 'Administration']] as const;
  return <><Header title="Settings" desc="Node-wide configuration and evidence."/><nav className="tabs" aria-label="Settings sections">{tabs.map(([name, label]) =>
    <Link key={name} to={`/settings/${name}`} aria-current={section === name ? 'page' : undefined}>{label}</Link>)}</nav>
    {node.isPending ? <LoadState/> : node.isError ? <ErrorState error={node.error} reload={() => void node.refetch()}/> :
      <><div className="sectionhead"><h2>Node configuration</h2><span className="state-tag">{node.data.configuration_mode === 'managed' ? 'Managed' : 'Externally managed · read-only'}</span></div>
      <dl className="metadata"><dt>Workspaces</dt><dd>{node.data.workspace_count}</dd><dt>Connections</dt><dd>{node.data.connection_count}</dd>
        <dt>Principals</dt><dd>{node.data.principal_count}</dd><dt>Remote publications</dt><dd>{node.data.publication_count}</dd><dt>Spending policies</dt><dd>{node.data.budget_policy_count}</dd>
        <dt>Node revision</dt><dd className="mono">{node.data.revision}</dd></dl>
      {section === 'network' ? <NetworkPublicationSummary/> : section === 'history' ? null : <div className="section"><EmptyState title={`${tabs.find(([name]) => name === section)?.[1] || 'Settings'} controls are not yet connected`}
        detail="Current Node configuration is shown above. This page does not edit retained history or administrative grants."/></div>}</>}
    {section === 'history' && <HistoryExports/>}
  </>;
}
export function ResourcePage({ pathname }: { pathname: string }) {
  const { global, workspace, section } = pageContext(pathname);
  const segments = pathname.split('/').filter(Boolean);
  if (pathname === paths.workspaces) return <WorkspacesPage/>;
  if (workspace) return <WorkspacePage slug={workspace} section={section || 'overview'} pathname={pathname}/>;
  if (pathname === paths.principals) return <PrincipalsPage/>;
  if (segments[0] === 'access' && segments[1]) return <PrincipalPage id={segments[1]}/>;
  if (pathname === paths.connections) return <ConnectionsPage/>;
  if (segments[0] === 'connections' && segments[1]) return <ConnectionPage id={segments[1]}/>;
  if (global === 'Settings') return <NodeSettingsPage section={segments[1] || 'history'}/>;
  if (pathname === paths.activity) return <ActivityPage/>;
  if (segments[0] === 'activity' && segments[1]) return <ActivityDetailPage id={segments[1]}/>;
  return <><Header title={global === 'Access' ? 'Access' : 'Activity'}/><EmptyState title="This Operator journey is not yet connected"
    detail="The page is reserved in the accepted navigation. No sample data or simulated mutations are displayed."/></>;
}
