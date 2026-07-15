package clientprofile

// Catalog returns all supported client profiles.
func Catalog() []Profile {
	specs := capabilityCatalog()
	profiles := make([]Profile, 0, len(specs))
	for _, spec := range specs {
		profiles = append(profiles, profileSpecAdapter{spec: profileSpecFromCapability(spec)})
	}
	return profiles
}
