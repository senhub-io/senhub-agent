package extension

// CatalogParam is one parameter of a probe type in the catalogue.
type CatalogParam struct {
	Name     string
	Kind     string // "string","int","float","bool","duration","string_list","block","map","block_list"
	Required bool
	Secret   bool
}

// CatalogProbe is one registered probe type with its parameter specs.
type CatalogProbe struct {
	Type   string
	Params []CatalogParam
}
