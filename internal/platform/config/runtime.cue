bind_addr: *"127.0.0.1:7777" | string

endpoints: *[] | [...#Endpoint]

#Endpoint: {
	name!: string
	selected_provider_config_ref!: string
	provider_configs!: [...#ProviderConfig]
}

#ProviderConfig: {
	ref!: string
	provider_spec!: string
	protocol_kind!: "chat_completions" | "responses" | "completions" | "messages"
	base_url?: string
	credential_ref?: string
	model_id?: string
	target_alias?: string
	if provider_spec == "custom" {
		base_url!: string
	}
}
