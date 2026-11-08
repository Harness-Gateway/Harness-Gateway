package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/harness-gateway/harness-gateway/api"
)

func TestPinnedUpstreamProjection(t *testing.T) {
	file, err := os.Open("testdata/openai-85a23866.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	upstream, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := project(upstream) // Verifies the original upstream checksum.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(projected, api.OpenAPI) {
		t.Fatal("pinned upstream projection differs from api/openapi.json; reproduce it with tools/project-openapi, then go generate ./api")
	}
	upstream[len(upstream)/2] ^= 1
	if _, err := project(upstream); err == nil {
		t.Fatal("accepted a modified upstream fixture")
	}
}

func TestPublishedGatewayContractIsCurrent(t *testing.T) {
	var document struct {
		Components struct{ Schemas node }
	}
	if err := json.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	updated, err := json.MarshalIndent(gatewayDocument(document.Components.Schemas), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(updated, '\n'), api.OpenAPI) {
		t.Fatal("gateway documentation is stale; reproduce the OpenAPI projection and run go generate ./api")
	}
}

func TestChecksumRequired(t *testing.T) {
	if _, err := project([]byte(`{"components":{"schemas":{}}}`)); err == nil {
		t.Fatal("accepted an unpinned schema")
	}
}

func TestObfuscationSchemaAllowsOnlyFalseOrNull(t *testing.T) {
	var document struct{ Components struct{ Schemas node } }
	if err := json.Unmarshal(api.OpenAPI, &document); err != nil {
		t.Fatal(err)
	}
	schema := properties(document.Components.Schemas["StreamOptions"].(node))["include_obfuscation"].(node)
	if schema["type"] != "boolean" || schema["nullable"] != true || !reflect.DeepEqual(schema["enum"], []any{false, nil}) {
		t.Fatalf("schema must admit false and null, and reject true: %#v", schema)
	}
}

func TestNullableNormalization(t *testing.T) {
	input := node{"anyOf": []any{node{"type": "string", "enum": []any{"stop", nil}}, node{"type": "null"}}, "description": "upstream", "examples": []any{"stop"}}
	want := node{"type": "string", "enum": []any{"stop"}, "nullable": true, "x-go-type": "string"}
	if actual := normalize(input); !reflect.DeepEqual(actual, want) {
		t.Fatalf("got %#v, want %#v", actual, want)
	}
	if actual := normalize(node{"type": []any{"integer", "null"}, "format": "unixtime"}); !reflect.DeepEqual(actual, node{"type": "integer", "nullable": true, "format": "int64"}) {
		t.Fatal(actual)
	}
}

func TestProjectionDoesNotMutateUpstream(t *testing.T) {
	original := node{"type": "object", "properties": node{"first": node{"type": "string"}, "second": node{"type": "boolean"}}, "required": []any{"first", "second"}}
	p := projector{upstream: map[string]node{"Base": original}}
	combined := node{"allOf": []any{ref("Base"), node{"type": "object", "properties": node{"third": node{"type": "integer"}}}}}
	selected := p.selectFields(combined, "first", "third")
	if !reflect.DeepEqual(selected["required"], []any{"first"}) || len(properties(selected)) != 2 {
		t.Fatal(selected)
	}
	properties(selected)["first"].(node)["type"] = "integer"
	if properties(original)["first"].(node)["type"] != "string" {
		t.Fatal("projection mutated upstream")
	}
}
