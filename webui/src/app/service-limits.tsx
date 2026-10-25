import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { getServiceLimits, setServiceLimits, type ServiceLimits, type ServiceLimitsSnapshot } from '../api/service-limits';
import { ControlRequestError } from '../api/transport';
import { queryKeys, type Workspace } from '../api/resources';
import { EmptyState, ErrorState, LoadState, errorMessage } from './control-ui';

export function WorkspaceOperationalLimits({ workspace, managed }: { workspace: Workspace; managed: boolean }) {
  const [version, setVersion] = useState(0);
  const [savedRevision, setSavedRevision] = useState<string>();
  const current = useQuery({ queryKey: ['workspace-service-limits', workspace.slug], queryFn: () => getServiceLimits(workspace.slug), retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false });
  const reload = async () => { const result = await current.refetch(); if (!result.isError) { setVersion(v => v + 1); setSavedRevision(undefined); } };
  return <section className="operational-limits" aria-label="Workspace operational limits">
    {current.isPending ? <LoadState/> : current.isError ? <ErrorState error={current.error} reload={() => void current.refetch()}/> :
      <OperationalLimitsEditor key={`${current.data.etag}:${version}`} workspace={workspace} current={current.data} managed={managed} reload={() => void reload()} saved={setSavedRevision}/>}
    {savedRevision && savedRevision === current.data?.etag && <p className="fieldhint" role="status">Workspace limits saved.</p>}
  </section>;
}

function OperationalLimitsEditor({ workspace, current, managed, reload, saved }: { workspace: Workspace; current: ServiceLimitsSnapshot; managed: boolean; reload: () => void; saved: (revision?: string) => void }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<ServiceLimits>(structuredClone(current.limits));
  const [changed, setChanged] = useState(false);
  const invalid = Object.values(draft).some(q => !Number.isInteger(q.requests_per_minute) || q.requests_per_minute < 0 || q.requests_per_minute > 100000) || !Number.isInteger(draft.models.concurrent ?? 0) || (draft.models.concurrent ?? 0) < 0 || (draft.models.concurrent ?? 0) > 10000;
  const mutation = useMutation({ retry: false, mutationFn: () => setServiceLimits(workspace.slug, current, draft),
    onSuccess: async result => {
      setChanged(false); saved(result.etag);
      qc.setQueryData(['workspace-service-limits', workspace.slug], result);
      await qc.invalidateQueries({ queryKey: queryKeys.workspace(workspace.slug) });
    },
  });
  const locked = !managed || mutation.isPending || mutation.isError;
  const edit = (service: keyof ServiceLimits, field: 'requests_per_minute' | 'concurrent', value: number) => {
    setDraft(old => ({ ...old, [service]: { ...old[service], [field]: value } })); setChanged(true); saved(undefined);
  };
  return <form onSubmit={event => { event.preventDefault(); if (!locked && !invalid && changed) mutation.mutate(); }}>
    <div className="sectionhead"><div><h2>Workspace limits</h2><p className="fieldhint">Every applicable independent ceiling is enforced across clients and Principals.</p></div>
      <button type="submit" className="btn primary" disabled={locked || !changed || invalid}>{mutation.isPending ? 'Saving…' : 'Save changes'}</button></div>
    <p className="fieldhint authority-context">Rate credit and active exchanges are local to this Node and reset on restart. Already admitted work may finish. These limits are independent of estimated USD spend and Principal permissions.</p>
    {!managed && <p className="fieldhint">This Node uses externally managed configuration. Edit service_limits in its configuration source.</p>}
    {workspace.enabled_services.length === 0 ? <EmptyState title="No enabled Services" detail="Enable a Workspace Service before editing its operational ceiling."/> :
      <div className="limitgrid">{workspace.enabled_services.map(service => <fieldset className="limiteditor" disabled={locked} key={service}>
        <legend>{service === 'models' ? 'Models' : service === 'mcp' ? 'MCP' : 'Direct Search'}</legend>
        <CeilingInput label={service === 'models' ? 'Requests / minute' : service === 'mcp' ? 'Tool calls / minute' : 'Queries / minute'} name={`${service}-rate`}
          value={draft[service].requests_per_minute} max={100000} change={value => edit(service, 'requests_per_minute', value)}/>
        {service === 'models' && <CeilingInput label="Concurrent exchanges" name="models-concurrent" value={draft.models.concurrent ?? 0} max={10000} change={value => edit('models', 'concurrent', value)}/>}
      </fieldset>)}</div>}
    {changed && !mutation.isError && <p className="fieldhint">Unsaved changes · applies to this Workspace only.</p>}
    {mutation.isError && <div className="formerror" role="alert">{mutation.error instanceof ControlRequestError && mutation.error.status === 503 ? 'The Node could not confirm whether the limits were saved.' : errorMessage(mutation.error)} Changes are locked until you read current state.
      <div className="actions"><button className="btn" type="button" onClick={reload}>Read current limits</button></div></div>}
    {changed && !mutation.isPending && !mutation.isError && <div className="actions"><button className="btn" type="button" onClick={() => { setDraft(structuredClone(current.limits)); setChanged(false); mutation.reset(); }}>Discard changes</button></div>}
  </form>;
}

function CeilingInput({ label, name, value, max, change }: { label: string; name: string; value: number; max: number; change: (value: number) => void }) {
  return <div className="operational-ceiling"><label htmlFor={name}>{label}</label>
    <select aria-label={`${label} policy`} value={value === 0 ? 'unlimited' : 'limited'} onChange={event => { change(event.target.value === 'unlimited' ? 0 : 1); }}>
      <option value="unlimited">Unlimited</option><option value="limited">Set ceiling</option></select>
    {value !== 0 && <input id={name} aria-label={label} type="number" min="1" max={max} step="1" required value={Number.isNaN(value) ? '' : value}
      onChange={event => { const n = event.target.valueAsNumber; change(n === 0 ? Number.NaN : n); }}/>}
  </div>;
}
