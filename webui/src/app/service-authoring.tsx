import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import {
  applyMCPMount, deleteMCPMount, deleteModelRoute, deleteSearchRoute, getMCPCatalog, getMCPMounts, getModelRoute,
  getNodeOverview, getSearchRoute, getWorkspace, listConnections, previewMCPMount,
  putModelRoute, putSearchRoute, queryKeys, previewModelMatch, setModelRouteOrder,
  type Connection, type MCPMount, type MCPMountPreview, type RouteDraft, type Workspace,
} from '../api/resources';
import { ControlRequestError } from '../api/transport';
import { EmptyState, ErrorState, errorMessage, LoadState, Modal } from './control-ui';
import { paths } from './routes';
import { CreateConnectionDialog } from './connection-create';

const validIdentifier = (value: string) => /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(value) && value !== '.' && value !== '..';
// The server validates Go path.Match grammar. The browser rejects only invalid
// input boundaries, not a second homegrown glob interpretation.
const validRoutePattern = (value: string) => !!value && value.length <= 128 && value === value.trim() &&
  !/\s/.test(value) && !Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127) && value !== '.' && value !== '..';
const changedAuthority = (error: unknown): boolean => error instanceof ControlRequestError && (error.status === 412 || error.status === 409);
function validMountAlias(value: string) {
  return /^[a-z][a-z0-9-]{0,31}$/.test(value) && value !== 'swobu';
}
function useWritable(): boolean {
  const node = useQuery({ queryKey: queryKeys.node, queryFn: getNodeOverview });
  return node.data?.configuration_mode === 'managed';
}
function useModelConnections() {
  return useQuery({ queryKey: queryKeys.connections, queryFn: listConnections });
}
async function invalidateWorkspace(qc: ReturnType<typeof useQueryClient>, slug: string) {
  await Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.workspace(slug) }),
    qc.invalidateQueries({ queryKey: queryKeys.services(slug) }),
    qc.invalidateQueries({ queryKey: queryKeys.workspaces }),
  ]);
}
function RouteEditorLoader({ workspace, name, close }: { workspace: Workspace; name: string | null; close: () => void }) {
  const source = useQuery({ queryKey: queryKeys.route(workspace.slug, name || ''),
    queryFn: () => getModelRoute(workspace.slug, name!), enabled: name !== null, retry: false });
  if (name !== null && source.isPending) return <Modal id="route-title" title={`Edit ${name}`} close={close}><div className="modalbody"><LoadState/></div></Modal>;
  if (name !== null && source.isError) return <Modal id="route-title" title={`Edit ${name}`} close={close}><div className="modalbody"><ErrorState error={source.error} reload={() => void source.refetch()}/></div></Modal>;
  return <RouteEditor key={`${workspace.slug}:${name ?? 'new'}`} workspace={workspace} name={name} close={close}
    initial={name !== null ? source.data! : { tiers: [{ targets: [{ id: 'primary', connection_id: '', model: '', protocol: 'responses' }] }] }}/>
}
function RouteEditor({ workspace, name, initial, close }: { workspace: Workspace; name: string | null; initial: RouteDraft; close: () => void }) {
  const qc = useQueryClient();
  const connections = useModelConnections();
  const [routeName, setRouteName] = useState(name ?? '');
  const [draft, setDraft] = useState<RouteDraft>(() => structuredClone(initial));
  const [creatingFor, setCreatingFor] = useState<{ tier: number; target: number } | null>(null);
  const mutation = useMutation({ mutationFn: () => putModelRoute(workspace, routeName, draft), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    await invalidateWorkspace(qc, workspace.slug);
    await qc.invalidateQueries({ queryKey: ['model-route', workspace.slug] });
    close();
  } });
  const change = (edit: (next: RouteDraft) => void) => {
    setDraft(previous => { const next = structuredClone(previous); edit(next); return next; });
    mutation.reset();
  };
  const rows = draft.tiers.flatMap(tier => tier.targets);
  const duplicate = new Set(rows.map(row => row.id)).size !== rows.length;
  const routeExists = name === null && workspace.routes.some(route => route.name === routeName);
  const usable = !routeExists && validRoutePattern(routeName) && draft.tiers.length > 0 && rows.length > 0 && !duplicate &&
    draft.tiers.every(tier => tier.targets.length > 0) && rows.every(target => validIdentifier(target.id) &&
      target.model.trim() && target.protocol.trim() && target.connection_id &&
      connections.data?.some(c => c.kind === 'model' && c.id === target.connection_id));
  const available = connections.data?.filter(c => c.kind === 'model') ?? [];
  return <><Modal id="route-title" title={name === null ? 'Create Model route' : `Edit route · ${name}`} close={close}>
    <form onSubmit={event => { event.preventDefault(); if (usable && !changedAuthority(mutation.error)) mutation.mutate(); }}>
      <div className="modalbody authoring-form">
        <label>Model pattern<input required autoFocus disabled={name !== null} value={routeName} placeholder="claude-sonnet-*"
          onChange={event => { setRouteName(event.target.value); mutation.reset(); }}/></label>
        {routeExists && <p role="alert" className="formerror">A route with this pattern already exists. Edit its targets from the Models list.</p>}
        <p className="fieldhint">Examples: <code>coding</code> (virtual model) · <code>claude-sonnet-*</code> (model family) · <code>deepseek/*</code> (provider IDs) · <code>*</code> (unqualified models). Go glob syntax; slashes are separators.</p>
        <p className="fieldhint">Route priority is set in the Models list. Tier order sets failover within the winning route. Targets in one tier are equal peers.</p>
        {connections.isPending ? <LoadState/> : connections.isError ? <ErrorState error={connections.error} reload={() => void connections.refetch()}/> :
          <>
            {!available.length && <p className="fieldhint">No Model Connections yet. Create one from a target below; this draft will be kept.</p>}
            <div className="tiereditor">{draft.tiers.map((tier, i) => <section className="panel" key={i}>
              <div className="panelhead"><h2>Tier {i + 1} <span className="small muted">· {i === 0 ? 'Primary' : 'Fallback'}</span></h2>
                <div className="actions">
                  <button type="button" className="btn" disabled={i === 0} onClick={() => change(d => { [d.tiers[i - 1], d.tiers[i]] = [d.tiers[i], d.tiers[i - 1]]; })}>↑</button>
                  <button type="button" className="btn" disabled={i === draft.tiers.length - 1} onClick={() => change(d => { [d.tiers[i + 1], d.tiers[i]] = [d.tiers[i], d.tiers[i + 1]]; })}>↓</button>
                  <button type="button" className="btn danger" disabled={draft.tiers.length === 1} onClick={() => change(d => { d.tiers.splice(i, 1); })}>Remove tier</button>
                </div></div>
              <div className="panelbody authoring-targets">{tier.targets.map((target, j) => <div className="targetedit" key={`${i}-${j}`}>
                <div className="authoring-fields">
                  <label>Target ID<input required value={target.id} onChange={event => change(d => { d.tiers[i].targets[j].id = event.target.value; })} placeholder="azure-east"/></label>
                  <label>Connection<select required value={target.connection_id} onChange={event => change(d => { d.tiers[i].targets[j].connection_id = event.target.value; })}>
                    <option value="">Choose Connection…</option>{available.map(connection => <option key={connection.id} value={connection.id}>{connection.display_name}</option>)}</select></label>
                  <label>Upstream model<input required value={target.model} onChange={event => change(d => { d.tiers[i].targets[j].model = event.target.value; })} placeholder="gpt-5"/></label>
                  <label>Protocol<input required value={target.protocol} onChange={event => change(d => { d.tiers[i].targets[j].protocol = event.target.value; })} list="route-protocols" placeholder="responses"/></label>
                </div><div className="actions"><button type="button" className="btn" disabled={mutation.isPending || mutation.isError} onClick={() => setCreatingFor({ tier: i, target: j })}>New Model Connection</button>
                <button className="btn danger" type="button" disabled={tier.targets.length === 1} onClick={() => change(d => { d.tiers[i].targets.splice(j, 1); })}>Remove target</button></div>
              </div>)}</div><div className="panelbody compact-footer"><button type="button" className="btn" onClick={() => change(d => {
                const id = `target-${crypto.randomUUID().slice(0, 8)}`;
                d.tiers[i].targets.push({ id, connection_id: '', model: '', protocol: 'responses' });
              })}>+ Equal peer</button></div></section>)}</div>
            <button type="button" className="btn" onClick={() => change(d => { d.tiers.push({ targets: [{ id: `target-${crypto.randomUUID().slice(0, 8)}`, connection_id: '', model: '', protocol: 'responses' }] }); })}>+ Fallback tier</button>
            <datalist id="route-protocols">{['responses', 'responses_stream', 'chat_completions', 'chat_completions_stream', 'messages', 'messages_stream'].map(protocol => <option value={protocol} key={protocol}/>)}</datalist>
            <p className="fieldhint">Protocol is validated by the Node against the selected provider. It is never inferred from model spelling.</p>
          </>}
        {duplicate && <p role="alert" className="formerror">Target IDs must be unique across all tiers.</p>}
        {mutation.isError && <p role="alert" className="formerror">{errorMessage(mutation.error)} The draft is preserved. Check the current Workspace before retrying if the save may have reached the Node.</p>}
      </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
        <button className="btn primary" type="submit" disabled={!usable || mutation.isPending || changedAuthority(mutation.error)}>{mutation.isPending ? 'Saving…' : name === null ? 'Create route' : 'Save route'}</button></div>
    </form>
  </Modal>
    {creatingFor && <CreateConnectionDialog kind="model" close={() => setCreatingFor(null)} onCreated={connection => {
      const { tier, target } = creatingFor;
      change(next => { next.tiers[tier].targets[target].connection_id = connection.id; });
      setCreatingFor(null);
    }}/>}</>;
}
function DeleteModelRouteDialog({ workspace, route, close }: { workspace: Workspace; route: string; close: () => void }) {
  const qc = useQueryClient();
  const mutation = useMutation({ mutationFn: () => deleteModelRoute(workspace, route), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    await invalidateWorkspace(qc, workspace.slug);
    await qc.invalidateQueries({ queryKey: ['model-route', workspace.slug] });
    close();
  } });
  return <Modal id="delete-route-title" title={`Delete route · ${route}`} close={close}>
    <div className="modalbody authoring-form">
      <p>Requests currently matching <span className="mono">{route}</span> may select a lower-priority route after deletion, or have no match. Other Connections and Workspaces remain unchanged.</p>
      {workspace.routes.length === 1 && <p className="fieldhint">Models will remain enabled but unconfigured.</p>}
      {mutation.isError && <p role="alert" className="formerror">{errorMessage(mutation.error)} Reload the Workspace before deciding again.</p>}
    </div>
    <div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
      <button className="btn danger" disabled={mutation.isPending} type="button" onClick={() => mutation.mutate()}>
        {mutation.isPending ? 'Deleting…' : 'Delete route'}</button></div>
  </Modal>;
}
export function ModelRoutesAuthoring({ workspace, selectedPattern }: { workspace: Workspace; selectedPattern?: string }) {
  const managed = useWritable();
  const qc = useQueryClient();
  const [editing, setEditing] = useState<string | null | undefined>();
  const [removing, setRemoving] = useState<string | null>(null);
  const [priorityDraft, setPriorityDraft] = useState<{ revision: string; order: string[] } | null>(null);
  const [modelID, setModelID] = useState('claude-sonnet-4-5');
  const [modelToCheck, setModelToCheck] = useState('');
  const enabled = workspace.enabled_services.includes('models');
  const liveOrder = workspace.routes.map(route => route.name);
  const pending = priorityDraft !== null;
  const staleDraft = priorityDraft !== null && priorityDraft.revision !== workspace.management_etag;
  const ordered = (priorityDraft?.order ?? liveOrder).map(name => workspace.routes.find(route => route.name === name)).filter(route => route !== undefined);
  const selected = ordered.find(route => route.name === selectedPattern) ?? ordered[0];
  const reorder = useMutation({ mutationFn: (order: string[]) => setModelRouteOrder(workspace, order), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    setPriorityDraft(null);
    setModelToCheck('');
    await invalidateWorkspace(qc, workspace.slug);
  } });
  const move = (index: number, direction: number) => {
    const order = [...(priorityDraft?.order ?? liveOrder)];
    [order[index], order[index + direction]] = [order[index + direction], order[index]];
    setPriorityDraft({ revision: priorityDraft?.revision ?? workspace.management_etag, order });
    reorder.reset();
  };
  const modelMatch = useQuery({
    queryKey: ['model-match', workspace.slug, workspace.management_etag, modelToCheck],
    queryFn: () => previewModelMatch(workspace.slug, modelToCheck), enabled: !!modelToCheck && !pending, retry: false,
  });
  return <>
    <div className="sectionhead"><h2>Model routes</h2><div className="actions"><span className="small muted">Highest priority first · first match wins</span>
      <button type="button" className="btn primary" disabled={!managed || !enabled || pending} onClick={() => setEditing(null)}>New route</button></div></div>
    {!enabled ? <EmptyState title="Models disabled" detail="Enable the Models Service before adding routes."/> : <>
      <p className="fieldhint">Routes are checked top to bottom. A failed target can use later tiers inside the winning route; it does not select another route.</p>
      {ordered.length ? <div className="priority-list" aria-label="Model route priority">
        {ordered.map((route, index) => <div className={`priority-route ${route.name === selected?.name ? 'selected' : ''}`} key={route.name}>
          <span className="priority-num">{index + 1}</span>
          <div className="priority-body">
            <Link to={paths.workspaceModelRoute} params={{ slug: workspace.slug, route: route.name }} className="priority-link mono">{route.name}</Link>
            <div className="small muted">{route.tiers.length} tier{route.tiers.length === 1 ? '' : 's'} · {route.tiers.reduce((n, tier) => n + tier.targets.length, 0)} targets</div>
            <div className="priority-sources">{route.tiers.map((tier, i) => `Tier ${i + 1}: ${tier.targets.map(t => t.model).join(' + ')}`).join(' → ')}</div>
            {index > 0 && ordered.slice(0, index).some(earlier => earlier.name === '*') && !route.name.includes('/') &&
              <span className="small route-warning">A preceding * can shadow this pattern. Try a model ID to inspect saved routing.</span>}
          </div>
          <div className="priority-movers"><button className="btn" type="button" aria-label={`Move ${route.name} up`} disabled={!managed || index === 0 || reorder.isPending || changedAuthority(reorder.error)}
            onClick={() => move(index, -1)}>↑</button>
            <button className="btn" type="button" aria-label={`Move ${route.name} down`} disabled={!managed || index === ordered.length - 1 || reorder.isPending || changedAuthority(reorder.error)}
              onClick={() => move(index, 1)}>↓</button></div>
        </div>)}
      </div> : <EmptyState title="No model routes" detail="Create a model pattern with one upstream target."/>}
      {pending && <div className="priority-pending" role="status"><div><b>Unapplied priority</b><p className="small muted">{staleDraft ? 'Workspace changed. Discard and review the new order.' : 'Live routing is unchanged until Apply priority.'}</p></div>
        <div className="actions"><button className="btn" type="button" onClick={() => { setPriorityDraft(null); reorder.reset(); void invalidateWorkspace(qc, workspace.slug); }}>Discard</button>
          <button className="btn primary" type="button" disabled={!managed || staleDraft || reorder.isPending || changedAuthority(reorder.error)}
            onClick={() => { if (priorityDraft) reorder.mutate(priorityDraft.order); }}>{reorder.isPending ? 'Applying…' : 'Apply priority'}</button></div></div>}
      {reorder.isError && <p role="alert" className="formerror">{errorMessage(reorder.error)} Discard and reload before another decision.</p>}
      <form className="route-try" onSubmit={event => { event.preventDefault(); setModelToCheck(modelID); }}>
        <label>Try model ID<input className="mono" value={modelID} onChange={event => { setModelID(event.target.value); setModelToCheck(''); }}
          placeholder="deepseek/deepseek-chat" spellCheck={false}/></label>
        <button className="btn" type="submit" disabled={!modelID || pending}>Check saved routes</button>
        <div className="route-try-result" aria-live="polite">{pending ? <span className="small muted">Apply or discard priority changes to preview live routing.</span> :
          modelMatch.isPending && modelToCheck ? <span className="small muted">Checking…</span> :
          modelMatch.isError ? <span role="alert" className="formerror">{errorMessage(modelMatch.error)}</span> :
          modelMatch.data?.matched ? <span><b>#{modelMatch.data.priority} · {modelMatch.data.pattern}</b> <span className="small muted">{modelMatch.data.tiers} tiers · {modelMatch.data.targets} targets</span></span> :
          modelToCheck ? <span className="small muted">No matching route</span> : <span className="small muted">Use an actual client model ID to preview the winning route.</span>}</div>
      </form>
      {selected && <>
        <div className="sectionhead section"><h2>Route · <span className="mono">{selected.name}</span></h2><div className="actions">
          <button type="button" className="btn" disabled={!managed || pending} onClick={() => setEditing(selected.name)}>Edit targets</button>
          <button type="button" className="btn danger" disabled={!managed || pending} onClick={() => setRemoving(selected.name)}>Delete</button></div></div>
        <p className="fieldhint">Tier 1 is preferred. Targets within a tier are balanced peers; later tiers provide safe fallback.</p>
        <div className="panel">{selected.tiers.map((tier, i) => <div className="route-tier" key={i}><div className="tier-index">Tier {i + 1}</div>
          <div>{tier.targets.map(target => <div className="targetrow" key={target.id}><b>{target.model}</b><span className="mono muted">{target.connection_id}</span><span className="small muted">{target.protocol}</span></div>)}</div></div>)}</div>
      </>}
    </>}
    {editing !== undefined && <RouteEditorLoader name={editing} workspace={workspace} close={() => setEditing(undefined)}/>}
    {removing !== null && <DeleteModelRouteDialog route={removing} workspace={workspace} close={() => setRemoving(null)}/>}
  </>;
}

function MCPEditor({ workspace, initial, close }: { workspace: Workspace; initial: MCPMount | null; close: () => void }) {
  const qc = useQueryClient();
  const connections = useModelConnections();
  const [alias, setAlias] = useState(initial?.alias ?? '');
  const [connectionID, setConnectionID] = useState(initial?.builtin ? 'builtin:swobu-admin' : initial?.connection_id ?? '');
  const builtin = connectionID === 'builtin:swobu-admin';
  const [tools, setTools] = useState<string[]>(initial?.selected_tools ?? []);
  const [creatingConnection, setCreatingConnection] = useState(false);
  const [review, setReview] = useState<MCPMountPreview | null>(null);
  const [adminToolNames, setAdminToolNames] = useState(initial?.selected_tools.join("\n") ?? "");
  const selectedTools = builtin ? [...new Set(adminToolNames.split(/[,\s]+/).filter(Boolean))] : tools;
  const catalog = useQuery({ queryKey: queryKeys.catalog(connectionID), queryFn: () => getMCPCatalog(connectionID), enabled: !!connectionID && !builtin, retry: false });
  const change = () => { setReview(null); preview.reset(); apply.reset(); };
  const mount: MCPMount = { alias, connection_id: builtin ? '' : connectionID, ...(builtin ? { builtin: 'swobu-admin' as const } : {}), selected_tools: [...selectedTools].sort() };
  const preview = useMutation({ mutationFn: () => previewMCPMount(workspace, mount), onSuccess: setReview });
  const apply = useMutation({ mutationFn: () => applyMCPMount(workspace, mount, review!), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    await Promise.all([invalidateWorkspace(qc, workspace.slug), qc.invalidateQueries({ queryKey: queryKeys.mounts(workspace.slug) })]);
    close();
  } });
  // A failed response may follow a successful commit. Read the owning state;
  // never resend retained approval or manufacture a new retry identity.
  const inspect = useMutation({ mutationFn: () => getWorkspace(workspace.slug), onSuccess: async current => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), current);
    await Promise.all([invalidateWorkspace(qc, workspace.slug), qc.invalidateQueries({ queryKey: queryKeys.mounts(workspace.slug) })]);
    close();
  } });
  const locked = apply.isPending || apply.isError;
  const available = connections.data?.filter(c => c.kind === 'mcp') ?? [];
  const valid = validMountAlias(alias) && !!connectionID && selectedTools.length > 0 && (builtin ? selectedTools.every(t => /^[a-z][a-z0-9_]*$/.test(t)) : catalog.isSuccess && tools.every(t => catalog.data.some(x => x.name === t)));
  return <><Modal id="mcp-title" title={initial ? `Edit MCP source · ${alias}` : 'Add MCP source'} close={close}>
    <div className="modalbody authoring-form">
      <div className="authoring-fields">
        <label>Namespace<input autoFocus disabled={!!initial || locked} value={alias} placeholder="github" onChange={event => { setAlias(event.target.value); change(); }}/></label>
        <label>MCP source<select disabled={locked} value={connectionID} onChange={event => { setConnectionID(event.target.value); setTools([]); change(); }}>
          <option value="">Choose source…</option><option value="builtin:swobu-admin">Swobu Node administration · builtin</option>{available.map(c => <option key={c.id} value={c.id}>{c.display_name}</option>)}
        </select></label>
        <button type="button" className="btn" disabled={locked || preview.isPending} onClick={() => setCreatingConnection(true)}>New MCP Connection</button>
      </div>
      {!builtin && (connections.isPending ? <LoadState/> : connections.isError ? <ErrorState error={connections.error} reload={() => void connections.refetch()}/> :
        !available.length ? <p className="fieldhint">No MCP Connections yet. Create one above, then review which tools this Workspace may expose.</p> : null)}
      {builtin && <><p className="fieldhint">This source delegates full Node administration to Principals with MCP access here, including broad Workspace grants. Review affected Principals and publication before applying.</p>
        <label>Selected Admin tool names<textarea rows={4} className="admin-tool-names" disabled={locked} value={adminToolNames} placeholder={"workspaces_list\nworkspace_get"} onChange={event => { setAdminToolNames(event.target.value); change(); }}/></label>
        <p className="fieldhint">Enter exact tool names, separated by spaces or commas. Review validates each name against the builtin catalog; newly added tools remain excluded.</p></>}
      {connectionID && !builtin && (catalog.isPending ? <LoadState/> : catalog.isError ? <ErrorState error={catalog.error} reload={() => void catalog.refetch()}/> :
        <><h3>Expose tools</h3><p className="fieldhint">Only explicitly selected tools are exposed. Newly discovered tools remain excluded.</p>
          <div className="tool-picker">{catalog.data.map(tool => <label key={tool.name}><input type="checkbox" disabled={locked} checked={tools.includes(tool.name)} onChange={event => {
            setTools(previous => event.target.checked ? [...previous, tool.name] : previous.filter(name => name !== tool.name)); change();
          }}/><span className="mono">{tool.name}</span></label>)}</div></>)}
      {!review && <p className="fieldhint">Review is computed against current Workspace, Connection, Principal and publication revisions. No write occurs until you explicitly approve it.</p>}
      {review && <div className="review-panel" role="group" aria-label="MCP authority impact review">
        <h3>Review authority change</h3>
        <dl className="metadata"><dt>Source</dt><dd>{review.builtin ? "This Node · in-process" : review.endpoint}</dd><dt>Execution identity</dt><dd>{review.execution_identity} · {review.identity_verified ? 'Verified' : 'Operator-declared, unverified'}</dd>
          <dt>Declared permissions</dt><dd>{review.permission_summary}</dd>
          <dt>Exposed tools</dt><dd>{review.allowed_tools.join(', ')}</dd>
          <dt>Potentially effectful</dt><dd>{review.potentially_effectful_tools.join(', ') || 'None flagged; tool declarations are not safety proofs'}</dd>
          <dt>Affected principals</dt><dd>{review.affected_principals.map(p => p.label).join(', ') || 'None'}</dd>
          <dt>Publications</dt><dd>{review.published ? 'Published through relay' : 'Local only'}</dd></dl>
        <p className="fieldhint">{review.warning || 'The review expires when any relevant owner revision changes.'}</p>
      </div>}
      {(preview.isError || apply.isError) && <div role="alert" className="formerror">{errorMessage(preview.error ?? apply.error)} {apply.isError ? 'Read the current Workspace before reviewing another change.' : 'Obtain a new review before applying.'}</div>}
      {inspect.isError && <p role="alert" className="formerror">{errorMessage(inspect.error)} Read current state again.</p>}
    </div>
    <div className="modalfoot"><button type="button" className="btn" onClick={close}>Cancel</button>
      {!review ? <button type="button" className="btn primary" disabled={!valid || preview.isPending} onClick={() => preview.mutate()}>{preview.isPending ? 'Reviewing…' : 'Review source & tools'}</button> :
        <>{apply.isError ? <button type="button" className="btn" disabled={inspect.isPending} onClick={() => inspect.mutate()}>{inspect.isPending ? 'Reading…' : 'Read current Workspace'}</button> :
          <button type="button" className="btn" disabled={apply.isPending} onClick={() => { setReview(null); preview.reset(); apply.reset(); }}>Change selection</button>}
          <button type="button" className="btn primary" disabled={locked} onClick={() => apply.mutate()}>{apply.isPending ? 'Applying…' : 'Apply reviewed mount'}</button></>}
    </div>
  </Modal>
    {creatingConnection && <CreateConnectionDialog kind="mcp" close={() => setCreatingConnection(false)} onCreated={connection => {
      setConnectionID(connection.id);
      setTools([]);
      change();
      setCreatingConnection(false);
    }}/>}</>;
}
function MCPRemove({ workspace, mount, close }: { workspace: Workspace; mount: MCPMount; close: () => void }) {
  const qc = useQueryClient();
  const mutation = useMutation({ mutationFn: () => deleteMCPMount(workspace, mount.alias), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    await Promise.all([invalidateWorkspace(qc, workspace.slug), qc.invalidateQueries({ queryKey: queryKeys.mounts(workspace.slug) })]);
    close();
  } });
  return <Modal id="mcp-delete-title" title={`Remove ${mount.alias}?`} close={close}>
    <div className="modalbody"><p>These tools immediately disappear from this Workspace's MCP catalog. The upstream Connection and other Workspaces remain unchanged.</p>
      {mutation.isError && <p className="formerror" role="alert">{errorMessage(mutation.error)}</p>}</div>
    <div className="modalfoot"><button type="button" className="btn" onClick={close}>Cancel</button>
      <button type="button" className="btn danger" disabled={mutation.isPending} onClick={() => mutation.mutate()}>Remove mount</button></div>
  </Modal>;
}
export function MCPAuthoring({ workspace }: { workspace: Workspace }) {
  const managed = useWritable();
  const mounts = useQuery({ queryKey: queryKeys.mounts(workspace.slug), queryFn: () => getMCPMounts(workspace.slug) });
  const [editing, setEditing] = useState<MCPMount | null | undefined>();
  const [removing, setRemoving] = useState<MCPMount | null>(null);
  const enabled = workspace.enabled_services.includes('mcp');
  return <><div className="sectionhead"><h2>MCP catalog</h2><div className="actions"><span className="small muted">Explicit Workspace tool allowlist</span>
    <Link className="textlink" to={paths.activity} search={{ workspace: workspace.slug, kind: 'mcp_operation', cursor: undefined }}>Observed tools ↗</Link>
    <button type="button" className="btn primary" disabled={!managed || !enabled} onClick={() => setEditing(null)}>Add server</button></div></div>
    {!enabled ? <EmptyState title="MCP disabled" detail="Enable the MCP Service before mounting upstream MCP Connections."/> :
      mounts.isPending ? <LoadState/> : mounts.isError ? <ErrorState error={mounts.error} reload={() => void mounts.refetch()}/> :
      mounts.data.length ? <div className="route-list">{mounts.data.map(m => <section className="panel" key={m.alias}>
        <div className="panelhead"><h2>{m.alias}</h2><div className="actions"><button className="btn" disabled={!managed} type="button" onClick={() => setEditing(m)}>Edit tools</button>
          <button className="btn danger" disabled={!managed} type="button" onClick={() => setRemoving(m)}>Remove</button></div></div>
        <div className="panelbody"><div className="small muted mono">{m.builtin ? "Swobu Node administration · builtin" : m.connection_id}</div>
          <div className="tools">{m.selected_tools.map(tool => <span className="tool" key={tool}>{m.alias}.{tool}</span>)}</div></div>
      </section>)}</div> : <EmptyState title="No MCP sources" detail="Choose a Connection, discover its actual tool catalog and explicitly review the allowlist."/>}
    {editing !== undefined && <MCPEditor key={editing?.alias || 'new'} workspace={workspace} initial={editing} close={() => setEditing(undefined)}/>}
    {removing && <MCPRemove workspace={workspace} mount={removing} close={() => setRemoving(null)}/>}
  </>;
}
export function SearchAuthoring({ workspace }: { workspace: Workspace }) {
  const managed = useWritable();
  const qc = useQueryClient();
  const enabled = workspace.enabled_services.includes('search');
  const connections = useModelConnections();
  const current = useQuery({ queryKey: queryKeys.searchRoute(workspace.slug), queryFn: () => getSearchRoute(workspace.slug), enabled });
  const [draft, setDraft] = useState<{ primary: string; fallback: string[] } | null>(null);
  const [confirmClear, setConfirmClear] = useState(false);
  const [creatingFor, setCreatingFor] = useState<'primary' | 'fallback' | null>(null);
  const primary = draft?.primary ?? current.data?.connection_id ?? '';
  const fallback = draft?.fallback ?? current.data?.fallback_connection_ids ?? [];
  const available: Connection[] = connections.data?.filter(c => c.kind === 'search') ?? [];
  const eligible = available.filter(c => c.id !== primary && !fallback.includes(c.id));
  const change = (nextPrimary: string, nextFallback: string[]) => {
    setDraft({ primary: nextPrimary, fallback: nextFallback }); save.reset(); remove.reset();
  };
  const save = useMutation({ mutationFn: () => putSearchRoute(workspace, { connection_id: primary, fallback_connection_ids: fallback }), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    setDraft(null);
    await Promise.all([invalidateWorkspace(qc, workspace.slug), qc.invalidateQueries({ queryKey: queryKeys.searchRoute(workspace.slug) })]);
  } });
  const remove = useMutation({ mutationFn: () => deleteSearchRoute(workspace), onSuccess: async committed => {
    qc.setQueryData(queryKeys.workspace(workspace.slug), committed);
    setDraft(null);
    setConfirmClear(false);
    await Promise.all([invalidateWorkspace(qc, workspace.slug), qc.invalidateQueries({ queryKey: queryKeys.searchRoute(workspace.slug) })]);
  } });
  const locked = !managed || save.isPending || remove.isPending || save.isError || remove.isError;
  const modified = primary !== (current.data?.connection_id ?? '') || JSON.stringify(fallback) !== JSON.stringify(current.data?.fallback_connection_ids ?? []);
  return <><div className="sectionhead"><h2>Direct Search route</h2><span className="small muted">Primary provider, then ordered fallbacks</span></div>
    {!enabled ? <EmptyState title="Search disabled" detail="Enable the Search Service before configuring its route."/> :
      current.isPending || connections.isPending ? <LoadState/> : current.isError ? <ErrorState error={current.error} reload={() => void current.refetch()}/> :
      connections.isError ? <ErrorState error={connections.error} reload={() => void connections.refetch()}/> :
      <section className="panel"><div className="panelbody authoring-form">
        <p className="fieldhint">Direct Search is independent of Models web_search and provider-native search. If an upstream fails, Swobu tries the next Connection. An empty successful result does not trigger fallback.</p>
        {!available.length && <p className="fieldhint">No Search Connections yet. Create one here without leaving this Workspace.</p>}
        <label>Primary Search Connection<select value={primary} disabled={locked}
          onChange={event => change(event.target.value, fallback.filter(id => id !== event.target.value).slice(0, 7))}>
          <option value="">Choose primary Connection…</option>{primary && !available.some(c => c.id === primary) && <option value={primary} disabled>Unavailable Connection: {primary}</option>}{available.map(c => <option value={c.id} key={c.id}>{c.display_name}</option>)}</select></label>
          <div className="actions"><button type="button" className="btn" disabled={locked} onClick={() => setCreatingFor('primary')}>New primary Connection</button></div>
          <div className="sectionhead"><h3>Fallback order</h3><span className="small muted">Up to 7 additional Search Connections</span></div>
          <div className="route-list">{fallback.map((id, i) => <div className="search-fallback-row" key={id}>
            <div><b>{i + 2}. {available.find(c => c.id === id)?.display_name ?? id}</b><div className="mono small muted">{id}</div></div>
            <div className="actions"><button className="btn" type="button" disabled={locked || i === 0} aria-label={`Move ${id} earlier`} onClick={() => {
              const next = [...fallback]; [next[i - 1], next[i]] = [next[i], next[i - 1]]; change(primary, next);
            }}>↑</button><button className="btn" type="button" disabled={locked || i === fallback.length - 1} aria-label={`Move ${id} later`} onClick={() => {
              const next = [...fallback]; [next[i], next[i + 1]] = [next[i + 1], next[i]]; change(primary, next);
            }}>↓</button><button className="btn" type="button" disabled={locked} aria-label={`Remove ${id}`} onClick={() => change(primary, fallback.filter(other => other !== id))}>Remove</button></div>
          </div>)}</div>
          {!!primary && !!eligible.length && fallback.length < 7 && <label>Add fallback Search Connection<select disabled={locked} value="" onChange={event => { if (event.target.value) change(primary, [...fallback, event.target.value]); }}>
            <option value="">Choose another Connection…</option>{eligible.map(c => <option value={c.id} key={c.id}>{c.display_name}</option>)}</select></label>}
          {!!primary && fallback.length < 7 && <div className="actions"><button type="button" className="btn" disabled={locked} onClick={() => setCreatingFor('fallback')}>New fallback Connection</button></div>}
        {(save.isError || remove.isError) && <div role="alert" className="formerror">{errorMessage(save.error ?? remove.error)} Do not retry against an unknown Workspace revision.
          <div className="actions"><button className="btn" type="button" onClick={async () => {
            await Promise.all([qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace.slug) }), current.refetch()]);
            setDraft(null); save.reset(); remove.reset();
          }}>Read current Workspace</button></div></div>}
        {modified && !save.isError && <p className="fieldhint">Unsaved Search routing changes. All new calls will use the committed route after save.</p>}
        <div className="actions"><button type="button" className="btn primary" disabled={locked || !primary || !modified || fallback.includes(primary)} onClick={() => save.mutate()}>Save Search route</button>
          {modified && <button className="btn" type="button" disabled={locked} onClick={() => setDraft(null)}>Discard changes</button>}
          {current.data && <button type="button" className="btn danger" disabled={locked} onClick={() => setConfirmClear(true)}>Clear route</button>}</div>
      </div></section>}
    {confirmClear && <Modal id="clear-search-title" title="Clear Direct Search route?" close={() => setConfirmClear(false)}>
      <div className="modalbody authoring-form"><p>Direct Search clients will stop resolving this Workspace Service until a new Search Connection is configured. Models provider-native search is not changed.</p>
        {remove.isError && <p role="alert" className="formerror">{errorMessage(remove.error)}</p>}</div>
      <div className="modalfoot"><button type="button" className="btn" onClick={() => setConfirmClear(false)}>Cancel</button>
        <button type="button" className="btn danger" disabled={locked} onClick={() => remove.mutate()}>Clear route</button></div>
    </Modal>}
    {creatingFor && <CreateConnectionDialog kind="search" close={() => setCreatingFor(null)} onCreated={connection => {
      if (creatingFor === 'primary') change(connection.id, fallback.filter(id => id !== connection.id));
      else change(primary, [...fallback, connection.id]);
      setCreatingFor(null);
    }}/>} 
  </>;
}
