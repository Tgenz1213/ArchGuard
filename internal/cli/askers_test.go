package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgenz1213/archguard/internal/analysis"
	"github.com/tgenz1213/archguard/internal/config"
	"github.com/tgenz1213/archguard/internal/inference"
	"github.com/tgenz1213/archguard/internal/telemetry"
)

func askNodeAt(name, baseURL string) config.Node {
	return config.Node{Name: name, Job: config.JobAsk, With: config.RunnerSystemOne, BaseURL: baseURL, Model: "jev-latest", Timeout: time.Minute}
}

func setKey(t *testing.T, key string) {
	t.Helper()
	t.Setenv(config.SystemOneAPIKeyEnv, key)
}

func TestNewAskers_NoAskNodeReadsNoKeySetting(t *testing.T) {
	setKey(t, "k")

	askers, err := newAskers([]config.Node{{Name: "shortlist", Job: config.JobRank, With: config.RunnerCosine}}, "not a url")
	if err != nil {
		t.Fatalf("newAskers: %v", err)
	}

	if askers.asks() {
		t.Error("asks() = true, want false with no ask node")
	}
}

func TestNewAskers_SendsTheKeyOnlyToItsOrigin(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		baseURL  string
		wantSent bool
	}{
		{"hosted Jev by default", "", "https://api.typesafe.ai", true},
		{"hosted Jev with the default port written out", "", "https://api.typesafe.ai:443/prefix", true},
		{"another host", "", "https://example.com", false},
		{"the named origin", "http://127.0.0.1:8009", "http://127.0.0.1:8009", true},
		{"the named origin with a trailing slash", "http://127.0.0.1:8009/", "http://127.0.0.1:8009/kev", true},
		{"another port", "http://127.0.0.1:8009", "http://127.0.0.1:8008", false},
		{"another scheme", "https://localhost:8009", "http://localhost:8009", false},
		{"host case doesn't matter", "https://API.typesafe.ai", "https://api.TypeSafe.ai", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setKey(t, "secret")

			askers, err := newAskers([]config.Node{askNodeAt("screen", tt.baseURL)}, tt.origin)
			if err != nil {
				t.Fatalf("newAskers: %v", err)
			}

			if got := askers.nodes["screen"].keySent; got != tt.wantSent {
				t.Errorf("keySent = %v, want %v", got, tt.wantSent)
			}
		})
	}
}

func TestNewAskers_ANodeOnTheKeysOriginNeedsTheKey(t *testing.T) {
	setKey(t, "")

	_, err := newAskers([]config.Node{askNodeAt("screen", config.DefaultSystemOneBaseURL)}, "")
	if err == nil || !strings.Contains(err.Error(), `node "screen"`) || !strings.Contains(err.Error(), config.SystemOneAPIKeyEnv) {
		t.Fatalf("err = %v, want one naming the node and %s", err, config.SystemOneAPIKeyEnv)
	}
}

func TestNewAskers_ANodeOffTheKeysOriginRunsWithoutAKey(t *testing.T) {
	setKey(t, "")

	askers, err := newAskers([]config.Node{askNodeAt("screen", "http://127.0.0.1:8009")}, "")
	if err != nil {
		t.Fatalf("newAskers: %v", err)
	}

	if askers.nodes["screen"].keySent {
		t.Error("keySent = true with no key set")
	}
}

func TestNewAskers_RealClientCarriesTheKeyDecision(t *testing.T) {
	var auth []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))

		if _, err := w.Write([]byte(`{"answers":{"a":{"type":"noul","noul":0.5}}}`)); err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	other := httptest.NewServer(server.Config.Handler)
	t.Cleanup(other.Close)

	setKey(t, "secret")

	askers, err := newAskers([]config.Node{askNodeAt("keyed", server.URL), askNodeAt("open", other.URL)}, server.URL)
	if err != nil {
		t.Fatalf("newAskers: %v", err)
	}

	for _, name := range []string{"keyed", "open"} {
		if _, err := askers.ForNode(config.Node{Name: name}).Ask(t.Context(), "code", map[string]string{"a": "q"}); err != nil {
			t.Fatalf("Ask on %s: %v", name, err)
		}
	}

	if len(auth) != 2 || auth[0] != "Bearer secret" || auth[1] != "" {
		t.Errorf("Authorization headers = %q, want the key on the keyed node only", auth)
	}
}

