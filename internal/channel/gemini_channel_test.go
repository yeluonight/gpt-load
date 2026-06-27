package channel

import (
	"net/http"
	"net/url"
	"testing"
)

func TestGeminiBuildUpstreamURLMapsOpenAIModelListToNative(t *testing.T) {
	ch := newTestGeminiChannel(t, "https://generativelanguage.googleapis.com")
	req := mustNewRequest(t, http.MethodGet, "http://localhost:3001/proxy/gemini/v1/models?pageSize=10")

	got, err := ch.BuildUpstreamURL(req, "gemini")
	if err != nil {
		t.Fatalf("BuildUpstreamURL returned error: %v", err)
	}

	want := "https://generativelanguage.googleapis.com/v1beta/models?pageSize=10"
	if got != want {
		t.Fatalf("BuildUpstreamURL = %q, want %q", got, want)
	}
}

func TestGeminiBuildUpstreamURLPreservesNativeModelList(t *testing.T) {
	ch := newTestGeminiChannel(t, "https://generativelanguage.googleapis.com")
	req := mustNewRequest(t, http.MethodGet, "http://localhost:3001/proxy/gemini/v1beta/models")

	got, err := ch.BuildUpstreamURL(req, "gemini")
	if err != nil {
		t.Fatalf("BuildUpstreamURL returned error: %v", err)
	}

	want := "https://generativelanguage.googleapis.com/v1beta/models"
	if got != want {
		t.Fatalf("BuildUpstreamURL = %q, want %q", got, want)
	}
}

func TestGeminiBuildUpstreamURLPreservesOpenAICompatibilityModelList(t *testing.T) {
	ch := newTestGeminiChannel(t, "https://generativelanguage.googleapis.com")
	req := mustNewRequest(t, http.MethodGet, "http://localhost:3001/proxy/gemini/v1beta/openai/v1/models")

	got, err := ch.BuildUpstreamURL(req, "gemini")
	if err != nil {
		t.Fatalf("BuildUpstreamURL returned error: %v", err)
	}

	want := "https://generativelanguage.googleapis.com/v1beta/openai/v1/models"
	if got != want {
		t.Fatalf("BuildUpstreamURL = %q, want %q", got, want)
	}
}

func TestGeminiBuildUpstreamURLAvoidsDuplicateNativeVersionPath(t *testing.T) {
	ch := newTestGeminiChannel(t, "https://generativelanguage.googleapis.com/v1beta")
	req := mustNewRequest(t, http.MethodGet, "http://localhost:3001/proxy/gemini/v1/models")

	got, err := ch.BuildUpstreamURL(req, "gemini")
	if err != nil {
		t.Fatalf("BuildUpstreamURL returned error: %v", err)
	}

	want := "https://generativelanguage.googleapis.com/v1beta/models"
	if got != want {
		t.Fatalf("BuildUpstreamURL = %q, want %q", got, want)
	}
}

func TestGeminiBuildUpstreamURLDoesNotMapPostModelsPath(t *testing.T) {
	ch := newTestGeminiChannel(t, "https://generativelanguage.googleapis.com")
	req := mustNewRequest(t, http.MethodPost, "http://localhost:3001/proxy/gemini/v1/models")

	got, err := ch.BuildUpstreamURL(req, "gemini")
	if err != nil {
		t.Fatalf("BuildUpstreamURL returned error: %v", err)
	}

	want := "https://generativelanguage.googleapis.com/v1/models"
	if got != want {
		t.Fatalf("BuildUpstreamURL = %q, want %q", got, want)
	}
}

func newTestGeminiChannel(t *testing.T, upstream string) *GeminiChannel {
	t.Helper()

	return &GeminiChannel{
		BaseChannel: &BaseChannel{
			Name: "gemini",
			Upstreams: []UpstreamInfo{
				{
					URL:    mustParseURL(t, upstream),
					Weight: 1,
				},
			},
		},
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	return parsed
}

func mustNewRequest(t *testing.T, method string, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("new request %q: %v", rawURL, err)
	}
	return req
}
