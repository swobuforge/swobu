import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { ErrorState, EmptyState, Header, LoadState, Modal, errorMessage } from './control-ui';
import { getNodeOverview, listWorkspaceServices, listWorkspaces, queryKeys, type Workspace } from '../api/resources';
import {
  applyCredentialRevocation, applyLimit, applyPermission, setPublication,
  clearLimit, createPrincipal, formatCents, getPrincipal, issueCredential, listCredentials, listPrincipals,
  readPublication, listRemoteAddresses, newIntentKey, parseDollarCents, previewCredentialRevocation, previewLimit,
  previewPermission, readLimitStatus, renamePrincipal,
  type Credential, type CredentialReview, type IssueCredential, type Limit, type LimitApproval,
  type PermissionReview, type Principal, type ServiceGrant, type ServiceRequestLimit,
} from '../api/authority';
import { paths } from './routes';
import { WorkspaceOperationalLimits } from './service-limits';

const accessKeys = {
  principals: ['principals'] as const,
  principal: (id: string) => ['principal', id] as const,
  credentials: (id: string) => ['principal-credentials', id] as const,
  limits: (slug: string) => ['workspace-limits', slug] as const,
  publications: (slug: string) => ['workspace-publications', slug] as const,
};
function useConfigurationWritable() {
  const node = useQuery({ queryKey: queryKeys.node, queryFn: getNodeOverview });
  return node.data?.configuration_mode === 'managed';
}
function Failure({ error, inspect }: { error: unknown; inspect?: () => void }) {
  return <div className="formerror" role="alert">{errorMessage(error)}
    {inspect && <div className="actions"><button className="btn" type="button" onClick={inspect}>Inspect current state</button></div>}
  </div>;
}
function Impact({ label, value }: { label: string; value: string | number }) {
  return <div className="authority-impact"><span className="small muted">{label}</span><b>{value}</b></div>;
}
const serviceNames: Record<ServiceGrant, string> = { models: 'Models', mcp: 'MCP', search: 'Search' };
function permissionLabel(services?: ServiceGrant[]): string {
  return services ? services.map(service => serviceNames[service]).join(' + ') : 'All enabled Services (including future)';
}
function rateLabel(limits?: ServiceRequestLimit[]): string {
  return limits?.length ? limits.map(l => `${serviceNames[l.service]}: ${l.requests_per_minute.toLocaleString()} req/min`).join(' · ') : 'No per-Principal request limit';
}
function GrantNames({ permissions }: { permissions: Principal['permissions'] }) {
  return <span>{permissions.length ? permissions.map(p => `${p.workspace}: ${permissionLabel(p.services)}`).join(' · ') : 'No Workspace access'}</span>;
}

export function PrincipalsPage() {
  const managed = useConfigurationWritable();
  const principals = useQuery({ queryKey: accessKeys.principals, queryFn: listPrincipals });
  const [creating, setCreating] = useState(false);
  return <><Header title="Access" desc="Principals are Node identities. Workspace grants and credentials have separate lifecycles."
    actions={<button className="btn primary" type="button" disabled={!managed} onClick={() => setCreating(true)}>New Principal</button>}/>
    {principals.isPending ? <LoadState/> : principals.isError ? <ErrorState error={principals.error} reload={() => void principals.refetch()}/> :
      principals.data.length ? <div className="objectlist">{principals.data.map(principal => <Link key={principal.id} className="objectrow"
        to={paths.principal} params={{ principalId: principal.id }}>
        <div><b className="objecttitle">{principal.label}</b><div className="small muted mono">{principal.id}</div></div>
        <div><GrantNames permissions={principal.permissions}/></div><div><span className="state-tag">{principal.status}</span></div>
        <span className="textlink">Inspect ↗</span>
      </Link>)}</div> : <EmptyState title="No managed Principals" detail="Create a Principal, grant Workspace use, then issue a separate credential. A Principal alone has no access."/>}
    {creating && <CreatePrincipalDialog close={() => setCreating(false)}/>}
  </>;
}
function CreatePrincipalDialog({ close }: { close: () => void }) {
  const [label, setLabel] = useState('');
  const [key, setKey] = useState(newIntentKey);
  const navigate = useNavigate();
  const qc = useQueryClient();
  const create = useMutation({ mutationFn: () => createPrincipal(label.trim(), key), onSuccess: async principal => {
    await qc.invalidateQueries({ queryKey: accessKeys.principals });
    close(); await navigate({ to: paths.principal, params: { principalId: principal.id } });
  } });
  return <Modal id="principal-create-title" title="New Principal" close={close}>
    <form onSubmit={e => { e.preventDefault(); create.mutate(); }}><div className="modalbody formstack">
      <label>Display label<input autoFocus maxLength={120} value={label} placeholder="Build agent" onChange={e => { setLabel(e.target.value); setKey(newIntentKey()); create.reset(); }}/></label>
      <p className="fieldhint">Creating an identity does not grant access or issue a bearer secret. Labels do not verify real-world identity.</p>
      {create.isError && <Failure error={create.error} inspect={() => { void qc.invalidateQueries({ queryKey: accessKeys.principals }); }}/>}</div>
      <div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
        <button className="btn primary" type="submit" disabled={!label.trim() || create.isPending}>{create.isPending ? 'Creating…' : 'Create Principal'}</button></div>
    </form>
  </Modal>;
}

