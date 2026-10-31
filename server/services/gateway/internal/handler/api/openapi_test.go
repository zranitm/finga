package api_test

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/services/gateway/internal/handler/api"
)

// The REST contract in api/openapi.yaml must describe exactly the routes
// the handler serves, and the schemas must carry exactly the JSON fields of
// the DTOs, so the two can't drift apart.

type openAPI struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]any `yaml:"properties"`
			Required   []string       `yaml:"required"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSpec(t *testing.T) openAPI {
	t.Helper()
	data, err := os.ReadFile("../../../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec openAPI
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestRoutesMatchOpenAPI(t *testing.T) {
	spec := loadSpec(t)
	var inSpec []string
	for path, ops := range spec.Paths {
		for method := range ops {
			inSpec = append(inSpec, strings.ToUpper(method)+" "+path)
		}
	}
	var inCode []string
	for _, r := range api.Routes() {
		inCode = append(inCode, r.Method+" "+r.Path)
	}
	sort.Strings(inSpec)
	sort.Strings(inCode)
	if !reflect.DeepEqual(inSpec, inCode) {
		t.Fatalf("routes differ\nspec: %v\ncode: %v", inSpec, inCode)
	}
}

func TestDTOsMatchOpenAPISchemas(t *testing.T) {
	spec := loadSpec(t)
	tests := []struct {
		schema string
		dto    any
	}{
		{"RegisterRequest", api.RegisterRequest{}},
		{"Account", api.AccountResponse{}},
		{"LoginRequest", api.LoginRequest{}},
		{"Session", api.SessionResponse{}},
		{"CreateCharacterRequest", api.CreateCharacterRequest{}},
		{"Character", api.CharacterResponse{}},
		{"Stats", api.StatsResponse{}},
		{"Characters", api.CharactersResponse{}},
		{"Settings", api.SettingsResponse{}},
		{"Error", api.ErrorResponse{}},
	}
	for _, tt := range tests {
		t.Run(tt.schema, func(t *testing.T) {
			schema, ok := spec.Components.Schemas[tt.schema]
			if !ok {
				t.Fatalf("schema %s missing from openapi.yaml", tt.schema)
			}
			var inSpec []string
			for name := range schema.Properties {
				inSpec = append(inSpec, name)
			}
			inCode := jsonFields(reflect.TypeOf(tt.dto))
			sort.Strings(inSpec)
			sort.Strings(inCode)
			if !reflect.DeepEqual(inSpec, inCode) {
				t.Fatalf("fields differ\nspec: %v\ncode: %v", inSpec, inCode)
			}
		})
	}
}

func jsonFields(t reflect.Type) []string {
	var out []string
	for field := range t.Fields() {
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
