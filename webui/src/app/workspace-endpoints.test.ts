import { describe, expect, it } from 'vitest';
import { workspaceEndpoints } from './workspace-endpoints';

describe('Workspace Connect endpoint presentation', () => {
  it('uses the same typed Service paths on local and published origins', () => {
    expect(workspaceEndpoints('http://127.0.0.1:7926', 'my-workspace', ['models', 'mcp', 'search'])).toEqual([
      { id: 'models-root', service: 'models', label: 'Workspace root (Codex / Claude Code)', url: 'http://127.0.0.1:7926/workspaces/my-workspace' },
      { id: 'models-v1', service: 'models', label: 'OpenAI-compatible API', url: 'http://127.0.0.1:7926/workspaces/my-workspace/v1' },
      { id: 'mcp', service: 'mcp', label: 'MCP endpoint', url: 'http://127.0.0.1:7926/workspaces/my-workspace/mcp' },
      { id: 'search', service: 'search', label: 'Direct Search', url: 'http://127.0.0.1:7926/workspaces/my-workspace/search' },
    ]);
    expect(workspaceEndpoints('https://node.example.test', 'team', ['mcp'], true)).toEqual([
      { id: 'mcp', service: 'mcp', label: 'MCP endpoint', url: 'https://node.example.test/workspaces/team/mcp' },
    ]);
  });
  it('never invents disabled Services or a remote URL from an invalid origin', () => {
    expect(workspaceEndpoints('https://node.example.test', 'team', [], true)).toEqual([]);
    expect(workspaceEndpoints('http://node.example.test', 'team', ['models'], true)).toEqual([]);
    expect(workspaceEndpoints('https://node.example.test/other', 'team', ['models'], true)).toEqual([]);
    expect(workspaceEndpoints('https://user:secret@node.example.test', 'team', ['models'], true)).toEqual([]);
    expect(workspaceEndpoints('https://node.example.test?token=secret', 'team', ['models'], true)).toEqual([]);
    expect(workspaceEndpoints('', 'team', ['models'], true)).toEqual([]);
  });
  it('encodes Workspace slugs rather than concatenating routing punctuation', () => {
    expect(workspaceEndpoints('https://node.example.test/', 'space/test', ['search'], true)[0].url).toBe(
      'https://node.example.test/workspaces/space%2Ftest/search');
  });
});
