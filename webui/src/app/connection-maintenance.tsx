import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { ControlRequestError } from '../api/transport';
import {
  editConnection, newIntentKey, queryKeys, replaceConnectionCredential,
  type Connection,
} from '../api/resources';
import { errorMessage, Modal } from './control-ui';
import { completeDraft, initialFields, type Fields } from './connection-maintenance-draft';

async function refreshConnection(qc: ReturnType<typeof useQueryClient>, connection: Connection): Promise<void> {
  // Invalidate observed revisions and dependent catalog/Workspace projections.
  // Drafts on other pages retain their own revision and must still review again.
  await Promise.all([
    qc.invalidateQueries({ queryKey: queryKeys.connections }),
    qc.invalidateQueries({ queryKey: queryKeys.connection(connection.id) }),
    qc.invalidateQueries({ queryKey: queryKeys.references(connection.id) }),
    qc.invalidateQueries({ queryKey: queryKeys.catalog(connection.id) }),
    qc.invalidateQueries({ queryKey: ['workspace'] }),
    qc.invalidateQueries({ queryKey: ['workspace-services'] }),
  ]);
}

export function EditConnectionDialog({ connection, close }: { connection: Connection; close: () => void }) {
  const qc = useQueryClient();
  const [fields, setFields] = useState(() => initialFields(connection));
  const [confirmed, setConfirmed] = useState(false);
  const [frozen, setFrozen] = useState(false);
  const [localError, setLocalError] = useState('');
  const update = (field: keyof Fields, value: string) => {
    setFields(previous => ({ ...previous, [field]: value }));
    setLocalError('');
  };
  const closeAfterFailure = () => {
    if (frozen) void qc.invalidateQueries({ queryKey: queryKeys.connection(connection.id) });
    close();
  };
  const save = useMutation({ gcTime: 0, mutationFn: () => editConnection(connection, completeDraft(connection, fields)),
    onSuccess: async () => { await refreshConnection(qc, connection); close(); },
    onError: () => setFrozen(true),
  });
  const spec = connection.spec;
  return <Modal id="edit-connection-title" title={`Edit ${connection.display_name}`} close={closeAfterFailure}>
    <form onSubmit={event => {
      event.preventDefault();
      try { completeDraft(connection, fields); setLocalError(''); save.mutate(); }
      catch (error) { setLocalError(errorMessage(error)); }
    }}>
      <div className="modalbody formstack">
        <p className="fieldhint">This replaces the complete Connection configuration at revision <code>{connection.revision}</code>, without changing its ID or Workspace references. Re-enter endpoints, credential references, custom headers and stdio arguments: the read response intentionally redacts private details, including parts of endpoint URLs. For a secret-only rotation, cancel and use Replace credential instead.</p>
        <label>Name<input data-autofocus required value={fields.name} disabled={frozen || save.isPending} onChange={event => update('name', event.target.value)}/></label>
        <p className="fieldhint">{connection.kind === 'model' ? `Model provider: ${connection.spec.provider_id}` : connection.kind === 'search' ? `Search provider: ${connection.spec.provider_id}` : `MCP transport: ${connection.spec.transport}`} · identity cannot change in place.</p>
        {('endpoint' in spec || (connection.kind === 'model' && connection.spec.shape === 'standard')) && <label>Endpoint {connection.kind === 'model' && connection.spec.shape === 'standard' ? '(optional)' : ''}<input type="url" value={fields.endpoint} disabled={frozen || save.isPending} onChange={event => update('endpoint', event.target.value)}/></label>}
        {'region' in spec && <label>Region<input required value={fields.region} disabled={frozen || save.isPending} onChange={event => update('region', event.target.value)}/></label>}
        {'access' in spec && <label>Access<select value={fields.access} disabled={frozen || save.isPending} onChange={event => update('access', event.target.value)}><option value="general_api">General API</option><option value="coding_plan">Coding plan</option></select></label>}
        {connection.kind === 'model' && connection.spec.shape === 'custom' && <label>Custom auth header (re-enter when applicable)<input value={fields.header} disabled={frozen || save.isPending} onChange={event => update('header', event.target.value)} autoComplete="off"/></label>}
        {connection.kind === 'mcp' && connection.spec.transport === 'stdio' && <>
          <label>Executable (full path)<input required value={fields.executable} disabled={frozen || save.isPending} onChange={event => update('executable', event.target.value)} autoComplete="off"/></label>
          <label>Arguments (one exact argument per line)<textarea rows={3} value={fields.args} disabled={frozen || save.isPending} onChange={event => update('args', event.target.value)} autoComplete="off"/></label>
          <label>Environment bindings (NAME=credential-reference per line)<textarea rows={3} value={fields.environment} disabled={frozen || save.isPending} onChange={event => update('environment', event.target.value)} autoComplete="off"/></label>
        </>}
        {!(connection.kind === 'mcp' && connection.spec.transport === 'stdio') && <label>Credential reference (re-enter; never disclosed on read)<input value={fields.credential} disabled={frozen || save.isPending} onChange={event => update('credential', event.target.value)} placeholder="env:API_KEY" autoComplete="off"/></label>}
        {connection.kind === 'mcp' && <>
          <label>Declared execution identity<input value={fields.executionIdentity} disabled={frozen || save.isPending} onChange={event => update('executionIdentity', event.target.value)}/></label>
          <label>Declared permissions<textarea rows={2} value={fields.permissionSummary} disabled={frozen || save.isPending} onChange={event => update('permissionSummary', event.target.value)}/></label>
        </>}
        <label className="checkline"><input type="checkbox" checked={confirmed} disabled={frozen || save.isPending} onChange={event => setConfirmed(event.target.checked)}/>
          I confirm this remains the same external account or server and I am replacing the complete private configuration.</label>
        {localError && <p role="alert" className="formerror">{localError}</p>}
        {save.isError && <div className="formerror" role="alert">{errorMessage(save.error)}<p>No automatic retry: inspect the current Connection before another edit.</p></div>}
      </div>
      <div className="modalfoot"><button type="button" className="btn" onClick={closeAfterFailure}>{frozen ? 'Inspect current Connection' : 'Cancel'}</button>
        <button type="submit" className="btn primary" disabled={!confirmed || frozen || save.isPending}>{save.isPending ? 'Saving…' : 'Save Connection'}</button></div>
    </form>
  </Modal>;
}

