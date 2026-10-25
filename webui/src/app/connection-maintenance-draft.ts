import type { Connection, ConnectionEdit } from '../api/resources';

// Connection reads deliberately redact credential locators and stdio argv/env.
// Editing never guesses those fields from the public view; the operator supplies
// a complete replacement spec. The existing Connection ID and references stay.
export type Fields = {
  name: string; endpoint: string; credential: string; region: string; access: string;
  header: string; executable: string; args: string; environment: string;
  executionIdentity: string; permissionSummary: string;
};
export function initialFields(connection: Connection): Fields {
  const spec = connection.spec;
  return {
    name: connection.display_name,
    endpoint: '', // Redacted endpoints can omit query/URL credentials; require exact re-entry.
    credential: '', header: '', executable: '', args: '', environment: '',
    region: 'region' in spec ? spec.region : '',
    access: 'access' in spec ? spec.access : 'general_api',
    executionIdentity: 'execution_identity' in spec ? spec.execution_identity ?? '' : '',
    permissionSummary: 'permission_summary' in spec ? spec.permission_summary ?? '' : '',
  };
}

export function envRefs(input: string): Record<string, string> {
  const entries: [string, string][] = [];
  const seen = new Set<string>();
  for (const line of input.split('\n').filter(line => line.trim())) {
    const at = line.indexOf('=');
    const key = line.slice(0, at).trim();
    const ref = line.slice(at + 1).trim();
    if (at < 1 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || !ref || seen.has(key)) {
      throw new Error('Enter each environment binding as a unique NAME=credential-reference line.');
    }
    seen.add(key);
    entries.push([key, ref]);
  }
  return Object.fromEntries(entries);
}

export function completeDraft(connection: Connection, fields: Fields): ConnectionEdit {
  const display_name = fields.name.trim();
  const credential_ref = fields.credential.trim();
  if (!display_name) throw new Error('Enter a Connection name.');
  if ('endpoint' in connection.spec && connection.spec.endpoint && !fields.endpoint.trim()) {
    throw new Error('Re-enter the complete endpoint; the readback may omit private URL components.');
  }
  if (connection.kind === 'model') {
    const spec = connection.spec;
    if (spec.authentication_configured && !credential_ref) {
      throw new Error('Re-enter the existing credential reference, or use Replace credential instead.');
    }
    if (spec.shape === 'custom' && spec.authentication_configured && !fields.header.trim()) {
      throw new Error('Re-enter the custom authentication header name.');
    }
    const configuration = spec.shape === 'standard' ? { endpoint: fields.endpoint.trim(), credential_ref } :
      spec.shape === 'zai' ? { access: fields.access as 'general_api' | 'coding_plan', credential_ref } :
      spec.shape === 'bedrock' ? { region: fields.region.trim(), endpoint: fields.endpoint.trim(), credential_ref } :
      { endpoint: fields.endpoint.trim(), header: fields.header.trim(), credential_ref };
    return { kind: 'model', display_name, same_identity: true,
      spec: { shape: spec.shape, provider_id: spec.provider_id, configuration } } as ConnectionEdit;
  }
  if (connection.kind === 'search') {
    if (connection.spec.authentication_configured && !credential_ref) {
      throw new Error('Re-enter the existing credential reference, or use Replace credential instead.');
    }
    return { kind: 'search', display_name, same_identity: true, spec: {
      provider_id: connection.spec.provider_id, endpoint: fields.endpoint.trim(), ...(credential_ref ? { credential_ref } : {}),
    } };
  }
  const declarations = { execution_identity: fields.executionIdentity.trim(), permission_summary: fields.permissionSummary.trim() };
  if (connection.spec.transport === 'streamable_http') {
    if (connection.spec.authentication_configured && !credential_ref) {
      throw new Error('Re-enter the existing credential reference, or use Replace credential instead.');
    }
    return { kind: 'mcp', display_name, same_identity: true, spec: { transport: 'streamable_http',
      ...declarations, configuration: { endpoint: fields.endpoint.trim(), ...(credential_ref ? { credential_ref } : {}) },
    } };
  }
  if (!fields.executable.trim()) throw new Error('Re-enter the full executable path; it is intentionally redacted on reads.');
  const refs = envRefs(fields.environment);
  if (connection.spec.environment_binding_count > 0 && !Object.keys(refs).length) {
    throw new Error('Re-enter the stdio environment bindings; they are intentionally redacted on reads.');
  }
  return { kind: 'mcp', display_name, same_identity: true, spec: { transport: 'stdio',
    ...declarations, configuration: {
      executable: fields.executable.trim(), args: fields.args ? fields.args.split('\n') : [], env_refs: refs,
    },
  } };
}

