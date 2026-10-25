export interface paths {
    "/api/v1/activity": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Content-free live snapshot or retained Activity page when limit is set. from/to/principal/result are available ONLY for retained kind=exchange. They filter the stored settlement history, not the live ring or MCP history. Cursors are scoped to Workspace and evidence kind, not to filters. Absence does not prove an execution never occurred. */
        get: operations["get_api_v1_activity"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/activity/export": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Download a bounded retained Activity slice as NDJSON or CSV. Requires one kind (exchange or mcp_operation); up to 2000 rows, 8 MiB and 30 seconds per response. Export is content-free retained evidence, not a complete execution audit or a transactionally consistent database snapshot. Repeat with X-Swobu-Next-Cursor until absent. from/to/principal/result apply to exchange only. */
        get: operations["get_api_v1_activity_export"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/activity/operations/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Content-free causal evidence and separate upstream/client outcomes for one exact operation. Counts describe recorded invocations, not proof against unrecorded execution or permission to retry. A 404 means unavailable evidence, not confirmed absence or failure. */
        get: operations["get_api_v1_activity_operations_id"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/activity/stream": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Bounded content-free live Activity observations (not durable replay). Reconnect starts a fresh view; Last-Event-ID and retained cursors are rejected. */
        get: operations["get_api_v1_activity_stream"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/browser-launches": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Native local host only. Creates one expiring one-shot browser launch path; the URL fragment is not a reusable Workspace credential. */
        post: operations["post_api_v1_browser_launches"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/browser-session": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Same-origin browser exchange of the one-shot launch token for a local, HttpOnly session cookie. One use only; no token returned. */
        post: operations["post_api_v1_browser_session"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/client-configurations": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description dry_run=true observes a client-file diff; dry_run=false requires retained exact review, strong If-Match, original retry key, and explicit human consent. Apply can leave an indeterminate marker; do not blindly resend. */
        post: operations["post_api_v1_client_configurations"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/clients": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Detect locally installed clients for exactly one existing Workspace. No client files are read or changed. */
        get: operations["get_api_v1_clients"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/connections": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Redacted Connection read, admitted local control only. Responses are no-store. */
        get: operations["get_api_v1_connections"];
        put?: never;
        /** @description Explicit typed Connection authoring; redacted read responses are not editable drafts. Local browser session and same-origin mutation admission required. */
        post: operations["post_api_v1_connections"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/connections/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Redacted Connection read, admitted local control only. Responses are no-store. */
        get: operations["get_api_v1_connections_id"];
        put?: never;
        post?: never;
        /** @description Delete an unused Connection at the reviewed revision, without cascade. Success returns 204; a repeat after deletion returns 404 without another management fact. Browser session and same-origin mutation admission are required. */
        delete: operations["delete_api_v1_connections_id"];
        options?: never;
        head?: never;
        /** @description Explicit typed Connection authoring; redacted read responses are not editable drafts. Local browser session and same-origin mutation admission required. */
        patch: operations["patch_api_v1_connections_id"];
        trace?: never;
    };
    "/api/v1/connections/{id}/checks": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Check the stored Connection at the reviewed strong If-Match revision. No request body. Observation is ephemeral, no-store and revision-bound; probes do not mutate configuration, credentials or Workspace catalogs. Live operator authority is rechecked before observation publication. */
        post: operations["post_api_v1_connections_id_checks"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/connections/{id}/credential-replacements": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Replace one existing credential binding for the same external identity. Retry with the original strong If-Match, Idempotency-Key and material; durable staging reuses material and returns the original result. Responses are redacted and no-store. */
        post: operations["post_api_v1_connections_id_credential_replacements"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/connections/{id}/mcp-catalog": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Secret-free MCP declarations from the current revision-bound Connection runtime binding. */
        get: operations["get_api_v1_connections_id_mcp_catalog"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/connections/{id}/references": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Redacted Connection read, admitted local control only. Responses are no-store. */
        get: operations["get_api_v1_connections_id_references"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/node": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Redacted, same-revision Node overview for local administration. ETag is the digest of complete committed swobu.yaml bytes; the response never returns YAML, secrets or operation receipts. This read is not an entire-Node mutation API. */
        get: operations["get_api_v1_node"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/principals": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description List managed Principals with their Workspace use relationships; never returns credentials or bearer secrets. */
        get: operations["get_api_v1_principals"];
        put?: never;
        /** @description Create Principal metadata only. Original idempotency key must survive uncertain retries. */
        post: operations["post_api_v1_principals"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/principals/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read exact Principal and grants, redacted and revisioned. */
        get: operations["get_api_v1_principals_id"];
        put?: never;
        post?: never;
        /** @description Preview or revoke one Principal and associated credentials at an exact Principal revision. Preview is not permission to commit; apply requires the original idempotency key and trusted host admission. */
        delete: operations["delete_api_v1_principals_id"];
        options?: never;
        head?: never;
        /** @description Rename Principal label at exact strong ETag; never changes its authority. */
        patch: operations["patch_api_v1_principals_id"];
        trace?: never;
    };
    "/api/v1/principals/{id}/credentials": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description List credential metadata. Secret tokens are never returned. */
        get: operations["get_api_v1_principals_id_credentials"];
        put?: never;
        /** @description Issue Workspace bearer once. Exact retry returns metadata and secret_not_recoverable; never a second token. */
        post: operations["post_api_v1_principals_id_credentials"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/principals/{id}/credentials/{credential}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /** @description dry_run=true previews selected credential impact; commit requires preview ETag, original idempotency key and trusted admission. */
        delete: operations["delete_api_v1_principals_id_credentials_credential"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/remote-addresses": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Built-in relay address; URL exists only once its transport identity has been created. */
        get: operations["get_api_v1_remote_addresses"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Current Workspace summaries with no private credentials. Requires local control authority; all responses are no-store. */
        get: operations["get_api_v1_workspaces"];
        put?: never;
        /** @description Create a Workspace at an immutable slug, with one durable idempotency key per intent. Requires local control authority; all responses are no-store. */
        post: operations["post_api_v1_workspaces"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read one Workspace with current management ETag and service configuration. Requires local control authority; all responses are no-store. */
        get: operations["get_api_v1_workspaces_slug"];
        put?: never;
        post?: never;
        /** @description Review the exact Workspace deletion impact with dry_run=true. Commit requires the Workspace ETag, original idempotency key, and local operator authority; Access and publication constraints are checked by the application owner. */
        delete: operations["delete_api_v1_workspaces_slug"];
        options?: never;
        head?: never;
        /** @description Update presentation metadata at the reviewed Workspace revision; slug is immutable. Requires local control authority; all responses are no-store. */
        patch: operations["patch_api_v1_workspaces_slug"];
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/limits": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read configured spending policy, or 404 when not configured. */
        get: operations["get_api_v1_workspaces_slug_limits"];
        put?: never;
        post?: never;
        /** @description Clear only the spending policy at exact accounting ETag. Retained reservations and settlements survive. */
        delete: operations["delete_api_v1_workspaces_slug_limits"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/limits/apply": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Consume an exact unexpired reviewed approval before committing the policy at the reviewed revision. */
        post: operations["post_api_v1_workspaces_slug_limits_apply"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/limits/preview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /** @description Preview a spending policy via a short-lived approval bound to the exact accounting revision and candidate. */
        post: operations["post_api_v1_workspaces_slug_limits_preview"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/limits/status": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Return retained reservation and settlement evidence bound to the strong accounting ETag. No budget is represented as null. */
        get: operations["get_api_v1_workspaces_slug_limits_status"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/mcp-mounts": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Current redacted MCP mounts belonging to this Workspace. */
        get: operations["get_api_v1_workspaces_slug_mcp_mounts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/mcp-mounts/{alias}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read one current mount. */
        get: operations["get_api_v1_workspaces_slug_mcp_mounts_alias"];
        /** @description dry_run=true produces an advisory review, dry_run=false applies an exact review with strong Workspace ETag and unchanged Connection revision. */
        put: operations["put_api_v1_workspaces_slug_mcp_mounts_alias"];
        post?: never;
        /** @description Delete a current MCP mount at a strong Workspace ETag. Other mounts and upstream Connections are retained. */
        delete: operations["delete_api_v1_workspaces_slug_mcp_mounts_alias"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/model-match": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Preview saved first-match model routing through the production Go path.Match resolver, without provider dispatch. */
        get: operations["get_api_v1_workspaces_slug_model_match"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/model-routes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read the current Models routes, including complete tier topology and redacted Connection references. No-store. */
        get: operations["get_api_v1_workspaces_slug_model_routes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /** @description Atomically replace first-match precedence with the exact permutation of configured route patterns. Requires local authority and the Workspace If-Match. No target revisions change. */
        patch: operations["patch_api_v1_workspaces_slug_model_routes"];
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/model-routes/{route}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read the editable route specification; route identity is the path, target versions are server-owned. */
        get: operations["get_api_v1_workspaces_slug_model_routes_route"];
        /** @description Atomic route create/update with one strong Workspace If-Match. If delivery is uncertain, read the Workspace and reconcile; no replay receipt is issued. */
        put: operations["put_api_v1_workspaces_slug_model_routes_route"];
        post?: never;
        /** @description Delete a Models route at the exact Workspace revision; last-route deletion leaves Models enabled but empty. */
        delete: operations["delete_api_v1_workspaces_slug_model_routes_route"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/permissions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Read one natural Workspace use relationship at current authority revision. */
        get: operations["get_api_v1_workspaces_slug_permissions_id"];
        /** @description Review or commit a Workspace permission with scoped Services and optional per-Service request ceilings. Omission of request_limits preserves existing ceilings; [] clears them. Omitting services preserves an existing scoped grant; a new omitted-service grant permits all enabled Services; the Web Operator sends an explicit selection. Review never grants or issues credentials; apply binds the preview revision, Services, and retry identity. */
        put: operations["put_api_v1_workspaces_slug_permissions_id"];
        post?: never;
        /** @description Review or commit a Workspace permission with scoped Services and optional per-Service request ceilings. Omission of request_limits preserves existing ceilings; [] clears them. Omitting services preserves an existing scoped grant; a new omitted-service grant permits all enabled Services; the Web Operator sends an explicit selection. Review never grants or issues credentials; apply binds the preview revision, Services, and retry identity. */
        delete: operations["delete_api_v1_workspaces_slug_permissions_id"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/publications": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Reachability through the built-in relay; Workspace credentials and Service policy still apply. */
        get: operations["get_api_v1_workspaces_slug_publications"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/publications/{remote_address_id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** @description Enable or disable relay reachability at the current publication revision. Workspace credential policy remains independent. */
        put: operations["put_api_v1_workspaces_slug_publications_remote_address_id"];
        post?: never;
        /** @description Enable or disable relay reachability at the current publication revision. Workspace credential policy remains independent. */
        delete: operations["delete_api_v1_workspaces_slug_publications_remote_address_id"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/search-route": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Ordered primary and fallback Search Connections, independent of provider-native LLM search. An upstream failure advances to the next configured Connection; an empty successful result is terminal. */
        get: operations["get_api_v1_workspaces_slug_search_route"];
        /** @description Ordered primary and fallback Search Connections, independent of provider-native LLM search. An upstream failure advances to the next configured Connection; an empty successful result is terminal. */
        put: operations["put_api_v1_workspaces_slug_search_route"];
        post?: never;
        /** @description Ordered primary and fallback Search Connections, independent of provider-native LLM search. An upstream failure advances to the next configured Connection; an empty successful result is terminal. */
        delete: operations["delete_api_v1_workspaces_slug_search_route"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/service-limits": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description Workspace-wide Models/MCP/Search admission policy. Zero is unlimited. Distinct from /limits, which owns durable spend policy. Local operator authority required. */
        get: operations["get_api_v1_workspaces_slug_service_limits"];
        /** @description Workspace-wide Models/MCP/Search admission policy. Zero is unlimited. Distinct from /limits, which owns durable spend policy. Local operator authority required. */
        put: operations["put_api_v1_workspaces_slug_service_limits"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/services": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description List all released Workspace Services, including disabled and enabled-but-unconfigured states. Snapshot is redacted and no-store. */
        get: operations["get_api_v1_workspaces_slug_services"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/workspaces/{slug}/services/{service}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        /** @description Checked explicit Service lifecycle. Requires trusted local operator admission and a strong Workspace If-Match; enabling with existing grants or remote publication requires an exact server-issued activation review digest from GET /services. Nonempty Service disable fails with 409. */
        put: operations["put_api_v1_workspaces_slug_services_service"];
        post?: never;
        /** @description Checked explicit Service lifecycle. Requires trusted local operator admission and a strong Workspace If-Match; enabling with existing grants or remote publication requires an exact server-issued activation review digest from GET /services. Nonempty Service disable fails with 409. */
        delete: operations["delete_api_v1_workspaces_slug_services_service"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        AccessIdentity: {
            created_at?: string;
            credentials: components["schemas"]["PrincipalCredential"][];
            id: string;
            kind: string;
            name: string;
            status: string;
        };
        ActivityExchange: {
            access_id: string;
            attempt_count: number;
            client_family?: string;
            client_protocol?: string;
            connection_id?: string;
            connection_revision?: number;
            duration_ms?: number | null;
            exchange_id: string;
            http_status?: number;
            model?: string;
            operation?: string;
            protocol?: string;
            provider?: string;
            route?: string;
            /** Format: date-time */
            started_at: string;
            state: string;
            target_version?: number;
            token_usage?: components["schemas"]["ActivityTokenUsage"];
            workspace: string;
        };
        ActivityExchangeDetail: {
            access_id: string;
            delivery_result?: string;
            error_code?: string;
            exchange_id: string;
            http_status?: number;
            invocations: components["schemas"]["ActivityInvocation"][];
            settled_at?: string;
            started_at: string;
            upstream_execution: components["schemas"]["ActivityUpstreamExecution"];
            workspace: string;
        };
        ActivityHealth: {
            committed_exchanges_total: number;
            committed_mcp_operations_total: number;
            dropped_exchanges_total: number;
            dropped_mcp_operations_total: number;
            last_error?: string;
            offered_exchanges_total: number;
            offered_mcp_operations_total: number;
            queue_capacity: number;
            queue_depth: number;
            state: string;
        };
        ActivityInvocation: {
            connection_id?: string;
            connection_revision?: number;
            execution_possibility?: string;
            failure?: string;
            invocation_id: string;
            kind: string;
            model?: string;
            outcome?: string;
            provider?: string;
            purpose?: string;
            route?: string;
            settled_at?: string;
            source?: string;
            started_at: string;
            target?: string;
            target_version?: number;
            tool?: string;
        };
        ActivityMCPOperation: {
            access_id: string;
            execution_possibility?: string;
            operation_id: string;
            outcome?: string;
            /** Format: date-time */
            settled_at?: string;
            source: string;
            /** Format: date-time */
            started_at: string;
            tool: string;
            workspace: string;
        };
        ActivityOperationDetail: {
            evidence: string;
            exchange?: components["schemas"]["ActivityExchangeDetail"];
            /** @enum {string} */
            kind: "exchange" | "mcp_operation";
            mcp_operation?: components["schemas"]["ActivityMCPOperation"];
        };
        ActivityPage: {
            exchange_next?: string;
            limit: number;
            mcp_next?: string;
            mode: string;
        };
        ActivityProjection: {
            exchanges: components["schemas"]["ActivityExchange"][];
            history?: components["schemas"]["ActivityHealth"];
            mcp_operations?: components["schemas"]["ActivityMCPOperation"][];
            page?: components["schemas"]["ActivityPage"];
            scope: components["schemas"]["ActivityScope"];
        };
        ActivityScope: {
            /** @enum {string} */
            kind: "all" | "workspace";
            workspace?: string;
        };
        ActivityTokenUsage: {
            cache_read_tokens?: number;
            cache_write_tokens?: number;
            input_tokens?: number;
            output_tokens?: number;
            reasoning_tokens?: number;
        };
        ActivityUpstreamExecution: {
            did_not_occur: number;
            may_have_occurred: number;
            occurred: number;
            recorded_invocations: number;
            unsettled: number;
        };
        BrowserLaunch: {
            launch_path: string;
        };
        BrowserSessionExchange: {
            token: string;
        };
        ClientConfigurationApplyResult: {
            already_configured: boolean;
            authentication: components["schemas"]["ClientConfigurationAuthentication"];
            changes: components["schemas"]["ClientConfigurationChange"][];
            client: components["schemas"]["DetectedClient"];
            config_paths: string[];
            model_id: string;
            post_apply_hint?: string;
            replayed: boolean;
            requires_replace: boolean;
            revision: string;
            workspace: string;
        };
        ClientConfigurationAuthentication: {
            credential_id?: string;
            issue_credential: boolean;
            permission_revision: string;
            principal: string;
        };
        ClientConfigurationChange: {
            after: string;
            before: string;
            before_exists: boolean;
            field: string;
        };
        ClientConfigurationPlan: {
            already_configured: boolean;
            authentication: components["schemas"]["ClientConfigurationAuthentication"];
            changes: components["schemas"]["ClientConfigurationChange"][];
            client: components["schemas"]["DetectedClient"];
            config_paths: string[];
            model_id: string;
            post_apply_hint?: string;
            requires_replace: boolean;
            revision: string;
            workspace: string;
        };
        ClientConfigurationRequest: {
            client: string;
            model_id: string;
            principal: string;
            review?: components["schemas"]["ClientConfigurationReview"];
            workspace: string;
        };
        ClientConfigurationReview: {
            authentication: components["schemas"]["ClientConfigurationAuthentication"];
            changes: components["schemas"]["ClientConfigurationChange"][];
            config_paths: string[];
            revision: string;
        };
        Connection: {
            /** Format: date-time */
            created_at: string;
            display_name: string;
            id: string;
            /** @constant */
            kind: "model";
            latest_check?: components["schemas"]["ConnectionCheck"];
            revision: string;
            spec: components["schemas"]["ConnectionStandardModel"] | components["schemas"]["ConnectionZAIModel"] | components["schemas"]["ConnectionBedrockModel"] | components["schemas"]["ConnectionCustomModel"];
            /** Format: date-time */
            updated_at: string;
        } | {
            /** Format: date-time */
            created_at: string;
            display_name: string;
            id: string;
            /** @constant */
            kind: "mcp";
            latest_check?: components["schemas"]["ConnectionCheck"];
            revision: string;
            spec: components["schemas"]["ConnectionMCPHTTP"] | components["schemas"]["ConnectionMCPStdio"];
            /** Format: date-time */
            updated_at: string;
        } | {
            /** Format: date-time */
            created_at: string;
            display_name: string;
            id: string;
            /** @constant */
            kind: "search";
            latest_check?: components["schemas"]["ConnectionCheck"];
            revision: string;
            spec: components["schemas"]["ConnectionSearch"];
            /** Format: date-time */
            updated_at: string;
        };
        ConnectionBedrockDraftSpec: {
            configuration: {
                credential_ref?: string;
                endpoint: string;
                region: string;
            };
            provider_id: string;
            /** @enum {string} */
            shape: "bedrock";
        };
        ConnectionBedrockModel: {
            authentication_configured: boolean;
            endpoint: string;
            provider_id: string;
            region: string;
            /** @constant */
            shape: "bedrock";
        };
        ConnectionCheck: {
            /** @enum {string} */
            authentication: "unknown" | "accepted" | "rejected" | "not_required";
            capability_summary: ("models" | "tools" | "search")[];
            /** Format: date-time */
            checked_at: string;
            connection_id: string;
            connection_revision: string;
            /** @enum {string} */
            reachability: "unknown" | "reachable" | "unreachable";
            /** @enum {string} */
            sanitized_error?: "timeout" | "network_denied" | "unavailable" | "authentication_failed" | "invalid_response" | "credential_unavailable";
        };
        ConnectionCreateDraft: {
            display_name: string;
            /** @enum {string} */
            kind: "model";
            same_identity?: boolean;
            spec: components["schemas"]["ConnectionStandardDraftSpec"] | components["schemas"]["ConnectionZAIDraftSpec"] | components["schemas"]["ConnectionBedrockDraftSpec"] | components["schemas"]["ConnectionCustomDraftSpec"];
        } | {
            display_name: string;
            /** @enum {string} */
            kind: "mcp";
            same_identity?: boolean;
            spec: components["schemas"]["ConnectionMCPHTTPDraftSpec"] | components["schemas"]["ConnectionMCPStdioDraftSpec"];
        } | {
            display_name: string;
            /** @enum {string} */
            kind: "search";
            same_identity?: boolean;
            spec: components["schemas"]["ConnectionSearchDraftSpec"];
        };
        ConnectionCredentialReplacement: {
            environment?: string;
            /** @constant */
            same_identity: true;
            secret: string;
        };
        ConnectionCredentialReplacementResult: {
            connection: components["schemas"]["Connection"];
            replayed: boolean;
        };
        ConnectionCustomDraftSpec: {
            configuration: {
                credential_ref?: string;
                endpoint: string;
                header?: string;
            };
            provider_id: string;
            /** @enum {string} */
            shape: "custom";
        };
        ConnectionCustomModel: {
            authentication_configured: boolean;
            endpoint: string;
            provider_id: string;
            /** @constant */
            shape: "custom";
        };
        ConnectionEditDraft: {
            display_name: string;
            /** @enum {string} */
            kind: "model";
            /** @constant */
            same_identity: true;
            spec: components["schemas"]["ConnectionStandardDraftSpec"] | components["schemas"]["ConnectionZAIDraftSpec"] | components["schemas"]["ConnectionBedrockDraftSpec"] | components["schemas"]["ConnectionCustomDraftSpec"];
        } | {
            display_name: string;
            /** @enum {string} */
            kind: "mcp";
            /** @constant */
            same_identity: true;
            spec: components["schemas"]["ConnectionMCPHTTPDraftSpec"] | components["schemas"]["ConnectionMCPStdioDraftSpec"];
        } | {
            display_name: string;
            /** @enum {string} */
            kind: "search";
            /** @constant */
            same_identity: true;
            spec: components["schemas"]["ConnectionSearchDraftSpec"];
        };
        ConnectionMCPCatalog: components["schemas"]["ConnectionMCPCatalogTool"][];
        ConnectionMCPCatalogTool: {
            connection_id: string;
            connection_revision: string;
            name: string;
            remote_name: string;
        };
        ConnectionMCPHTTP: {
            authentication_configured: boolean;
            endpoint: string;
            execution_identity: string;
            /** @constant */
            identity_verified: false;
            permission_summary: string;
            /** @constant */
            transport: "streamable_http";
        };
        ConnectionMCPHTTPDraftSpec: {
            configuration: {
                credential_ref?: string;
                endpoint: string;
            };
            /** @description Operator-declared upstream identity; unverified. */
            execution_identity?: string;
            /** @description Operator-declared permissions; not proof of tool safety. */
            permission_summary?: string;
            /** @enum {string} */
            transport: "streamable_http";
        };
        ConnectionMCPStdio: {
            argument_count: number;
            environment_binding_count: number;
            executable_name: string;
            execution_identity: string;
            /** @constant */
            identity_verified: false;
            permission_summary: string;
            /** @constant */
            transport: "stdio";
        };
        ConnectionMCPStdioDraftSpec: {
            configuration: {
                args?: string[];
                env_refs?: {
                    [key: string]: string;
                };
                executable: string;
            };
            /** @description Operator-declared upstream identity; unverified. */
            execution_identity?: string;
            /** @description Operator-declared permissions; not proof of tool safety. */
            permission_summary?: string;
            /** @enum {string} */
            transport: "stdio";
        };
        ConnectionMutationProblem: components["schemas"]["ControlProblem"] & {
            consumers?: components["schemas"]["ConnectionReference"][];
            current?: components["schemas"]["Connection"];
        };
        ConnectionReference: {
            /** @enum {string} */
            kind: "model" | "mcp" | "search";
            mount?: string;
            route?: string;
            target?: string;
            workspace: string;
        };
        ConnectionReferences: {
            connection_id: string;
            connection_revision: string;
            consumers: components["schemas"]["ConnectionReference"][];
        };
        ConnectionSearch: {
            authentication_configured: boolean;
            endpoint: string;
            provider_id: string;
        };
        ConnectionSearchDraftSpec: {
            credential_ref?: string;
            endpoint: string;
            provider_id: string;
        };
        ConnectionStandardDraftSpec: {
            configuration: {
                credential_ref: string;
                endpoint?: string;
            };
            provider_id: string;
            /** @enum {string} */
            shape: "standard";
        };
        ConnectionStandardModel: {
            authentication_configured: boolean;
            endpoint?: string;
            provider_id: string;
            /** @constant */
            shape: "standard";
        };
        ConnectionZAIDraftSpec: {
            configuration: {
                /** @enum {string} */
                access: "general_api" | "coding_plan";
                credential_ref: string;
            };
            provider_id: string;
            /** @enum {string} */
            shape: "zai";
        };
        ConnectionZAIModel: {
            access: string;
            authentication_configured: boolean;
            provider_id: string;
            /** @constant */
            shape: "zai";
        };
        ControlProblem: {
            code: string;
            detail?: string;
            fields?: {
                [key: string]: string;
            };
            instance?: string;
            recovery?: {
                action: string;
            };
            status: number;
            title: string;
            /** Format: uri */
            type: string;
        };
        CredentialIssue: {
            access: components["schemas"]["AccessIdentity"];
            credential: components["schemas"]["PrincipalCredential"];
            secret_not_recoverable?: boolean;
            token?: string;
        };
        CredentialIssueRequest: {
            workspace: string;
        };
        CredentialReview: {
            approval_class: string;
            credential: components["schemas"]["PrincipalCredential"];
            principal: string;
            revision: string;
            warning: string;
        };
        CredentialRevoked: {
            credential: string;
            principal: components["schemas"]["PrincipalAuthority"];
            replayed: boolean;
            revoked: boolean;
        };
        DetectedClient: {
            id: string;
            name: string;
        };
        MCPMount: {
            alias: string;
            /** @enum {string} */
            builtin?: "swobu-admin";
            connection_id: string;
            selected_tools: string[];
        } & ({
            connection_id: string;
        } | {
            /** @enum {string} */
            builtin: "swobu-admin";
            /** @enum {string} */
            connection_id?: "";
        });
        MCPMountDraft: {
            alias?: string;
            /** @enum {string} */
            builtin?: "swobu-admin";
            connection_id?: string;
            review?: components["schemas"]["MCPMountReview"];
            selected_tools: string[];
        } & ({
            connection_id: string;
        } | {
            /** @enum {string} */
            builtin: "swobu-admin";
            /** @enum {string} */
            connection_id?: "";
        });
        MCPMountPreview: {
            access_revision: string;
            affected_principals: components["schemas"]["MCPMountPrincipal"][];
            alias: string;
            allowed_tools: string[];
            approval_class: string;
            /** @enum {string} */
            builtin?: "swobu-admin";
            connection_id: string;
            connection_revision: string;
            digest: string;
            endpoint: string;
            execution_identity: string;
            existing_mount?: components["schemas"]["WorkspaceMCPMount"];
            identity_verified: boolean;
            permission_summary: string;
            potentially_effectful_tools: string[];
            publication_revision: string;
            published: boolean;
            revision: string;
            warning: string;
            workspace: string;
        } & ({
            connection_id: string;
        } | {
            /** @enum {string} */
            builtin: "swobu-admin";
            /** @enum {string} */
            connection_id?: "";
        });
        MCPMountPrincipal: {
            id: string;
            label: string;
        };
        MCPMountReview: {
            access_revision: string;
            connection_revision: string;
            digest: string;
            publication_revision: string;
            workspace_etag: string;
        };
        ModelRouteMatch: {
            matched: boolean;
            pattern?: string;
            priority?: number;
            targets?: number;
            tiers?: number;
        };
        ModelRouteOrder: {
            order: string[];
        };
        ModelRouteSpec: {
            tiers: components["schemas"]["WorkspaceRouteTier"][];
        };
        NodeOverview: {
            budget_policy_count: number;
            /** @enum {string} */
            configuration_mode: "managed" | "externally_managed";
            connection_count: number;
            principal_count: number;
            publication_count: number;
            revision: string;
            workspace_count: number;
        };
        PermissionDraft: {
            /** @enum {string} */
            action?: "use";
            /** Format: date-time */
            expires_at?: string;
            request_limits?: components["schemas"]["ServiceRequestLimit"][];
            services?: ("models" | "mcp" | "search")[];
        };
        PermissionReview: {
            affected_credentials: number;
            after: components["schemas"]["PrincipalPermission"] | null;
            approval_class: string;
            before: components["schemas"]["PrincipalPermission"] | null;
            operation: string;
            principal: string;
            revision: string;
            warning: string;
            workspace: string;
        };
        PermissionState: {
            allowed: boolean;
            permission: components["schemas"]["PrincipalPermission"] | null;
            principal: string;
            replayed?: boolean;
            revision: string;
            workspace: string;
        };
        PrincipalAuthority: {
            /** Format: date-time */
            created_at?: string;
            id: string;
            kind: string;
            label: string;
            permissions: components["schemas"]["PrincipalPermission"][];
            revision: string;
            status: string;
        };
        PrincipalCredential: {
            /** Format: date-time */
            created_at: string;
            id: string;
            workspace: string;
        };
        PrincipalLabel: {
            label: string;
        };
        PrincipalPermission: {
            /** @enum {string} */
            action: "use";
            /** Format: date-time */
            expires_at?: string;
            request_limits?: components["schemas"]["ServiceRequestLimit"][];
            services?: ("models" | "mcp" | "search")[];
            workspace: string;
        };
        PrincipalRevocation: {
            credential_count: number;
            principal: components["schemas"]["PrincipalAuthority"];
            replayed: boolean;
        };
        RemoteAddress: {
            id: string;
            managed: boolean;
            url?: string;
        };
        SearchRoute: {
            connection_id: string;
            fallback_connection_ids: string[];
        };
        ServiceActivationIntent: {
            review_digest?: string;
        };
        ServiceActivationReview: {
            access_revision: string;
            affected_principals: components["schemas"]["ServiceAffectedPrincipal"][];
            digest: string;
            publication_revision: string;
            published: boolean;
            requires_confirmation: boolean;
            /** @enum {string} */
            service: "models" | "mcp" | "search";
            warning?: string;
            workspace_etag: string;
        };
        ServiceAffectedPrincipal: {
            id: string;
            label: string;
        };
        ServiceRequestLimit: {
            requests_per_minute: number;
            /** @enum {string} */
            service: "models" | "mcp" | "search";
        };
        SpendApproval: {
            digest: string;
            expires_at: string;
            id: string;
            operation: string;
            revision: string;
            used: boolean;
            workspace: string;
        };
        SpendPolicy: {
            amount_cents: number;
            /** @enum {string} */
            currency: "USD";
            /** @enum {string} */
            mode: "stop_new_billable_work" | "observe_only";
            /** @enum {string} */
            unknown_price: "reject";
            /** @enum {string} */
            window: "day" | "month";
        };
        SpendStatus: {
            charged_cents: string;
            currency: string;
            limit: components["schemas"]["SpendPolicy"] | null;
            partial_quantity_operations?: number;
            remaining_cents: string | null;
            reserved_cents: string;
            settled_cents: string;
            uncertain_cents: string;
            unknown_price_operations?: number;
            unquoted_operations?: number;
            window_end: string;
            window_key: string;
            window_start: string;
            workspace: string;
        };
        WorkspaceCreateDraft: {
            display_name?: string;
            initial_route?: string;
            slug: string;
            target?: components["schemas"]["WorkspaceRouteTarget"];
        };
        WorkspaceDeletionPreview: {
            approval_class: string;
            diff: string;
            management_etag: string;
            management_revision: number;
            route_count: number;
            warning: string;
            workspace: string;
        };
        WorkspaceDeletionReceipt: {
            deleted: boolean;
        };
        WorkspaceDisplayNameDraft: {
            display_name: string;
        };
        WorkspaceMCPMount: {
            alias: string;
            allowed_tools: string[] | null;
            /** @enum {string} */
            builtin?: "swobu-admin";
            connection_id: string;
        } & ({
            connection_id: string;
        } | {
            /** @enum {string} */
            builtin: "swobu-admin";
            /** @enum {string} */
            connection_id?: "";
        });
        WorkspaceProjection: {
            display_name: string;
            enabled_services: ("models" | "mcp" | "search")[];
            management_etag: string;
            management_revision: number;
            mcp_mounts?: components["schemas"]["WorkspaceMCPMount"][];
            readiness: components["schemas"]["WorkspaceReadiness"];
            routes: components["schemas"]["WorkspaceRoute"][];
            service_limits?: components["schemas"]["WorkspaceServiceLimits"];
            slug: string;
        };
        WorkspacePublication: {
            address: string;
            published: boolean;
            revision: string;
            url?: string;
            workspace: string;
        };
        WorkspaceReadiness: {
            llm: string;
            mcp: string;
            search?: string;
        };
        WorkspaceRoute: {
            name: string;
            tiers: components["schemas"]["WorkspaceRouteTier"][];
        };
        WorkspaceRouteTarget: {
            connection_id: string;
            id: string;
            model: string;
            protocol: string;
        };
        WorkspaceRouteTier: {
            targets: components["schemas"]["WorkspaceRouteTarget"][];
        };
        WorkspaceServiceError: {
            code: string;
            message: string;
        };
        WorkspaceServiceLimits: {
            mcp: components["schemas"]["WorkspaceServiceQuota"];
            models: components["schemas"]["WorkspaceServiceQuota"];
            search: components["schemas"]["WorkspaceServiceQuota"];
        };
        WorkspaceServiceQuota: {
            concurrent?: number;
            requests_per_minute: number;
        };
        WorkspaceServiceState: {
            activation_review?: components["schemas"]["ServiceActivationReview"];
            configured: boolean;
            enabled: boolean;
            /** @enum {string} */
            kind: "models" | "mcp" | "search";
        };
        WorkspaceSummary: {
            display_name: string;
            route_count: number;
            slug: string;
        };
    };
    responses: never;
    parameters: {
        /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
        IdempotencyKey: string;
        /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
        IfMatch: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    get_api_v1_activity: {
        parameters: {
            query?: {
                workspace?: string;
                kind?: "exchange" | "mcp_operation";
                limit?: number;
                exchange_cursor?: string;
                mcp_cursor?: string;
                from?: string;
                to?: string;
                principal?: string;
                result?: "succeeded" | "client_cancelled" | "client_write_failed" | "provider_stream_failed" | "checkpoint_commit_failed" | "exchange_failed";
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Live local Node observation; Cache-Control: no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ActivityProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_activity_export: {
        parameters: {
            query: {
                workspace?: string;
                kind: "exchange" | "mcp_operation";
                format?: "ndjson" | "csv";
                max_rows?: number;
                cursor?: string;
                from?: string;
                to?: string;
                principal?: string;
                result?: "succeeded" | "client_cancelled" | "client_write_failed" | "provider_stream_failed" | "checkpoint_commit_failed" | "exchange_failed";
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Bounded retained data, with optional X-Swobu-Next-Cursor, X-Swobu-Export-Rows and Cache-Control: no-store. */
            200: {
                headers: {
                    /** @description retained-page: not a complete or immutable database snapshot. */
                    "X-Swobu-Export-Mode"?: "retained-page";
                    /** @description Number of records in this response. */
                    "X-Swobu-Export-Rows"?: number;
                    /** @description Opaque continuation for the same Workspace and kind. */
                    "X-Swobu-Next-Cursor"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/x-ndjson": string;
                    "text/csv": string;
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_activity_operations_id: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Live local Node observation; Cache-Control: no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ActivityOperationDetail"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_activity_stream: {
        parameters: {
            query?: {
                workspace?: string;
                kind?: "exchange" | "mcp_operation";
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Server-sent events, each containing a bounded Activity observation */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": string;
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_browser_launches: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Created one-shot native browser launch */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BrowserLaunch"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_browser_session: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BrowserSessionExchange"];
            };
        };
        responses: {
            /** @description Local browser session cookie issued (no response body) */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_client_configurations: {
        parameters: {
            query: {
                dry_run: boolean;
            };
            header?: {
                /** @description Required only for apply */
                "If-Match"?: string;
                /** @description Required only for apply (16-256 bytes) */
                "Idempotency-Key"?: string;
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ClientConfigurationRequest"];
            };
        };
        responses: {
            /** @description Live local Node observation; Cache-Control: no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ClientConfigurationPlan"] | components["schemas"]["ClientConfigurationApplyResult"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_clients: {
        parameters: {
            query: {
                workspace: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Live local Node observation; Cache-Control: no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DetectedClient"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_connections: {
        parameters: {
            query?: {
                kind?: "model" | "mcp" | "search";
                q?: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current redacted Connection view */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Connection"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_connections: {
        parameters: {
            query?: never;
            header: {
                /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectionCreateDraft"];
            };
        };
        responses: {
            /** @description Original created Connection with replay acknowledgement */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionCredentialReplacementResult"];
                };
            };
            /** @description Canonical failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_connections_id: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current redacted Connection view */
            200: {
                headers: {
                    /** @description Strong quoted resource revision */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Connection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_connections_id: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Connection deleted; no response body */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            /** @description Exact consumers prevent deletion */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ConnectionMutationProblem"];
                };
            };
            /** @description Revision changed; current is an advisory redacted reload summary */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ConnectionMutationProblem"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    patch_api_v1_connections_id: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectionEditDraft"];
            };
        };
        responses: {
            /** @description Committed redacted Connection */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Connection"];
                };
            };
            /** @description Canonical failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_connections_id_checks: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Sanitized observation for the reviewed revision */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionCheck"];
                };
            };
            /** @description Connection changed before or during the probe */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ConnectionMutationProblem"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_connections_id_credential_replacements: {
        parameters: {
            query?: never;
            header: {
                /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConnectionCredentialReplacement"];
            };
        };
        responses: {
            /** @description Original committed redacted result */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionCredentialReplacementResult"];
                };
            };
            /** @description Reviewed revision changed */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ConnectionMutationProblem"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_connections_id_mcp_catalog: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current MCP catalog for the Connection */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionMCPCatalog"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_connections_id_references: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current redacted Connection view */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConnectionReferences"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_node: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Committed Node authority, without private material */
            200: {
                headers: {
                    /** @description Strong whole-Node revision (quoted SHA-256) */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NodeOverview"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_principals: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalAuthority"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_principals: {
        parameters: {
            query?: never;
            header: {
                /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PrincipalLabel"];
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalAuthority"];
                };
            };
            /** @description Admitted local Operator state; no-store */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalAuthority"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_principals_id: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalAuthority"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_principals_id: {
        parameters: {
            query?: {
                dry_run?: boolean;
            };
            header: {
                "If-Match": string;
                "Idempotency-Key"?: string;
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": Record<string, never>;
            };
        };
        responses: {
            /** @description Reviewed Principal revocation or committed, revisioned result */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalRevocation"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    patch_api_v1_principals_id: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PrincipalLabel"];
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalAuthority"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_principals_id_credentials: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PrincipalCredential"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_principals_id_credentials: {
        parameters: {
            query?: never;
            header: {
                /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path: {
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CredentialIssueRequest"];
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CredentialIssue"];
                };
            };
            /** @description Admitted local Operator state; no-store */
            201: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CredentialIssue"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_principals_id_credentials_credential: {
        parameters: {
            query?: {
                /** @description true previews the exact impact without committing or reserving a retry key */
                dry_run?: boolean;
            };
            header?: {
                "If-Match"?: string;
                "Idempotency-Key"?: string;
            };
            path: {
                id: string;
                credential: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": Record<string, never>;
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CredentialReview"] | components["schemas"]["CredentialRevoked"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_remote_addresses: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RemoteAddress"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current Workspace summaries */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceSummary"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_workspaces: {
        parameters: {
            query?: never;
            header: {
                /** @description Original retry identity. Durable semantic replay/conflict is owned by the application transaction. */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["WorkspaceCreateDraft"];
            };
        };
        responses: {
            /** @description Committed Workspace projection */
            200: {
                headers: {
                    /** @description Strong quoted Workspace management revision */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Committed Workspace projection */
            201: {
                headers: {
                    /** @description Strong quoted Workspace management revision */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Committed Workspace projection */
            200: {
                headers: {
                    /** @description Strong quoted Workspace management revision */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug: {
        parameters: {
            query?: {
                dry_run?: boolean;
            };
            header?: {
                "If-Match"?: string;
                "Idempotency-Key"?: string;
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": Record<string, never>;
            };
        };
        responses: {
            /** @description Deletion preview or committed deletion acknowledgement */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceDeletionPreview"] | components["schemas"]["WorkspaceDeletionReceipt"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    patch_api_v1_workspaces_slug: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["WorkspaceDisplayNameDraft"];
            };
        };
        responses: {
            /** @description Committed Workspace projection */
            200: {
                headers: {
                    /** @description Strong quoted Workspace management revision */
                    ETag: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_limits: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpendPolicy"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_limits: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": null;
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_workspaces_slug_limits_apply: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": {
                    approval_id: string;
                    limit: components["schemas"]["SpendPolicy"];
                };
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpendPolicy"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    post_api_v1_workspaces_slug_limits_preview: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SpendPolicy"];
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpendApproval"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_limits_status: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpendStatus"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_mcp_mounts: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Mounts */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MCPMount"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_mcp_mounts_alias: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
                alias: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Mount */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MCPMount"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_mcp_mounts_alias: {
        parameters: {
            query: {
                dry_run: boolean;
            };
            header?: {
                /** @description Required when dry_run=false; must equal the reviewed Workspace ETag. */
                "If-Match"?: string;
            };
            path: {
                slug: string;
                alias: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["MCPMountDraft"];
            };
        };
        responses: {
            /** @description Review or committed mount */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MCPMountPreview"] | components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_mcp_mounts_alias: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
                alias: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workspace */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_model_match: {
        parameters: {
            query: {
                model: string;
            };
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelRouteMatch"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_model_routes: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceRoute"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    patch_api_v1_workspaces_slug_model_routes: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ModelRouteOrder"];
            };
        };
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Current or committed Models route */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_model_routes_route: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
                route: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModelRouteSpec"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_model_routes_route: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
                route: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ModelRouteSpec"];
            };
        };
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Current or committed Models route */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_model_routes_route: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
                route: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current or committed Models route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_permissions_id: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PermissionState"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_permissions_id: {
        parameters: {
            query?: {
                /** @description true previews the exact impact without committing or reserving a retry key */
                dry_run?: boolean;
            };
            header?: {
                "If-Match"?: string;
                "Idempotency-Key"?: string;
            };
            path: {
                slug: string;
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["PermissionDraft"];
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PermissionReview"] | components["schemas"]["PermissionState"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_permissions_id: {
        parameters: {
            query?: {
                /** @description true previews the exact impact without committing or reserving a retry key */
                dry_run?: boolean;
            };
            header?: {
                "If-Match"?: string;
                "Idempotency-Key"?: string;
            };
            path: {
                slug: string;
                id: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": Record<string, never>;
            };
        };
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PermissionReview"] | components["schemas"]["PermissionState"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_publications: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspacePublication"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_publications_remote_address_id: {
        parameters: {
            query?: never;
            header?: {
                "If-Match"?: string;
            };
            path: {
                slug: string;
                remote_address_id: "relay";
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspacePublication"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_publications_remote_address_id: {
        parameters: {
            query?: never;
            header?: {
                "If-Match"?: string;
            };
            path: {
                slug: string;
                remote_address_id: "relay";
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Admitted local Operator state; no-store */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspacePublication"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_search_route: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Configured Search route */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SearchRoute"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_search_route: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SearchRoute"];
            };
        };
        responses: {
            /** @description Workspace */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_search_route: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Workspace */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_service_limits: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Current operational limits */
            200: {
                headers: {
                    /** @description Current Workspace management ETag */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceLimits"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_service_limits: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["WorkspaceServiceLimits"];
            };
        };
        responses: {
            /** @description Current operational limits */
            200: {
                headers: {
                    /** @description Current Workspace management ETag */
                    ETag?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceLimits"];
                };
            };
            /** @description Workspace ETag stale */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Workspace ETag required */
            428: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    get_api_v1_workspaces_slug_services: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                slug: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Three current Workspace Services */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceState"][];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    put_api_v1_workspaces_slug_services_service: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
                service: "models" | "mcp" | "search";
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ServiceActivationIntent"];
            };
        };
        responses: {
            /** @description Updated Workspace */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Configured children block disabling this Service */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Workspace revision stale */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Existing grants or publication require an activation review */
            428: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
    delete_api_v1_workspaces_slug_services_service: {
        parameters: {
            query?: never;
            header: {
                /** @description One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction. */
                "If-Match": components["parameters"]["IfMatch"];
            };
            path: {
                slug: string;
                service: "models" | "mcp" | "search";
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Updated Workspace */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceProjection"];
                };
            };
            /** @description Configured children block disabling this Service */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Workspace revision stale */
            412: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["WorkspaceServiceError"];
                };
            };
            /** @description Canonical control failure */
            default: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["ControlProblem"];
                };
            };
        };
    };
}
