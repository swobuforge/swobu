import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import { previewModelMatch, type Workspace } from '../api/resources';
import { clientRecipe, clientRecipeKinds, requiresModel, usableModelID, type ClientRecipeKind } from './remote-client-recipes';
import { paths } from './routes';
import type { WorkspaceEndpoint } from './workspace-endpoints';

// Manual setup is intentionally independent of Connect's reviewed local-file
// mutation: this browser cannot write configuration on a second machine.
export function ManualWorkspaceSetup({ workspace, local, remote }: {
  workspace: Workspace;
  local: WorkspaceEndpoint[];
  remote: WorkspaceEndpoint[];
}) {
  const kinds = clientRecipeKinds.filter(item => workspace.enabled_services.includes(item.service));
  const exactRoutes = workspace.routes.map(route => route.name).filter(name => !/[*?[\]\\]/.test(name));
  const [kind, setKind] = useState<ClientRecipeKind>(kinds[0]?.id || 'codex');
  const [where, setWhere] = useState<'local' | 'remote'>('local');
  const [model, setModel] = useState(exactRoutes[0] || '');
  const [copyState, setCopyState] = useState<{ body: string; ok: boolean } | null>(null);
  const chosen = kinds.find(item => item.id === kind) ? kind : kinds[0]?.id;
  const endpoints = where === 'remote' ? remote : local;
  const needsModel = chosen ? requiresModel(chosen) : false;
  const modelValid = usableModelID(model);
  const match = useQuery({
    queryKey: ['manual-client-model-match', workspace.slug, workspace.management_etag, model],
    queryFn: () => previewModelMatch(workspace.slug, model),
    enabled: needsModel && modelValid && endpoints.length > 0,
    retry: false,
  });
  // A failed refresh can retain cached data; only a completed successful check
  // may authorize an example for the currently observed Workspace revision.
  const routeReady = !needsModel || (modelValid && match.isSuccess && !match.isFetching && match.data.matched === true);
  const recipe = chosen && routeReady ? clientRecipe(chosen, endpoints, model) : null;
  const copy = async () => {
    if (!recipe) return;
    try { await navigator.clipboard.writeText(recipe.body); setCopyState({ body: recipe.body, ok: true }); }
    catch { setCopyState({ body: recipe.body, ok: false }); }
  };
  if (kinds.length === 0) return null;
  return <section className="section" aria-label="Manual client setup">
    <div className="sectionhead"><h2>Configure another client</h2><span className="small muted">Manual setup · no files are modified</span></div>
    <div className="panel"><div className="panelbody formstack">
      <p className="fieldhint">For a client outside this Node, select the published address and configure that client on its own machine. These examples contain no issued secrets. <Link to={paths.workspaceAccess} params={{ slug: workspace.slug }}>Grant access and issue a Workspace credential</Link> first.</p>
      <div className="filterbar">
        <label>Where the client runs<select aria-label="Client location" value={where} onChange={event => { setWhere(event.target.value as 'local' | 'remote'); setCopyState(null); }}>
          <option value="local">This host / reachable local listener</option>
          <option value="remote">Another device / published relay</option>
        </select></label>
        <label>Client or protocol<select aria-label="Client configuration" value={chosen} onChange={event => { setKind(event.target.value as ClientRecipeKind); setCopyState(null); }}>
          {kinds.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select></label>
      </div>
      {needsModel && <label>Manual model ID<input list="manual-model-ids" value={model} onChange={event => { setModel(event.target.value); setCopyState(null); }} placeholder="Choose a model ID that matches a saved route"/>
        <datalist id="manual-model-ids">{exactRoutes.map(route => <option key={route} value={route}/>)}</datalist></label>}
      {needsModel && !modelValid && <p role="status" className="fieldhint">Enter one concrete model ID. Route patterns are not client model IDs.</p>}
      {needsModel && modelValid && endpoints.length > 0 && (match.isPending || match.isFetching ? <p className="fieldhint">Checking the saved Models route…</p> :
        match.isError ? <p className="formerror" role="alert">Cannot verify this model ID against current Workspace routes. <button type="button" className="btn" onClick={() => void match.refetch()}>Check again</button></p> :
        !match.data?.matched ? <p className="formerror" role="alert">No saved Models route matches this model ID. <Link to={paths.workspaceModels} params={{ slug: workspace.slug }}>Inspect routes</Link>.</p> :
        <p className="fieldhint">Model ID matches route <code>{match.data.pattern}</code>. This verifies routing configuration, not upstream availability.</p>)}
      {where === 'remote' && remote.length === 0 ? <p className="fieldhint" role="status">No valid published HTTPS address is available. Publish the Workspace and inspect its state before configuring another device.</p> :
        recipe ? <div className="connect-review" role="group" aria-label="Client configuration example">
          <h3>{recipe.title}</h3>
          <p className="fieldhint">{recipe.destination}</p>
          <pre className="mono" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxWidth: '100%', fontSize: '0.84rem' }}>{recipe.body}</pre>
          <p className="fieldhint">{recipe.note}</p>
          <div className="actions"><button type="button" className="btn" onClick={() => void copy()}>Copy example</button><Link className="textlink" to={paths.activity} search={{ workspace: workspace.slug, kind: chosen === 'mcp' ? 'mcp_operation' : 'exchange', cursor: undefined }}>Inspect real Activity ↗</Link></div>
          <p className="fieldhint" aria-live="polite">{copyState?.body === recipe.body ? copyState.ok ? 'Example copied. Supply the credential on the client machine before use.' : 'Clipboard unavailable. Select the example manually.' : recipe.sendsRequest ? 'Executing this command makes a real upstream request; it is not a connectivity-only test.' : 'Pasting this example does not prove that the client can connect.'}</p>
        </div> : <p className="fieldhint">Choose a configured Service and, for Models, a valid model ID to display setup instructions.</p>}
    </div></div>
  </section>;
}