func TestNewAskers_RejectsAKeyOverPlainHTTPToAnotherMachine(t *testing.T) {
	setKey(t, "secret")

	_, err := newAskers([]config.Node{askNodeAt("screen", "http://10.0.0.5:8009")}, "http://10.0.0.5:8009")
	if err == nil || !strings.Contains(err.Error(), "plain http") || !strings.Contains(err.Error(), "10.0.0.5:8009") {
		t.Fatalf("err = %v, want one naming the plain-http origin", err)
	}
}

func TestNewAskers_SendsTheKeyOverPlainHTTPOnThisMachine(t *testing.T) {
	for _, keyOrigin := range []string{"http://localhost:8009", "http://127.0.0.2:8009", "http://[::1]:8009"} {
		t.Run(keyOrigin, func(t *testing.T) {
			setKey(t, "secret")

			askers, err := newAskers([]config.Node{askNodeAt("screen", keyOrigin)}, keyOrigin)
			if err != nil {
				t.Fatalf("newAskers: %v", err)
			}

			if !askers.nodes["screen"].keySent {
				t.Error("keySent = false, want the key sent to its own loopback origin")
			}
		})
	}
}

func TestNewAskers_APlainHTTPOriginElsewhereIsFineWithoutAKey(t *testing.T) {
	setKey(t, "")

	if _, err := newAskers([]config.Node{askNodeAt("screen", "http://127.0.0.1:9")}, "http://10.0.0.5:8009"); err != nil {
		t.Errorf("newAskers: %v", err)
	}
}

func TestNewAskers_RejectsAKeyOriginThatIsNotAnOrigin(t *testing.T) {
	for _, origin := range []string{"ftp://x", "http://x/path", "https://u@x", "https://x?q", "https://x#f", "x:8009", "://"} {
		t.Run(origin, func(t *testing.T) {
			setKey(t, "")

			_, err := newAskers([]config.Node{askNodeAt("screen", "http://127.0.0.1:9")}, origin)
			if err == nil || !strings.Contains(err.Error(), config.SystemOneKeyOriginEnv) {
				t.Errorf("err = %v, want one naming %s", err, config.SystemOneKeyOriginEnv)
			}
		})
	}
}

type replyAsker inference.AskReply

func (r replyAsker) Ask(context.Context, string, map[string]string) (inference.AskReply, error) {
	return inference.AskReply(r), nil
}

func TestSystemOneAskers_MeteredRecordsAsking(t *testing.T) {
	usage := inference.Usage{InputTokens: 12, OutputTokens: 1, Reported: true}
	askers := &systemOneAskers{nodes: map[string]askingNode{"screen": {asker: replyAsker{Usage: usage}}}}
	tally := &telemetry.Tally{}

	if _, err := askers.metered(tally).ForNode(config.Node{Name: "screen"}).Ask(t.Context(), "code", nil); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if got := tally.Totals(telemetry.Asking); got.Calls != 1 || got.Usage != usage {
		t.Errorf("asking totals = %+v, want one call with %+v", got, usage)
	}
}

func TestSystemOneAskers_RejectedKeyError(t *testing.T) {
	askers := &systemOneAskers{
		keyOrigin: origin{scheme: "https", host: "api.typesafe.ai", port: "443"},
		nodes: map[string]askingNode{
			"keyed": {baseURL: "https://api.typesafe.ai", keySent: true},
			"open":  {baseURL: "http://127.0.0.1:8009"},
		},
	}
	cause := &analysis.KeyRejectedError{Err: fmt.Errorf("asking: %w", errors.New("system one api error: 401 Unauthorized: quota"))}

	tests := []struct {
		node, want string
	}{
		{"keyed", config.SystemOneAPIKeyEnv + " was rejected by https://api.typesafe.ai; set a valid key and run the check again: the keyed node's server rejected its key"},
		{"open", "http://127.0.0.1:8009 needs a key, and " + config.SystemOneAPIKeyEnv + " is sent only to https://api.typesafe.ai:443; set " + config.SystemOneKeyOriginEnv + " to that server's origin, with the key set: the open node's server"},
		{"unknown", "the unknown node's server rejected its key: system one api error: 401 Unauthorized: quota"},
	}

	for _, tt := range tests {
		t.Run(tt.node, func(t *testing.T) {
			rejected := *cause
			rejected.Node = tt.node

			err := askers.rejectedKeyError(&rejected)
			if got := err.Error(); !strings.Contains(got, tt.want) || !strings.HasSuffix(got, "401 Unauthorized: quota") {
				t.Errorf("message = %q, want it to contain %q and end with the server's answer", got, tt.want)
			}

			var wrapped *analysis.KeyRejectedError
			if !errors.As(err, &wrapped) {
				t.Error("the message no longer wraps the KeyRejectedError")
			}
		})
	}
}