export function WorkspaceAccess({ workspace }: { workspace: Workspace }) {
  const managed = useConfigurationWritable();
  const principals = useQuery({ queryKey: accessKeys.principals, queryFn: listPrincipals });
  const services = useQuery({ queryKey: queryKeys.services(workspace.slug), queryFn: () => listWorkspaceServices(workspace.slug) });
  const enabled = (services.data ?? []).filter(s => s.enabled).map(s => s.kind);
  const [now] = useState(() => Date.now());
  const [target, setTarget] = useState<{ principal: Principal; grant: boolean } | null>(null);
  const [selecting, setSelecting] = useState(false);
  const active = (principals.data ?? []).filter(p => p.permissions.some(g => g.workspace === workspace.slug));
  const available = (principals.data ?? []).filter(p => p.status === 'active' && !p.permissions.some(g =>
    g.workspace === workspace.slug && (!g.expires_at || Date.parse(g.expires_at) > now)));
  return <><div className="sectionhead"><h2>Workspace access</h2><button type="button" className="btn primary"
    disabled={!managed || principals.isPending || principals.isError || services.isPending || services.isError || !enabled.length || !available.length} onClick={() => setSelecting(true)}>Grant access</button></div>
    <p className="fieldhint authority-context">Select only the Services a Principal needs. A credential remains bound to its Workspace; its current Service permissions are rechecked for every operation. Credentials are issued independently.</p>
    {principals.isPending ? <LoadState/> : principals.isError ? <ErrorState error={principals.error} reload={() => void principals.refetch()}/> :
      active.length ? <div className="objectlist">{active.map(p => { const grant = p.permissions.find(g => g.workspace === workspace.slug)!;
        return <div className="objectrow" key={p.id}><div><Link to={paths.principal} params={{ principalId: p.id }} className="textlink">{p.label}</Link>
          <div className="small muted mono">{p.id}</div></div><div>{permissionLabel(grant.services)}</div>
          <div className="small muted">{rateLabel(grant.request_limits)} · {grant.expires_at ? (Date.parse(grant.expires_at) <= now ? 'Expired' : `Expires ${new Date(grant.expires_at).toLocaleString()}`) : 'No expiry'}</div>
          <div className="actions"><button className="btn" disabled={!managed || p.status !== 'active' || services.isPending || services.isError || !enabled.length} onClick={() => setTarget({ principal: p, grant: true })}>Edit Services</button>
          <button className="btn danger" disabled={!managed || p.status !== 'active'} onClick={() => setTarget({ principal: p, grant: false })}>Revoke</button></div></div>;
      })}</div> : <EmptyState title="No Workspace grants" detail="Only the local host Operator can administer this Workspace until a Principal is granted use."/>}
    {selecting && <Modal id="choose-principal-title" title="Grant Workspace use" close={() => setSelecting(false)}>
      <div className="modalbody"><p className="fieldhint">Choose an existing Principal. Create new Principals from Access before granting them Workspace use.</p>
        <div className="authority-selection">{available.map(p => <button key={p.id} type="button" className="authority-select" onClick={() => { setSelecting(false); setTarget({ principal: p, grant: true }); }}>
          <b>{p.label}</b><span className="small muted mono">{p.id}</span></button>)}
        {!available.length && <EmptyState title="No eligible Principals" detail="Create another managed Principal from Access."/>}</div></div>
      <div className="modalfoot"><button type="button" className="btn" onClick={() => setSelecting(false)}>Close</button>
        <Link className="btn" to={paths.principals} onClick={() => setSelecting(false)}>Open Access ↗</Link></div>
    </Modal>}
    {target && <PermissionDialog workspace={workspace.slug} principal={target.principal} grant={target.grant} enabled={enabled} close={() => setTarget(null)}/>}
  </>;
}
function PermissionDialog({ workspace, principal, grant, enabled, close }: { workspace: string; principal: Principal; grant: boolean; enabled: ServiceGrant[]; close: () => void }) {
  const qc = useQueryClient();
  const current = principal.permissions.find(p => p.workspace === workspace);
  const [selected, setSelected] = useState<ServiceGrant[]>(() => current?.services?.filter(s => enabled.includes(s)) ?? (enabled.includes('models') ? ['models'] : enabled.slice(0, 1)));
  const [rateInputs, setRateInputs] = useState<Record<ServiceGrant, string>>(() => Object.fromEntries(
    (['models', 'mcp', 'search'] as ServiceGrant[]).map(service => [service, String(current?.request_limits?.find(x => x.service === service)?.requests_per_minute ?? '')]),
  ) as Record<ServiceGrant, string>);
  const draftRates: ServiceRequestLimit[] = selected.flatMap(service => rateInputs[service].trim()
    ? [{ service, requests_per_minute: Number(rateInputs[service]) }] : []);
  const validRates = draftRates.every(rate => Number.isInteger(rate.requests_per_minute) && rate.requests_per_minute >= 1 && rate.requests_per_minute <= 100000);
  const [review, setReview] = useState<PermissionReview | null>(null);
  const [key, setKey] = useState(newIntentKey);
  const preview = useMutation({ mutationFn: () => previewPermission(workspace, principal.id, grant, current?.expires_at, grant ? selected : undefined, grant ? draftRates : undefined), onSuccess: setReview });
  const toggle = (kind: ServiceGrant) => {
    setSelected(before => enabled.filter(s => s === kind ? !before.includes(kind) : before.includes(s)));
    setReview(null); setKey(newIntentKey()); preview.reset(); apply.reset();
  };
  const apply = useMutation({ mutationFn: () => applyPermission(review!, key), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: accessKeys.principals }), qc.invalidateQueries({ queryKey: accessKeys.principal(principal.id) }),
      qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace) }), qc.invalidateQueries({ queryKey: queryKeys.node })]);
    close();
  } });
  return <Modal id="permission-title" title={`${grant ? 'Grant' : 'Revoke'} use · ${principal.label}`} close={close}>
    <div className="modalbody formstack"><p className="fieldhint">Workspace: <b>{workspace}</b>. Updating a grant changes access immediately for all this Principal’s existing Workspace credentials; it does not issue or revoke tokens.</p>
      {!review ? <>{grant ? <><p>Select the exact allowed Services:</p>
          <fieldset className="authority-choice"><legend>Allowed Services</legend>{enabled.map(kind =>
            <label key={kind}><input type="checkbox" checked={selected.includes(kind)} onChange={() => toggle(kind)}/> {serviceNames[kind]}</label>)}</fieldset>
          <p className="fieldhint">Unselected Services are denied, even if enabled later. An empty set cannot be granted.</p>
          <div className="formstack"><b>Per-Principal request ceilings</b><p className="fieldhint">Optional requests/minute per enabled Service, shared by all of this Principal's credentials. Blank means no ceiling. These are live operation rates, not dollar/token budgets.</p>
            {selected.map(service => <label key={service}>{serviceNames[service]} requests / minute
              <input type="number" min={1} max={100000} step={1} value={rateInputs[service]} placeholder="No ceiling" onChange={event => {
                setRateInputs(before => ({ ...before, [service]: event.target.value })); setReview(null); setKey(newIntentKey()); preview.reset(); apply.reset();
              }}/></label>)}
            {!validRates && <p role="alert" className="formerror">Limits must be whole numbers from 1 to 100,000.</p>}
          </div></> :
          <p>Remove this Principal’s ability to make new Workspace requests?</p>}
        {preview.isError && <Failure error={preview.error} inspect={() => void qc.invalidateQueries({ queryKey: accessKeys.principals })}/>}
        <button className="btn" type="button" disabled={preview.isPending || (grant && (!selected.length || !validRates))} onClick={() => preview.mutate()}>Review {grant ? 'grant' : 'revocation'}</button></> :
        <><Impact label="Before" value={review.before ? permissionLabel(review.before.services) : 'No grant'}/><Impact label="After" value={review.after ? permissionLabel(review.after.services) : 'No grant'}/>
          <Impact label="Request ceilings before" value={rateLabel(review.before?.request_limits)}/>
          <Impact label="Request ceilings after" value={rateLabel(review.after?.request_limits)}/>
          <Impact label="Affected credentials" value={review.affected_credentials}/><p className="fieldhint">{review.warning}</p>
          {apply.isError && <Failure error={apply.error} inspect={() => { setReview(null); setKey(newIntentKey()); apply.reset(); void qc.invalidateQueries({ queryKey: accessKeys.principals }); }}/>}</>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
      {review && <button type="button" className={`btn ${grant ? 'primary' : 'danger'}`} disabled={apply.isPending}
        onClick={() => apply.mutate()}>{apply.isPending ? 'Applying…' : grant ? 'Confirm grant' : 'Confirm revocation'}</button>}</div>
  </Modal>;
}

