package x365

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Node is a validated X365 endpoint. Its exported fields contain private
// account and provider material and are not safe for public diagnostics.
type Node struct {
	Name      string
	UUID      string
	Server    string
	Port      int
	Path      string
	Host      string
	SNI       string
	PublicKey string
	ShortID   string

	realityFingerprint string
}

// ParseNode validates an x365 URI and returns its normalized endpoint fields.
func ParseNode(name, rawURI string) (Node, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return Node{}, errors.New("invalid x365 URL")
	}
	if u.Scheme != "x365" {
		return Node{}, fmt.Errorf("not an x365 URL: %s", u.Scheme)
	}
	portText := u.Port()
	if u.User == nil || u.Hostname() == "" || portText == "" {
		return Node{}, errors.New("missing user, host, or port")
	}
	uuid := u.User.Username()
	if _, err := ParseUserID(uuid); err != nil {
		return Node{}, fmt.Errorf("invalid user id: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return Node{}, fmt.Errorf("invalid port: %q", portText)
	}
	q := u.Query()
	missing := missingQueryFields(q, "path", "host", "sni", "pbk", "sid")
	if len(missing) != 0 {
		return Node{}, fmt.Errorf("missing query fields: %s", strings.Join(missing, ", "))
	}
	if _, err := parseRealityPublicKey(q.Get("pbk")); err != nil {
		return Node{}, err
	}
	if _, err := parseRealityShortID(q.Get("sid")); err != nil {
		return Node{}, err
	}
	return Node{
		Name:      strings.TrimSpace(name),
		UUID:      uuid,
		Server:    u.Hostname(),
		Port:      port,
		Path:      normalizeNodePath(q.Get("path")),
		Host:      q.Get("host"),
		SNI:       q.Get("sni"),
		PublicKey: q.Get("pbk"),
		ShortID:   q.Get("sid"),

		realityFingerprint: q.Get("fp"),
	}, nil
}

// UserID decodes the node's UUID into X365 request material.
func (n Node) UserID() (UserID, error) {
	userID, err := ParseUserID(n.UUID)
	if err != nil {
		return UserID{}, fmt.Errorf("invalid user id: %w", err)
	}
	return userID, nil
}

// Label returns the node name and endpoint summary for private UI or log output.
func (n Node) Label() string {
	return fmt.Sprintf("%s %s", n.Name, n.ServerSummary())
}

// ServerSummary returns provider endpoint fields for private UI display.
func (n Node) ServerSummary() string {
	return fmt.Sprintf("%s:%d path=%s host=%s sni=%s", n.Server, n.Port, n.Path, n.Host, n.SNI)
}

// Normalize for Referer concatenation; url.URL inserts the slash for the
// request URL, but the header does not.
func normalizeNodePath(path string) string {
	if path == "" || strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func missingQueryFields(q url.Values, fields ...string) []string {
	missing := make([]string, 0, len(fields))
	for _, field := range fields {
		if q.Get(field) == "" {
			missing = append(missing, field)
		}
	}
	return missing
}
