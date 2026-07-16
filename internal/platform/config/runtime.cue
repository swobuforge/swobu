bind_addr: *"127.0.0.1:7926" | string
patch_diagnostic_thresholds: #PatchDiagnosticThresholdsConfig

endpoints: *[] | [...#Endpoint]

#PatchDiagnosticThresholdsConfig: {
	min_repeated_decode_mutations: *2 | int & >=1
	min_noop_ratio_population:     *3 | int & >=1
	noop_ratio_percent_threshold:  *80 | int & >=1 & <=100
}

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