export function PrincipalPage({ id }: { id: string }) {
  const managed = useConfigurationWritable();
  const principal = useQuery({ queryKey: accessKeys.principal(id), queryFn: () => getPrincipal(id) });
  const credentials = useQuery({ queryKey: accessKeys.credentials(id), queryFn: () => listCredentials(id) });
  const [now] = useState(() => Date.now());
  const [editing, setEditing] = useState(false);
  const [issuing, setIssuing] = useState(false);
  const [revoking, setRevoking] = useState<Credential | null>(null);
  if (principal.isPending) return <><Header title="Principal" back={{ href: paths.principals, label: 'Access' }}/><LoadState/></>;
  if (principal.isError) return <><Header title={id} back={{ href: paths.principals, label: 'Access' }}/><ErrorState error={principal.error} reload={() => void principal.refetch()}/></>;
  const p = principal.data;
  const usablePermissions = p.permissions.filter(g => !g.expires_at || Date.parse(g.expires_at) > now);
  return <><Header title={p.label} desc={`${p.kind} Principal · ${p.status}`} back={{ href: paths.principals, label: 'Access' }}
    actions={<button className="btn" type="button" disabled={!managed || p.status !== 'active'} onClick={() => setEditing(true)}>Rename</button>}/>
    <dl className="metadata"><dt>Principal ID</dt><dd className="mono">{p.id}</dd><dt>Status</dt><dd>{p.status}</dd><dt>Authority</dt><dd>Workspace use permissions only. Not Node administration.</dd></dl>
    <div className="section"><div className="sectionhead"><h2>Workspace grants</h2></div>
      {p.permissions.length ? <div className="objectlist">{p.permissions.map(g => <div className="objectrow" key={g.workspace}>
        <div><Link to={paths.workspaceAccess} params={{ slug: g.workspace }} className="textlink">{g.workspace}</Link></div>
        <div>{permissionLabel(g.services)}</div><div className="small muted">{g.expires_at ? `Expires ${new Date(g.expires_at).toLocaleString()}` : 'No expiry'}</div>
        <Link className="textlink" to={paths.workspaceAccess} params={{ slug: g.workspace }}>Manage ↗</Link></div>)}</div> :
        <EmptyState title="No grants" detail="Grant Workspace use before issuing a credential. A Principal by itself cannot call a Workspace."/>}
    </div>
    <div className="section"><div className="sectionhead"><h2>Workspace credentials</h2><button className="btn primary" type="button"
      disabled={!managed || p.status !== 'active' || !usablePermissions.length} onClick={() => setIssuing(true)}>Issue credential</button></div>
      <p className="fieldhint authority-context">Bearer secrets are shown only once. Revoking one credential leaves other credentials and grants intact.</p>
      {credentials.isPending ? <LoadState/> : credentials.isError ? <ErrorState error={credentials.error} reload={() => void credentials.refetch()}/> :
        credentials.data.length ? <div className="objectlist">{credentials.data.map(c => <div className="objectrow" key={c.id}>
          <div><b className="mono">{c.id}</b></div><div>{c.workspace}</div><div className="small muted">Issued {new Date(c.created_at).toLocaleString()}</div>
          <button className="btn danger" disabled={!managed} type="button" onClick={() => setRevoking(c)}>Revoke</button></div>)}</div> :
          <EmptyState title="No active credentials" detail="Issue a credential for a Workspace where this Principal has use permission."/>}
    </div>
    {editing && <RenamePrincipalDialog principal={p} close={() => setEditing(false)}/>}
    {issuing && <IssueCredentialDialog principal={p} close={() => setIssuing(false)}/>}
    {revoking && <RevokeCredentialDialog principal={p} credential={revoking} close={() => setRevoking(null)}/>}
  </>;
}
function RenamePrincipalDialog({ principal, close }: { principal: Principal; close: () => void }) {
  const qc = useQueryClient();
  const [label, setLabel] = useState(principal.label);
  const mutation = useMutation({ mutationFn: () => renamePrincipal(principal, label.trim()), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: accessKeys.principals }), qc.invalidateQueries({ queryKey: accessKeys.principal(principal.id) })]); close();
  } });
  return <Modal id="rename-principal-title" title="Principal label" close={close}><form onSubmit={e => { e.preventDefault(); mutation.mutate(); }}>
    <div className="modalbody formstack"><label>Label<input autoFocus value={label} maxLength={120} onChange={e => setLabel(e.target.value)}/></label>
      <p className="fieldhint">Changing this label does not change credentials, grants or the Principal ID.</p>
      {mutation.isError && <Failure error={mutation.error} inspect={() => void qc.invalidateQueries({ queryKey: accessKeys.principal(principal.id) })}/>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button><button className="btn primary" type="submit"
      disabled={!label.trim() || label.trim() === principal.label || mutation.isPending}>Save label</button></div></form></Modal>;
}
function IssueCredentialDialog({ principal, close }: { principal: Principal; close: () => void }) {
  const qc = useQueryClient();
  const [now] = useState(() => Date.now());
  const usablePermissions = principal.permissions.filter(g => !g.expires_at || Date.parse(g.expires_at) > now);
  const [workspace, setWorkspace] = useState(usablePermissions[0]?.workspace || '');
  const [key, setKey] = useState(newIntentKey);
  const issue = useMutation({ mutationFn: () => issueCredential(principal.id, workspace, key), onSuccess: () => {
    void qc.invalidateQueries({ queryKey: accessKeys.credentials(principal.id) });
  } });
  const result: IssueCredential | undefined = issue.data;
  return <Modal id="issue-credential-title" title="Issue Workspace credential" close={close}>
    <div className="modalbody formstack">{!result ? <><label>Workspace<select value={workspace} onChange={e => { setWorkspace(e.target.value); setKey(newIntentKey()); issue.reset(); }}>
      {usablePermissions.map(p => <option key={p.workspace} value={p.workspace}>{p.workspace}</option>)}
    </select></label><p className="fieldhint">Issued token is scoped to this Principal and Workspace. It is never stored in the browser beyond this dialog.</p>
      {issue.isError && <Failure error={issue.error} inspect={() => { void qc.invalidateQueries({ queryKey: accessKeys.credentials(principal.id) }); }}/>}</> :
      <><Impact label="Credential ID" value={result.credential.id}/>
        {result.token && !result.secret_not_recoverable ? <><label>One-time bearer token<textarea className="authority-secret" readOnly rows={3} value={result.token}/></label>
          <button className="btn" type="button" onClick={() => void navigator.clipboard.writeText(result.token!)}>Copy credential</button>
          <p className="fieldhint">Copy it now. Closing this dialog discards the displayed secret; Swobu cannot reveal it again.</p></> :
          <p className="fieldhint">This issuance was already recorded, but its one-time secret cannot be recovered. Inspect credentials and issue a new one if needed.</p>}</>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>{result ? 'Done' : 'Cancel'}</button>
      {!result && <button className="btn primary" type="button" disabled={!workspace || issue.isPending} onClick={() => issue.mutate()}>
        {issue.isPending ? 'Issuing…' : 'Issue credential'}</button>}</div>
  </Modal>;
}
function RevokeCredentialDialog({ principal, credential, close }: { principal: Principal; credential: Credential; close: () => void }) {
  const qc = useQueryClient();
  const [review, setReview] = useState<CredentialReview | null>(null);
  const [key, setKey] = useState(newIntentKey);
  const preview = useMutation({ mutationFn: () => previewCredentialRevocation(principal.id, credential.id), onSuccess: setReview });
  const remove = useMutation({ mutationFn: () => applyCredentialRevocation(review!, key), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: accessKeys.credentials(principal.id) }), qc.invalidateQueries({ queryKey: accessKeys.principal(principal.id) })]); close();
  } });
  return <Modal id="credential-revoke-title" title="Revoke one credential" close={close}>
    <div className="modalbody formstack"><Impact label="Credential" value={credential.id}/><Impact label="Workspace" value={credential.workspace}/>
      {!review ? <><p className="fieldhint">This does not revoke Workspace permissions or any sibling credential.</p>
        {preview.isError && <Failure error={preview.error}/>}
        <button className="btn" type="button" disabled={preview.isPending} onClick={() => preview.mutate()}>Review revocation</button></> :
        <><p className="fieldhint">{review.warning}</p>{remove.isError && <Failure error={remove.error} inspect={() => { setReview(null); setKey(newIntentKey()); void qc.invalidateQueries({ queryKey: accessKeys.credentials(principal.id) }); }}/>}</>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
      {review && <button className="btn danger" type="button" disabled={remove.isPending} onClick={() => remove.mutate()}>
        {remove.isPending ? 'Revoking…' : 'Confirm revocation'}</button>}</div>
  </Modal>;
}

