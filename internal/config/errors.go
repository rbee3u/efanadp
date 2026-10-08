package config

// ErrorKind selects startup guidance independently of diagnostic wording or
// private values.
type ErrorKind uint8

const (
	UUIDPlaceholder ErrorKind = iota + 1
	InvalidUUID
	MissingUUID
	MissingProxies
	TemplateBase64
	TemplateTooLarge
	TemplateCompression
	TemplateYAML
	TemplateEmpty
	TemplateUserInfo
	UnsupportedTemplateURL
	UnsupportedURL
	TemplateListShape
	ProxyListShape
	EmptyTemplateList
	EmptyProxyList
	InvalidYAML
	NoNodes
)

// Error carries a stable category and the existing sanitized diagnostic.
type Error struct {
	Kind ErrorKind
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func configError(kind ErrorKind, err error) error { return &Error{Kind: kind, Err: err} }
