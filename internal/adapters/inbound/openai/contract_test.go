package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/harness-gateway/harness-gateway/api"
	"github.com/harness-gateway/harness-gateway/internal/application"
	"github.com/harness-gateway/harness-gateway/internal/domain"
)

// Convert OAS 3.0 nullable into its specified JSON Schema type expansion. In
// particular, keep enum constraints: nullable must not short-circuit validation.
func strictContract(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	var convert func(any) any
	convert = func(value any) any {
		switch value := value.(type) {
		case map[string]any:
			result := map[string]any{}
			for key, field := range value {
				if key != "nullable" {
					result[key] = convert(field)
				}
			}
			if value["nullable"] == true {
				if kind, ok := value["type"].(string); ok {
					result["type"] = []any{kind, "null"}
				}
			}
			if ref, ok := value["$ref"].(string); ok {
				result["$ref"] = strings.Replace(ref, "#/components/schemas/", "#/$defs/", 1)
			}
			return result
		case []any:
			result := make([]any, len(value))
			for i, field := range value {
				result[i] = convert(field)
			}
			return result
		default:
			return value
		}
	}
	components := document["components"].(map[string]any)["schemas"]
	data, err := json.Marshal(map[string]any{"$ref": "#/$defs/" + name, "$defs": convert(components)})
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestStreamingResponsesSatisfyStrictPublishedContract(t *testing.T) {
	schema := strictContract(t, "Chunk")
	backend := harnessFunc(func(_ context.Context, _ domain.Request, emit func(string) error) (domain.Result, error) {
		if err := emit("hello"); err != nil {
			return domain.Result{}, err
		}
		return domain.Result{Usage: &domain.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}}, nil
	})
	w := httptest.NewRecorder()
	body := `{"model":"codex","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`
	handler(backend, time.Second).ServeHTTP(w, localRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	chunks, nulls, terminals := 0, 0, 0
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(chunk); err != nil {
			t.Fatalf("wire chunk violates published schema: %v\n%s", err, data)
		}
		chunks++
		for _, choice := range chunk["choices"].([]any) {
			choice := choice.(map[string]any)
			if choice["finish_reason"] == nil {
				nulls++
			} else {
				terminals++
			}
			choice["finish_reason"] = "unsupported-finish"
			if schema.Validate(chunk) == nil {
				t.Fatal("strict validator ignored finish_reason enum")
			}
		}
	}
	if chunks != 4 || nulls != 2 || terminals != 1 {
		t.Fatalf("missing actual stream phases: chunks=%d nulls=%d terminals=%d", chunks, nulls, terminals)
	}
}

func TestOptionalNullParametersMatchPublishedContract(t *testing.T) {
	schema := strictContract(t, "ChatRequest")
	for _, test := range []struct {
		field string
		valid bool
	}{
		{"", true}, {`,"reasoning_effort":null`, true}, {`,"reasoning_effort":"low"`, true},
		{`,"stream_options":null`, true}, {`,"stream":true,"stream_options":{"include_usage":null}`, true},
		{`,"reasoning_effort":"unsupported"`, false}, {`,"stream":true,"stream_options":{"include_usage":"yes"}`, false},
	} {
		t.Run(test.field, func(t *testing.T) {
			body := `{"model":"codex","messages":[{"role":"user","content":"hi"}]` + test.field + `}`
			var request any
			if err := json.Unmarshal([]byte(body), &request); err != nil {
				t.Fatal(err)
			}
			if accepted := schema.Validate(request) == nil; accepted != test.valid {
				t.Fatalf("schema accepted=%t expected=%t: %s", accepted, test.valid, body)
			}
			backend := harnessFunc(func(_ context.Context, _ domain.Request, emit func(string) error) (domain.Result, error) {
				return domain.Result{}, emit("ok")
			})
			w := httptest.NewRecorder()
			handler(backend, time.Second).ServeHTTP(w, localRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
			if accepted := w.Code == http.StatusOK; accepted != test.valid {
				t.Fatalf("handler HTTP %d differs from contract: %s", w.Code, w.Body)
			}
		})
	}
}

func TestKnownFieldCaseAliasesAreAcceptedCompatibilityLimit(t *testing.T) {
	for _, test := range []struct{ body, model string }{
		{`{"MODEL":"codex","MESSAGES":[{"ROLE":"user","CONTENT":[{"TYPE":"text","TEXT":"hello"}]}]}`, "codex"},
		{`{"model":"codex","MODEL":"copilot","messages":[{"role":"user","content":"hello"}]}`, "copilot"},
		{`{"model":"codex","messages":[{"role":"user","content":"hello"}],"STREAM":true,"STREAM_OPTIONS":{"INCLUDE_USAGE":null}}`, "codex"},
	} {
		t.Run(test.body, func(t *testing.T) {
			selected := ""
			backends := []application.Backend{}
			for _, name := range []string{"codex", "copilot"} {
				backends = append(backends, application.Backend{Model: application.Model{ID: name, Provider: name}, Harness: harnessFunc(func(_ context.Context, request domain.Request, emit func(string) error) (domain.Result, error) {
					selected = name
					if len(request.Messages) != 1 || request.Messages[0].Text != "hello" {
						t.Errorf("case alias lost message: %+v", request)
					}
					return domain.Result{}, emit("ok")
				})})
			}
			var value any
			if err := json.Unmarshal([]byte(test.body), &value); err != nil {
				t.Fatal(err)
			}
			if strictContract(t, "ChatRequest").Validate(value) == nil {
				t.Fatal("canonical schema unexpectedly admits aliases")
			}
			w := httptest.NewRecorder()
			New(application.New(backends, 1), time.Second, nil).ServeHTTP(w, localRequest("POST", "/v1/chat/completions", strings.NewReader(test.body)))
			if w.Code != http.StatusOK || selected != test.model {
				t.Fatalf("accepted compatibility changed: status=%d selected=%q expected=%q body=%s", w.Code, selected, test.model, w.Body)
			}
		})
	}
}
