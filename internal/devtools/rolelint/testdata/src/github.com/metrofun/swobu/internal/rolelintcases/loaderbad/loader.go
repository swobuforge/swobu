package loaderbad

type Loader interface {
	Load() string
}

type Catalog struct{} // want `struct "Catalog" is used most often as "Loader"; include that interface noun in the concrete name`

func (Catalog) Load() string { return "ok" }

func build() Loader {
	return Catalog{}
}
