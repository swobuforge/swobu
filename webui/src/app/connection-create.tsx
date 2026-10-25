import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';
import { createConnection, newIntentKey, queryKeys, type Connection, type ConnectionCreate, type ConnectionKind } from '../api/resources';
import { ControlRequestError } from '../api/transport';
import { errorMessage, Modal } from './control-ui';
import { paths } from './routes';

// One Connection creation command for the global list and for in-place
// Workspace authoring. The caller owns whether to navigate or attach the new
// identity to its unsaved draft; creation never modifies Workspace state.
export function CreateConnectionDialog({ close, kind: fixedKind, onCreated }: {
  close: () => void;
  kind?: ConnectionKind;
  onCreated?: (connection: Connection) => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [name, setName] = useState('');
  const [kind, setKind] = useState<ConnectionKind>(fixedKind ?? 'model');
  const [provider, setProvider] = useState(fixedKind === 'search' ? 'brave' : fixedKind === 'mcp' ? '' : 'openai');
  const [endpoint, setEndpoint] = useState('');
  const [credential, setCredential] = useState('');
  const [executionIdentity, setExecutionIdentity] = useState('');
  const [permissionSummary, setPermissionSummary] = useState('');
  const [uncertain, setUncertain] = useState(false);
  const [intent, setIntent] = useState(newIntentKey);
  const changed = () => { setIntent(newIntentKey()); mutation.reset(); };
  function draft(): ConnectionCreate {
    if (kind === 'model') return { kind, display_name: name.trim(), spec: { shape: 'standard', provider_id: provider,
      configuration: { credential_ref: credential, ...(endpoint.trim() ? { endpoint: endpoint.trim() } : {}) } } };
    if (kind === 'mcp') return { kind, display_name: name.trim(), spec: { transport: 'streamable_http',
      execution_identity: executionIdentity.trim(), permission_summary: permissionSummary.trim(),
      configuration: { endpoint: endpoint.trim(), ...(credential.trim() ? { credential_ref: credential.trim() } : {}) } } };
    return { kind, display_name: name.trim(), spec: { provider_id: provider, endpoint: endpoint.trim(), ...(credential.trim() ? { credential_ref: credential.trim() } : {}) } };
  }
  const mutation = useMutation({ mutationFn: () => createConnection(draft(), intent), onSuccess: async connection => {
    // Creation itself returned an authoritative identity. Make it selectable
    // immediately, even when a list refresh is slow, then reconcile in place.
    qc.setQueryData<Connection[]>(queryKeys.connections, current => [
      ...(current ?? []).filter(item => item.id !== connection.id), connection,
    ]);
    void qc.invalidateQueries({ queryKey: queryKeys.connections });
    close();
    if (onCreated) onCreated(connection);
    else await navigate({ to: paths.connection, params: { id: connection.id } });
  }, onError: error => {
    // A missing acknowledgement can follow a committed creation. Preserve the
    // original fields and key until an explicit identical replay resolves it.
    if (!(error instanceof ControlRequestError) || error.status >= 500) setUncertain(true);
  } });
  const locked = mutation.isPending || uncertain;
  const valid = name.trim().length > 0 && (kind === 'model' ? credential.startsWith('env:') || credential.startsWith('keyring:') : endpoint.startsWith('https://') || endpoint.startsWith('http://127.0.0.1'));
  return <Modal id="new-connection-title" title="New Connection" close={close}>
    <form onSubmit={event => { event.preventDefault(); if (valid && !mutation.isPending) mutation.mutate(); }}><div className="modalbody formstack">
      <label>Name<input disabled={locked} data-autofocus required value={name} onChange={event => { setName(event.target.value); changed(); }} placeholder="OpenAI account"/></label>
      <label>Kind<select disabled={locked || fixedKind !== undefined} value={kind} onChange={event => { const next = event.target.value as ConnectionKind; setKind(next); setProvider(next === 'model' ? 'openai' : next === 'search' ? 'brave' : ''); changed(); }}>
        <option value="model">Models</option><option value="mcp">MCP</option><option value="search">Search</option></select></label>
      {kind !== 'mcp' && <label>Provider {kind === 'model' ? '(standard API)' : ''}<input disabled={locked} required value={provider} onChange={event => { setProvider(event.target.value); changed(); }} placeholder={kind === 'model' ? 'openai' : 'brave'}/></label>}
      {kind !== 'model' && <label>Endpoint<input disabled={locked} required type="url" value={endpoint} onChange={event => { setEndpoint(event.target.value); changed(); }} placeholder="https://example.com/api"/></label>}
      {kind === 'model' && <label>Override provider endpoint (optional)<input disabled={locked} type="url" value={endpoint} onChange={event => { setEndpoint(event.target.value); changed(); }} placeholder="Use provider default"/></label>}
      <label>Credential reference {kind === 'model' ? '' : '(optional)'}<input disabled={locked} required={kind === 'model'} value={credential} onChange={event => { setCredential(event.target.value); changed(); }}
        placeholder="env:OPENAI_API_KEY" autoComplete="off"/></label>
      {kind === 'mcp' && <><label>Upstream execution identity (declaration)<input disabled={locked} value={executionIdentity} onChange={event => { setExecutionIdentity(event.target.value); changed(); }}/></label>
        <label>Upstream permissions (declaration)<input disabled={locked} value={permissionSummary} onChange={event => { setPermissionSummary(event.target.value); changed(); }}/></label></>}
      <p className="fieldhint">This creates an upstream identity. Credential references point to secrets already available on the Node; this form does not upload secret material or mount a Connection into a Workspace.</p>
      {uncertain && <p className="fieldhint">The outcome is uncertain. Retry the same creation to recover its result. Closing this dialog discards this retry intent; inspect Connections before creating again.</p>}
      {mutation.isError && <p role="alert" className="formerror">{errorMessage(mutation.error)}</p>}
    </div><div className="modalfoot"><button className="btn" type="button" onClick={close}>Cancel</button><button className="btn primary" disabled={!valid || mutation.isPending} type="submit">
      {mutation.isPending ? 'Creating…' : mutation.isError ? 'Retry same creation' : 'Create Connection'}</button></div></form>
  </Modal>;
}
