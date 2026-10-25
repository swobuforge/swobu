import { describe, expect, it } from 'vitest';
import { pageContext, pageRoutes, globalPages, workspaceSections } from './routes';
describe('Operator address space', () => {
  it('retains the accepted page and Workspace section order', () => {
    expect(globalPages).toEqual(['Workspaces', 'Connections', 'Access', 'Activity', 'Settings']);
    expect(workspaceSections).toEqual(['Overview', 'Services', 'Access', 'Limits', 'Connect']);
  });
  it('keeps Workspace identity and section in URL state', () => {
    expect(pageContext('/workspaces/research/services/models')).toEqual({ global: 'Workspaces', workspace: 'research', section: 'services', title: 'research' });
  });
  it('has no legacy global Connect or runtime route', () => {
    expect(pageRoutes.some(path => ['/runtime', '/shares', '/connect'].includes(path))).toBe(false);
  });
});
