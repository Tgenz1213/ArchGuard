package cli

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

// origin is a URL's scheme, host and port, so a key sent to one never reaches another scheme or port on the same host.
type origin struct {
	scheme, host, port string
}

func (o origin) String() string {
	return o.scheme + "://" + net.JoinHostPort(o.host, o.port)
}

func originOf(u *url.URL) origin {
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}

	return origin{scheme: u.Scheme, host: strings.ToLower(u.Hostname()), port: port}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

type askingNode struct {
	asker   inference.Asker
	baseURL string
	keySent bool
}

type systemOneAskers struct {
	nodes     map[string]askingNode
	keyOrigin origin
}

func (a *systemOneAskers) ForNode(n config.Node) inference.Asker {
	return a.nodes[n.Name].asker
}

func (a *systemOneAskers) asks() bool {
	return len(a.nodes) > 0
}

func (a *systemOneAskers) metered(recorder telemetry.Recorder) *systemOneAskers {
	metered := &systemOneAskers{nodes: make(map[string]askingNode, len(a.nodes)), keyOrigin: a.keyOrigin}
	for name, n := range a.nodes {
		n.asker = telemetry.NewAsker(n.asker, recorder)
		metered.nodes[name] = n
	}

	return metered
}

func (a *systemOneAskers) rejectedKeyError(rejected *analysis.KeyRejectedError) error {
	n, ok := a.nodes[rejected.Node]
	if !ok {
		return fmt.Errorf("the %s node's server rejected its key: %w", rejected.Node, rejected)
	}

	if n.keySent {
		return fmt.Errorf("%s was rejected by %s (node %s); set a valid key and run the check again: %w", config.SystemOneAPIKeyEnv, n.baseURL, rejected.Node, rejected)
	}

	return fmt.Errorf("%s (node %s) needs a key, and %s is sent only to %s; set %s to that server's origin, with the key set: %w",
		n.baseURL, rejected.Node, config.SystemOneAPIKeyEnv, a.keyOrigin, config.SystemOneKeyOriginEnv, rejected)
}

// keyOriginEnv is read before .env is loaded, so a .env committed in a pull request can't name where the CI's key goes.
func newAskers(nodes []config.Node, keyOriginEnv string) (*systemOneAskers, error) {
	askers := &systemOneAskers{nodes: map[string]askingNode{}}

	if !slices.ContainsFunc(nodes, func(n config.Node) bool { return n.Job == config.JobAsk }) {
		return askers, nil
	}

	apiKey := os.Getenv(config.SystemOneAPIKeyEnv)

	keyOrigin, err := systemOneKeyOrigin(keyOriginEnv, apiKey != "")
	if err != nil {
		return nil, err
	}

	askers.keyOrigin = keyOrigin

	for _, n := range nodes {
		if n.Job != config.JobAsk {
			continue
		}

		built, err := buildAskingNode(n, apiKey, keyOrigin)
		if err != nil {
			return nil, err
		}

		askers.nodes[n.Name] = built
	}

	return askers, nil
}

func buildAskingNode(n config.Node, apiKey string, keyOrigin origin) (askingNode, error) {
	parsed, err := url.Parse(n.BaseURL)
	if err != nil {
		return askingNode{}, fmt.Errorf("analysis.pipeline node %q.base_url: %w", n.Name, err)
	}

	toKeyOrigin := originOf(parsed) == keyOrigin
	if toKeyOrigin && apiKey == "" {
		return askingNode{}, fmt.Errorf("analysis.pipeline node %q asks %s, which needs the %s environment variable", n.Name, n.BaseURL, config.SystemOneAPIKeyEnv)
	}

	key := ""
	if toKeyOrigin {
		key = apiKey
	}

	provider, err := inference.NewSystemOneProvider(n.BaseURL, key, n.Model, n.Timeout)
	if err != nil {
		return askingNode{}, fmt.Errorf("analysis.pipeline node %q: %w", n.Name, err)
	}

	return askingNode{asker: provider, baseURL: n.BaseURL, keySent: toKeyOrigin}, nil
}

// The key's origin never comes from archguard.yaml, so a change to the config can't send a CI secret to another server.
func systemOneKeyOrigin(raw string, keySet bool) (origin, error) {
	if raw == "" {
		raw = config.DefaultSystemOneBaseURL
	}

	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Opaque != "" ||
		parsed.User != nil || strings.TrimSuffix(parsed.Path, "/") != "" || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") {
		return origin{}, fmt.Errorf("%s: %q must be an http or https origin such as http://127.0.0.1:8009, with no path, user, query or fragment", config.SystemOneKeyOriginEnv, raw)
	}

	keyOrigin := originOf(parsed)
	if keySet && keyOrigin.scheme == "http" && !isLoopback(keyOrigin.host) {
		return origin{}, fmt.Errorf("%s would be sent over plain http to %s; use https, or a server on this machine", config.SystemOneAPIKeyEnv, keyOrigin)
	}

	return keyOrigin, nil
}
