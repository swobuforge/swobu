# Workspace Configuration

Swobu stores local routing configuration in one private YAML file. The daemon is the only writer while it runs; use Cockpit or the local workspace command surface for changes. Manual edits require stopping the daemon first.

```yaml
schema_version: 1
workspaces:
  dev:
    default_route: chat
    routes:
      chat:
        tiers:
          - targets:
              - id: openai-primary
                model: gpt-5
                protocol: responses
                connection:
                  openai:
                    credential: env:OPENAI_API_KEY
```

Clients use `/c/dev/...` and request the route name (`chat`) as `model`. The model value `default`, or any other non-empty model name without a matching route, selects `default_route`; missing or blank model values fail. The first tier is primary, later tiers are fallbacks, and targets within one tier are equally balanced.

Swobu listens on `127.0.0.1:7926` by default. Both the Cockpit launcher and the
foreground daemon accept the same startup controls:

```sh
swobu --addr 127.0.0.1:9000 --config ./swobu.yaml
swobu daemon --addr 127.0.0.1:9000 --config ./swobu.yaml
```

`--addr` overrides `SWOBU_ADDR`; `--config` overrides `SWOBU_CONFIG_PATH`.
When bare `swobu` must start the daemon, it uses those resolved values and then
opens Cockpit against the same address. Address and config path are
restart-bound startup configuration, not routing state, and workspace edits
never rewrite them.

Each target uses exactly one connection arm: `openai`, `anthropic`, `openrouter`, `chatgpt`, `ollama`, `lmstudio`, `vllm`, `azure`, `bedrock`, or `custom`. Credentials are locators such as `env:OPENAI_API_KEY`, not secret values. Protocols must be concrete; `auto` is rejected.

Swobu rejects unknown fields and obsolete schemas at startup. The file is
replaced atomically after every successful command. Failures before rename keep
the old snapshot; directory-sync uncertainty after rename publishes the renamed
snapshot and fail-stops later writes until restart. A second daemon using the
same path is rejected while the first holds the lock.
