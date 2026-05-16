bind_addr: *"127.0.0.1:7926" | string

endpoints: *[] | [...#Endpoint]

#Endpoint: {
	name!: string
	selected_provider_config_ref!: string
	provider_configs!: [...#ProviderConfig]
}

#ProviderConfig: {
	ref!: string
	provider_spec!: string
	base_url?: string
	credential_ref?: string
	model_id?: string
	target_alias?: string
	protocol_kind?: string
	selected_frame?: string
	if provider_spec == "openai_compatible" {
		base_url!: string
	}
}
