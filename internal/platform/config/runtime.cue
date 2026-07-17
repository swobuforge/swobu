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
	auth_header?: string
	credential_ref?: string
	route_model_id?: string
	model_id?: string
	target_alias?: string
	target_rank?: int & >=1
	target_weight?: int & >=1
	provider_protocol?: string
	if provider_spec == "openai_compatible" {
		base_url!: string
	}
}