export function WorkspaceLimits({ workspace }: { workspace: Workspace }) {
  const managed = useConfigurationWritable();
  const qc = useQueryClient();
  const snapshot = useQuery({ queryKey: accessKeys.limits(workspace.slug), queryFn: () => readLimitStatus(workspace.slug) });
  const [editing, setEditing] = useState(false);
  const [clearing, setClearing] = useState(false);
  const clear = useMutation({ mutationFn: () => clearLimit(workspace.slug, snapshot.data!.revision), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: accessKeys.limits(workspace.slug) }), qc.invalidateQueries({ queryKey: queryKeys.node })]); setClearing(false);
  } });
  return <><WorkspaceOperationalLimits workspace={workspace} managed={managed}/><div className="sectionhead section"><h2>Spending policy</h2><div className="actions"><button className="btn" type="button" disabled={!managed || !snapshot.data || (!!snapshot.data.status.limit && !Number.isSafeInteger(snapshot.data.status.limit.amount_cents))} onClick={() => setEditing(true)}>
    {snapshot.data?.status.limit ? 'Change policy' : 'Set policy'}</button>{snapshot.data?.status.limit && <button className="btn danger" type="button" disabled={!managed} onClick={() => setClearing(true)}>Clear policy</button>}</div></div>
    {snapshot.isPending ? <LoadState/> : snapshot.isError ? <ErrorState error={snapshot.error} reload={() => void snapshot.refetch()}/> : <>
      <p className="fieldhint authority-context">Policy admission is independent from Principal grants. These figures include conservative reservations and uncertain outcomes, not confirmed provider invoices.</p>
      <dl className="metadata"><dt>Policy</dt><dd>{snapshot.data.status.limit ? `${formatCents(snapshot.data.status.limit.amount_cents)} / ${snapshot.data.status.limit.window} · ${snapshot.data.status.limit.mode === 'observe_only' ? 'Observe only' : 'Stop new billable work'}` : 'No spending ceiling'}</dd>
        <dt>Current window</dt><dd>{snapshot.data.status.window_key}</dd>
        <dt>Reserved</dt><dd>{formatCents(snapshot.data.status.reserved_cents)}</dd><dt>Settled</dt><dd>{formatCents(snapshot.data.status.settled_cents)}</dd>
        <dt>Uncertain</dt><dd>{formatCents(snapshot.data.status.uncertain_cents)}</dd>
        <dt>Charged estimate</dt><dd>{formatCents(snapshot.data.status.charged_cents)}</dd>
        <dt>Remaining budget</dt><dd>{snapshot.data.status.remaining_cents === null ? 'Unlimited / no policy' : formatCents(snapshot.data.status.remaining_cents)}</dd>
        <dt>Unknown-price operations</dt><dd>{snapshot.data.status.unknown_price_operations}</dd>
        <dt>Incomplete usage</dt><dd>{snapshot.data.status.partial_quantity_operations} partial · {snapshot.data.status.unquoted_operations} unquoted</dd>
      </dl>
    </>}
    {editing && <LimitDialog slug={workspace.slug} current={snapshot.data?.status.limit ?? null} close={() => setEditing(false)}/>}
    {clearing && snapshot.data && <Modal id="clear-limit-title" title="Clear spending policy" close={() => setClearing(false)}>
      <div className="modalbody formstack"><p>Remove the spending ceiling from <b>{workspace.slug}</b>? This does not revoke client permission, release unsettled reservations or erase accounting history.</p>
        {clear.isError && <Failure error={clear.error} inspect={() => { setClearing(false); void qc.invalidateQueries({ queryKey: accessKeys.limits(workspace.slug) }); }}/>}</div>
      <div className="modalfoot"><button className="btn" type="button" onClick={() => setClearing(false)}>Cancel</button>
        <button className="btn danger" type="button" disabled={clear.isPending} onClick={() => clear.mutate()}>Confirm clear policy</button></div>
    </Modal>}
  </>;
}
function LimitDialog({ slug, current, close }: { slug: string; current: Limit | null; close: () => void }) {
  const qc = useQueryClient();
  const [amount, setAmount] = useState(current ? String(current.amount_cents / 100) : '10.00');
  const [window, setWindow] = useState<'day' | 'month'>(current?.window ?? 'day');
  const [mode, setMode] = useState<Limit['mode']>(current?.mode ?? 'stop_new_billable_work');
  const [approval, setApproval] = useState<LimitApproval | null>(null);
  const cents = parseDollarCents(amount);
  const limit: Limit | null = cents === null ? null : { currency: 'USD', amount_cents: cents, window, mode, unknown_price: 'reject' };
  const resetReview = () => { setApproval(null); preview.reset(); apply.reset(); };
  const preview = useMutation({ mutationFn: () => previewLimit(slug, limit!), onSuccess: setApproval });
  const apply = useMutation({ mutationFn: () => applyLimit(approval!, limit!), onSuccess: async () => {
    await Promise.all([qc.invalidateQueries({ queryKey: accessKeys.limits(slug) }), qc.invalidateQueries({ queryKey: queryKeys.node })]); close();
  } });
  return <Modal id="limit-editor-title" title="Workspace spending policy" close={close}>
    <div className="modalbody formstack"><label>Budget (USD)<input autoFocus inputMode="decimal" value={amount} onChange={e => { setAmount(e.target.value); resetReview(); }}/></label>
      <label>Window<select value={window} onChange={e => { setWindow(e.target.value as 'day'|'month'); resetReview(); }}><option value="day">Daily</option><option value="month">Monthly</option></select></label>
      <label>Enforcement<select value={mode} onChange={e => { setMode(e.target.value as Limit['mode']); resetReview(); }}>
        <option value="stop_new_billable_work">Block new billable work at limit</option><option value="observe_only">Observe only (does not block)</option></select></label>
      <p className="fieldhint">Unknown pricing rejects admission by policy. Budget does not cover every kind of usage with perfect certainty. Already executed work is never undone.</p>
      {!approval ? <>{preview.isError && <Failure error={preview.error} inspect={() => void qc.invalidateQueries({ queryKey: accessKeys.limits(slug) })}/>}</> :
        <><Impact label="Reviewed ceiling" value={`${formatCents(limit!.amount_cents)} / ${window}`}/>
          <Impact label="Enforcement" value={mode === 'observe_only' ? 'Observe, do not block' : 'Stop new billable work'}/>
          <p className="fieldhint">Approval expires at {new Date(approval.expires_at).toLocaleString()}. Applying rechecks the current accounting revision.</p>
          {apply.isError && <Failure error={apply.error} inspect={() => { resetReview(); void qc.invalidateQueries({ queryKey: accessKeys.limits(slug) }); }}/>}</>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button>
      {!approval ? <button className="btn" type="button" disabled={!limit || preview.isPending} onClick={() => preview.mutate()}>Review policy</button> :
        <button className="btn primary" type="button" disabled={apply.isPending} onClick={() => apply.mutate()}>
          {apply.isPending ? 'Applying…' : 'Confirm policy'}</button>}</div>
  </Modal>;
}

export function WorkspacePublication({ workspace }: { workspace: Workspace }) {
  const managed = useConfigurationWritable();
  const qc = useQueryClient();
  const query = useQuery({ queryKey: accessKeys.publications(workspace.slug), queryFn: () => readPublication(workspace.slug) });
  const mutation = useMutation({ mutationFn: (published: boolean) => setPublication(query.data!, published),
    onSuccess: async () => { await Promise.all([
      qc.invalidateQueries({ queryKey: accessKeys.publications(workspace.slug) }),
      qc.invalidateQueries({ queryKey: queryKeys.node })]); } });
  const publication = query.data;
  return <div className="section"><div className="sectionhead"><h2>Remote access</h2></div>
    <p className="fieldhint authority-context">Publishing makes all enabled Workspace Services, including MCP, addressable through the Swobu relay. Existing credentials, grants and limits still apply. If swobu-admin is mounted, Principals granted MCP access can invoke its allowed Node administration tools.</p>
    {query.isPending ? <LoadState/> : query.isError ? <ErrorState error={query.error} reload={() => void query.refetch()}/> :
      publication && <div className="objectrow"><div><b>{publication.published ? 'Published' : 'Local only'}</b>
          <div className="small mono muted">{publication.url ? `${publication.url}/workspaces/${workspace.slug}` : `/workspaces/${workspace.slug}`}</div></div>
        <div>Workspace credential required</div>
        <button className={`btn ${publication.published ? 'danger' : 'primary'}`} type="button" disabled={!managed || mutation.isPending}
          onClick={() => mutation.mutate(!publication.published)}>{mutation.isPending ? 'Applying…' : publication.published ? 'Unpublish' : 'Publish via relay'}</button></div>}
    {mutation.isError && <Failure error={mutation.error} inspect={() => void query.refetch()}/>}
  </div>;
}

export function NetworkPublicationSummary() {
  const addresses = useQuery({ queryKey: ['remote-addresses'], queryFn: listRemoteAddresses });
  const workspaces = useQuery({ queryKey: queryKeys.workspaces, queryFn: listWorkspaces });
  return <div className="section"><div className="sectionhead"><h2>Remote address</h2></div>
    <p className="fieldhint authority-context">Swobu manages one built-in relay address. Publishing a Workspace uses this address; it does not create separate permissions or Services.</p>
    {addresses.isPending ? <LoadState/> : addresses.isError ? <ErrorState error={addresses.error} reload={() => void addresses.refetch()}/> :
      addresses.data.map(address => <div className="objectrow" key={address.id}><div><b>Swobu relay</b></div>
        <div className="mono muted">{address.url || 'Address generated on first publication'}</div>
        <div>Managed by Swobu</div></div>)}
    <div className="sectionhead" style={{ marginTop: 28 }}><h2>Workspaces</h2></div>
    {workspaces.isPending ? <LoadState/> : workspaces.isError ? <ErrorState error={workspaces.error} reload={() => void workspaces.refetch()}/> :
      workspaces.data.length ? <div className="objectlist">{workspaces.data.map(w => <Link className="objectrow" key={w.slug}
        to={paths.workspaceConnect} params={{ slug: w.slug }}><div><b>{w.display_name || w.slug}</b></div>
        <div className="mono">/workspaces/{w.slug}</div><span className="textlink">Configure ↗</span></Link>)}</div> :
        <EmptyState title="No Workspaces" detail="Create a Workspace before publishing it."/>}
  </div>;
}
