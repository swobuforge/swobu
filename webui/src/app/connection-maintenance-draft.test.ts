import { describe, expect, test } from 'vitest';
import { completeDraft, envRefs, initialFields } from './connection-maintenance-draft';
import type { Connection } from '../api/resources';

describe('complete private Connection authoring', () => {
  const model = { id: 'c1', kind: 'model', display_name: 'Prod', revision: '7',
    spec: { shape: 'standard', provider_id: 'openai', endpoint: 'https://api.example.test', authentication_configured: true },
  } as Connection;
  test('never reconstructs private inputs from redacted readback', () => {
    const fields = initialFields(model);
    expect(fields.credential).toBe('');
    expect(fields.endpoint).toBe('');
    expect(() => completeDraft(model, fields)).toThrow('endpoint');
    expect(() => completeDraft(model, { ...fields, endpoint: 'https://api.example.test' })).toThrow('credential reference');
  });
  test('sends a full explicit same-identity edit preserving Connection ID separately', () => {
    expect(completeDraft(model, { ...initialFields(model), name: 'Renamed', credential: 'env:KEY', endpoint: 'https://api.example.test' })).toEqual({
      kind: 'model', same_identity: true, display_name: 'Renamed',
      spec: { shape: 'standard', provider_id: 'openai', configuration: { endpoint: 'https://api.example.test', credential_ref: 'env:KEY' } },
    });
  });
  test('custom header must also be explicitly reentered', () => {
    const custom = { ...model, spec: { shape: 'custom', provider_id: 'custom', endpoint: 'https://own.test', authentication_configured: true } } as Connection;
    expect(() => completeDraft(custom, { ...initialFields(custom), endpoint: 'https://own.test', credential: 'env:KEY' })).toThrow('header');
  });
  test('Search provider identity is stable and no new credential binding is invented', () => {
    const search = { ...model, kind: 'search', spec: { provider_id: 'brave', endpoint: 'https://search.test', authentication_configured: false } } as Connection;
    expect(completeDraft(search, { ...initialFields(search), endpoint: 'https://search.test' }).spec).toEqual({ provider_id: 'brave', endpoint: 'https://search.test' });
  });
  test('stdio input is exact argv and unique environment bindings, never shell parsed', () => {
    const stdio = { ...model, kind: 'mcp', spec: { transport: 'stdio', executable_name: 'node', argument_count: 3, environment_binding_count: 2, execution_identity: '', permission_summary: '' } } as Connection;
    expect(() => completeDraft(stdio, initialFields(stdio))).toThrow('executable');
    expect(() => envRefs('TOKEN=env:A\nTOKEN=env:B')).toThrow('unique');
    expect(envRefs('TOKEN=env:A\nOTHER=secret:B')).toEqual({ TOKEN: 'env:A', OTHER: 'secret:B' });
    expect(completeDraft(stdio, { ...initialFields(stdio), executable: '/usr/bin/node', args: '--flag\nvalue', environment: 'TOKEN=env:A' }).spec).toEqual({
      transport: 'stdio', execution_identity: '', permission_summary: '',
      configuration: { executable: '/usr/bin/node', args: ['--flag', 'value'], env_refs: { TOKEN: 'env:A' } },
    });
  });
});
