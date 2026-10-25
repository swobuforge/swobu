import { describe, expect, it } from 'vitest';
import { clientRecipe, requiresModel, usableModelID } from './remote-client-recipes';
import { workspaceEndpoints } from './workspace-endpoints';

const published = workspaceEndpoints('https://relay.example', 'team', ['models', 'mcp', 'search'], true);
const local = workspaceEndpoints('http://127.0.0.1:7926', 'team', ['models', 'mcp', 'search']);

describe('manual Workspace client recipes', () => {
  it('matches the canonical Codex adapter without embedding credentials', () => {
    const recipe = clientRecipe('codex', published, 'coding')!;
    expect(recipe.body).toContain('model = "coding"');
    expect(recipe.body).toContain('base_url = "https://relay.example/workspaces/team"');
    expect(recipe.body).toContain('wire_api = "responses"');
    expect(recipe.body).toContain('env_key = "SWOBU_API_KEY"');
    expect(recipe.body).not.toContain('experimental_bearer_token');
    expect(recipe.body).not.toContain('Bearer ');
    expect(recipe.sendsRequest).toBe(false);
  });
  it('matches Claude Code discovery and requires explicit credential substitution', () => {
    const recipe = clientRecipe('claude', published, 'coding')!;
    const parsed = JSON.parse(recipe.body);
    expect(parsed.env.ANTHROPIC_BASE_URL).toBe('https://relay.example/workspaces/team');
    expect(parsed.env.ANTHROPIC_AUTH_TOKEN).toBe('<WORKSPACE_CREDENTIAL>');
    expect(parsed.env.CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY).toBe('1');
    expect(recipe.note).toContain('Choose model "coding" explicitly');
  });
  it('generates a real Responses request and safely shell-quotes model IDs', () => {
    const model = `foo'bar`;
    const recipe = clientRecipe('openai', local, model)!;
    expect(recipe.sendsRequest).toBe(true);
    expect(recipe.body).toContain('http://127.0.0.1:7926/workspaces/team/v1/responses');
    expect(recipe.body).toContain('Authorization: Bearer $SWOBU_API_KEY');
    expect(recipe.body).toContain(`foo'\\''bar`);
    expect(recipe.body).not.toContain('<WORKSPACE_CREDENTIAL>');
  });
  it('keeps MCP and Search authorization independent of Models', () => {
    const endpoints = workspaceEndpoints('https://relay.example', 'team', ['mcp', 'search'], true);
    expect(clientRecipe('codex', endpoints, 'coding')).toBeNull();
    const mcp = clientRecipe('mcp', endpoints, '')!;
    const mcpBody = JSON.parse(mcp.body);
    expect(mcpBody.transport).toBe('streamable_http');
    expect(mcpBody.url).toBe('https://relay.example/workspaces/team/mcp');
    expect(mcpBody.headers.Authorization).toBe('Bearer <WORKSPACE_CREDENTIAL>');
    expect(clientRecipe('search', endpoints, '')!.body).toContain('/search?query=hello&limit=5');
    expect(clientRecipe('search', endpoints, '')!.sendsRequest).toBe(true);
  });
  it('rejects absent/invalid model and Service addresses rather than inventing configuration', () => {
    expect(requiresModel('codex')).toBe(true);
    expect(requiresModel('mcp')).toBe(false);
    for (const id of ['', ' coding', 'coding\nother', 'coding\u0000other', 'a'.repeat(257)]) {
      expect(usableModelID(id)).toBe(false);
      expect(clientRecipe('codex', published, id)).toBeNull();
    }
    expect(clientRecipe('mcp', [], '')).toBeNull();
    expect(clientRecipe('search', [], '')).toBeNull();
  });
});
