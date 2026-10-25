import { describe, expect, it } from 'vitest';
import { paths, pageContext } from '../app/routes';

describe('W04 authority navigation', () => {
  it('keeps Principal detail separate from Workspace grant and credential lifecycle', () => {
    expect(paths.principals).toBe('/access');
    expect(paths.principal).toBe('/access/$principalId');
    expect(paths.workspaceAccess).toBe('/workspaces/$slug/access');
  });
  it('keeps publication on the Workspace Connect surface and budgets in Limits', () => {
    expect(pageContext('/workspaces/engineering/limits').section).toBe('limits');
    expect(pageContext('/workspaces/engineering/connect').section).toBe('connect');
    expect(paths.network).toBe('/settings/network');
  });
});
