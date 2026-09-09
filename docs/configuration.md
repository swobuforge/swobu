# Workspace Configuration

Swobu stores local routing configuration in one YAML file on your machine. The daemon is the only writer while it runs; use Cockpit or the local workspace command surface for changes. Manual edits require stopping the daemon first.

On Unix, keep the file owner-readable/writable only (`0600`) and its directory
owner-accessible only (`0700`). Swobu checks these permissions when opening it.

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

Each target selects one provider under `connection`; see the
[backend recipes](./README.md#backend-recipes) for setup examples. Credentials
are references such as `env:OPENAI_API_KEY`, not secret values. Set the protocol
as shown in the provider recipe, or let Cockpit configure it.

Swobu validates the configuration at startup and saves changes atomically.
Only one daemon can use a configuration file at a time. A directory-sync
warning means the change was saved, but its persistence through a system crash
is uncertain. Check the underlying filesystem; the daemon remains writable.
