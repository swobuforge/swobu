import type { WorkspaceEndpoint } from './workspace-endpoints';

// Recipes are client-side documentation over published Service addresses, not
// configuration mutations. They MUST NOT accept or serialize bearer material.
export type ClientRecipeKind = 'codex' | 'claude' | 'openai' | 'mcp' | 'search';
export type ClientRecipe = {
  title: string;
  destination: string;
  body: string;
  note: string;
  sendsRequest: boolean;
};

export const clientRecipeKinds: ReadonlyArray<{ id: ClientRecipeKind; label: string; service: 'models' | 'mcp' | 'search' }> = [
  { id: 'codex', label: 'Codex CLI', service: 'models' },
  { id: 'claude', label: 'Claude Code', service: 'models' },
  { id: 'openai', label: 'OpenAI-compatible HTTP', service: 'models' },
  { id: 'mcp', label: 'Streamable HTTP MCP', service: 'mcp' },
  { id: 'search', label: 'Direct Search HTTP', service: 'search' },
];

export function requiresModel(kind: ClientRecipeKind): boolean {
  return kind === 'codex' || kind === 'claude' || kind === 'openai';
}

// The server matcher remains the authority; this only rules out malformed
// values that would make pasted client configuration unusable.
export function usableModelID(value: string): boolean {
  const hasControlCharacter = Array.from(value).some(character => {
    const code = character.charCodeAt(0);
    return code < 32 || code === 127;
  });
  return value.length > 0 && value.length <= 256 && value.trim() === value && !hasControlCharacter;
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

function endpoint(endpoints: readonly WorkspaceEndpoint[], id: string): string | undefined {
  return endpoints.find(candidate => candidate.id === id)?.url;
}

export function clientRecipe(kind: ClientRecipeKind, endpoints: readonly WorkspaceEndpoint[], model: string): ClientRecipe | null {
  const root = endpoint(endpoints, 'models-root');
  const v1 = endpoint(endpoints, 'models-v1');
  const mcp = endpoint(endpoints, 'mcp');
  const search = endpoint(endpoints, 'search');
  if (requiresModel(kind) && (!root || !v1 || !usableModelID(model))) return null;
  // The guard above proves model endpoints exist for all model recipes.
  const modelRoot = root || '';
  const apiRoot = v1 || '';
  switch (kind) {
    case 'codex': return {
      title: 'Codex CLI',
      destination: 'Merge into the client machine\'s ~/.codex/config.toml',
      body: [
        `model = ${JSON.stringify(model)}`,
        'model_provider = "swobu"',
        '',
        '[model_providers.swobu]',
        'name = "Swobu"',
        `base_url = ${JSON.stringify(modelRoot)}`,
        'wire_api = "responses"',
        'env_key = "SWOBU_API_KEY"',
      ].join('\n'),
      note: 'Set SWOBU_API_KEY to an issued Workspace credential on the client machine. Merge these keys into your existing TOML; do not paste a second [model_providers.swobu] table. The model ID must match a saved route.',
      sendsRequest: false,
    };
    case 'claude': return {
      title: 'Claude Code',
      destination: 'Merge the env keys into the client machine\'s ~/.claude/settings.json',
      body: JSON.stringify({ env: {
        ANTHROPIC_BASE_URL: root,
        ANTHROPIC_AUTH_TOKEN: '<WORKSPACE_CREDENTIAL>',
        CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY: '1',
      } }, null, 2),
      note: `Replace the placeholder with an issued Workspace credential on the client machine. Merge the env keys without replacing unrelated settings. Choose model ${JSON.stringify(model)} explicitly in Claude Code; discovery alone does not select it.`,
      sendsRequest: false,
    };
    case 'openai': return {
      title: 'OpenAI-compatible HTTP',
      destination: 'Run in a POSIX-compatible shell with SWOBU_API_KEY set',
      body: [
        'curl --fail-with-body --show-error --silent \\',
        '  -H "Authorization: Bearer $SWOBU_API_KEY" \\',
        '  -H "Content-Type: application/json" \\',
        `  --data-binary ${shellQuote(JSON.stringify({ model, input: 'Hello from Swobu' }))} \\`,
        `  ${shellQuote(apiRoot + '/responses')}`,
      ].join('\n'),
      note: 'This sends a real model request and may incur upstream charges. For SDKs, use the shown /v1 base URL, the same model ID, and a Workspace credential as the API key. Never use a Node Control credential here.',
      sendsRequest: true,
    };
    case 'mcp': return mcp ? {
      title: 'Streamable HTTP MCP',
      destination: 'Connection fields for an MCP client supporting Streamable HTTP',
      body: JSON.stringify({ transport: 'streamable_http', url: mcp, headers: {
        Authorization: 'Bearer <WORKSPACE_CREDENTIAL>',
      } }, null, 2),
      note: 'This describes connection fields, not a universal client-specific JSON schema. Adapt them to your MCP client. Replace the credential placeholder on the client machine. The Principal must have MCP access to this Workspace.',
      sendsRequest: false,
    } : null;
    case 'search': return search ? {
      title: 'Direct Search HTTP',
      destination: 'Run in a POSIX-compatible shell with SWOBU_API_KEY set',
      body: [
        'curl --fail-with-body --show-error --silent \\',
        '  -H "Authorization: Bearer $SWOBU_API_KEY" \\',
        `  ${shellQuote(search + '?query=hello&limit=5')}`,
      ].join('\n'),
      note: 'This performs a real Search request. The Principal needs Search access; the Service must have a configured Search route. Search requests may incur provider charges.',
      sendsRequest: true,
    } : null;
  }
}
