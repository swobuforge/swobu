package loaderok

type Loader interface {
	Load() string
}

type CatalogLoader struct{}

func (CatalogLoader) Load() string { return "ok" }

func build() Loader {
	return CatalogLoader{}
}
