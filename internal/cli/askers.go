package cli

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
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
		return rejected
	}

	if n.keySent {
		return fmt.Errorf("%s was rejected by %s (node %s); set a valid key and run the check again", config.SystemOneAPIKeyEnv, n.baseURL, rejected.Node)
	}

	return fmt.Errorf("%s (node %s) needs a key; %s is sent only to %s, so set %s to that server's origin", n.baseURL, rejected.Node, config.SystemOneAPIKeyEnv, a.keyOrigin, config.SystemOneKeyOriginEnv)
}

func newAskers(nodes []config.Node, injected func(config.Node) inference.Asker) (*systemOneAskers, error) {
	askers := &systemOneAskers{nodes: map[string]askingNode{}}

	asks := 0
	for _, n := range nodes {
		if n.Job == config.JobAsk {
			asks++
		}
	}

	if asks == 0 {
		return askers, nil
	}

	apiKey := os.Getenv(config.SystemOneAPIKeyEnv)

	keyOrigin, err := systemOneKeyOrigin(apiKey != "")
	if err != nil {
		return nil, err
	}

	askers.keyOrigin = keyOrigin

	for _, n := range nodes {
		if n.Job != config.JobAsk {
			continue
		}

		built, err := buildAskingNode(n, apiKey, keyOrigin, injected)
		if err != nil {
			return nil, err
		}

		askers.nodes[n.Name] = built
	}

	return askers, nil
}

func buildAskingNode(n config.Node, apiKey string, keyOrigin origin, injected func(config.Node) inference.Asker) (askingNode, error) {
	parsed, err := url.Parse(n.BaseURL)
	if err != nil {
		return askingNode{}, fmt.Errorf("analysis.pipeline node %q.base_url: %w", n.Name, err)
	}

	built := askingNode{baseURL: n.BaseURL, keySent: apiKey != "" && originOf(parsed) == keyOrigin}

	if injected != nil {
		built.asker = injected(n)
		return built, nil
	}

	key := ""
	if built.keySent {
		key = apiKey
	}

	provider, err := inference.NewSystemOneProvider(n.BaseURL, key, n.Model, n.Timeout)
	if err != nil {
		return askingNode{}, fmt.Errorf("analysis.pipeline node %q: %w", n.Name, err)
	}

	built.asker = provider

	return built, nil
}

// systemOneKeyOrigin comes from the environment, never archguard.yaml, so a change to the config can't send a CI secret to another server.
func systemOneKeyOrigin(keySet bool) (origin, error) {
	raw := os.Getenv(config.SystemOneKeyOriginEnv)
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

func asKeyRejected(err error) (*analysis.KeyRejectedError, bool) {
	var rejected *analysis.KeyRejectedError
	ok := errors.As(err, &rejected)

	return rejected, ok
}