function hasCredential(connection: Connection): boolean {
  if (connection.kind === 'mcp' && connection.spec.transport === 'stdio') return connection.spec.environment_binding_count > 0;
  return 'authentication_configured' in connection.spec && connection.spec.authentication_configured;
}
export function canReplaceConnectionCredential(connection: Connection): boolean { return hasCredential(connection); }

export function ReplaceConnectionCredentialDialog({ connection, close }: { connection: Connection; close: () => void }) {
  const qc = useQueryClient();
  const stdio = connection.kind === 'mcp' && connection.spec.transport === 'stdio';
  const [secret, setSecret] = useState('');
  const [environment, setEnvironment] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [intent, setIntent] = useState(newIntentKey);
  const [uncertain, setUncertain] = useState(false);
  const changed = () => { setIntent(newIntentKey()); setUncertain(false); mutation.reset(); };
  // Keep submitted material immutable through pending delivery and manual replay.
  // Discard the mutation closure when the dialog unmounts.
  const mutation = useMutation({ gcTime: 0, mutationFn: () => replaceConnectionCredential(connection, secret, environment.trim(), intent),
    onSuccess: async () => { setSecret(''); await refreshConnection(qc, connection); close(); },
    onError: () => setUncertain(true),
  });
  const conflict = mutation.error instanceof ControlRequestError && (mutation.error.status === 412 || mutation.error.status === 409);
  const retryable = !(mutation.error instanceof ControlRequestError) || mutation.error.status >= 500;
  const inspect = () => { void qc.invalidateQueries({ queryKey: queryKeys.connection(connection.id) }); close(); };
  return <Modal id="replace-connection-credential-title" title="Replace credential" close={close}>
    <form onSubmit={event => { event.preventDefault(); if (!uncertain && !mutation.isPending && confirmed) mutation.mutate(); }}>
      <div className="modalbody formstack">
        <p className="fieldhint">Replace material for the existing Connection <b>{connection.display_name}</b> at revision <code>{connection.revision}</code>. Workspace references stay unchanged. This sends the new secret only to local Control for storage, not to a model or Activity.</p>
        {stdio && <label>Existing environment variable name<input required data-autofocus value={environment} disabled={uncertain || mutation.isPending} onChange={event => { setEnvironment(event.target.value); changed(); }} placeholder="TOKEN" autoComplete="off"/></label>}
        <label>New secret<input type="password" data-autofocus={!stdio || undefined} required value={secret} disabled={uncertain || mutation.isPending} onChange={event => { setSecret(event.target.value); changed(); }} autoComplete="new-password"/></label>
        <label className="checkline"><input type="checkbox" checked={confirmed} disabled={uncertain || mutation.isPending} onChange={event => setConfirmed(event.target.checked)}/>I confirm the external identity is unchanged.</label>
        {mutation.isError && <div role="alert" className="formerror">{errorMessage(mutation.error)}<p>{conflict ? 'The replacement cannot be retried without reviewing current state.' : retryable ? 'The outcome may be uncertain. Only the identical request with the original retry key and material can be retried.' : 'Review the current Connection before another replacement.'}</p></div>}
      </div>
      <div className="modalfoot"><button type="button" className="btn" onClick={uncertain ? inspect : close}>{uncertain ? 'Inspect current Connection' : 'Cancel'}</button>
        {uncertain && !conflict && retryable ? <button type="button" className="btn primary" disabled={mutation.isPending} onClick={() => mutation.mutate()}>Retry exact replacement</button> :
          <button type="submit" className="btn primary" disabled={uncertain || !confirmed || !secret.trim() || (stdio && !environment.trim()) || mutation.isPending}>{mutation.isPending ? 'Replacing…' : 'Replace credential'}</button>}
      </div>
    </form>
  </Modal>;
}
