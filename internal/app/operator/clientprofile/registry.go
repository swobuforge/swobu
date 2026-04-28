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
// TODO this is only used in view, so it should own it as well
func FindByLabel(profiles []Profile, label string) Profile {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Identity().Label) == label {
			return profile
		}
	}
	return nil
}

// FindByID returns the matching profile by exact trimmed id.
func FindByID(profiles []Profile, id string) Profile {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Identity().ID) == id {
			return profile
		}
	}
	return nil
}
