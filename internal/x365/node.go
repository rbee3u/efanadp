package x365

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

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

func (n Node) UserID() (UserID, error) {
	userID, err := ParseUserID(n.UUID)
	if err != nil {
		return UserID{}, fmt.Errorf("invalid user id: %w", err)
	}
	return userID, nil
}

func (n Node) Label() string {
	return fmt.Sprintf("%s %s", n.Name, n.ServerSummary())
}

func (n Node) ServerSummary() string {
	return fmt.Sprintf("%s:%d path=%s host=%s sni=%s", n.Server, n.Port, n.Path, n.Host, n.SNI)
}

// normalizeNodePath gives the path a leading slash.
//
// url.URL.String inserts the separator itself, so a path without one still
// produces a correct request URL. The Referer header is built by concatenating
// host and path, though, and would silently lose the separator, sending
// "https://example.orgnode" on a transport whose whole purpose is to look like
// ordinary browser traffic. Normalizing here rather than rejecting keeps a
// config that works today working.
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
