package clientprofile

import "strings"

// Catalog returns all supported client profiles.
func Catalog() []Profile {
	specs := capabilityCatalog()
	profiles := make([]Profile, 0, len(specs))
	for _, spec := range specs {
		profiles = append(profiles, profileSpecAdapter{spec: profileSpecFromCapability(spec)})
	}
	return profiles
}

// FindByLabel returns the matching profile by exact trimmed label.
func FindByLabel(profiles []Profile, label string) Profile {
	label = strings.TrimSpace(label) // trimlowerlint:allow boundary canonicalization
	if label == "" {
		return nil
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Identity().Label) == label { // trimlowerlint:allow boundary canonicalization
			return profile
		}
	}
	return nil
}

// FindByID returns the matching profile by exact trimmed id.
func FindByID(profiles []Profile, id string) Profile {
	id = strings.TrimSpace(id) // trimlowerlint:allow boundary canonicalization
	if id == "" {
		return nil
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Identity().ID) == id { // trimlowerlint:allow boundary canonicalization
			return profile
		}
	}
	return nil
}
