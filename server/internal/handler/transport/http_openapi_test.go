package transport_test

import (
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	haccount "finga/internal/handler/account"
	hcharacter "finga/internal/handler/character"
	hitem "finga/internal/handler/item"
	hpvp "finga/internal/handler/pvp"
	"finga/internal/handler/transport"
	"finga/pkg/panics"
)

// api/openapi.yaml must match the handler's routes and the DTOs' JSON fields exactly.

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
	data, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec openAPI
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

// ginParam is a path parameter as Gin writes it, :id, which OpenAPI writes {id}.
var ginParam = regexp.MustCompile(`:(\w+)`)

func TestRoutesMatchOpenAPI(t *testing.T) {
	spec := loadSpec(t)
	var inSpec []string
	for path, ops := range spec.Paths {
		for method := range ops {
			inSpec = append(inSpec, strings.ToUpper(method)+" "+path)
		}
	}
	e := transport.NewEngine()
	log := slog.New(slog.DiscardHandler)
	mountREST(transport.RESTOptions{Logger: log, Panics: panics.NewRecorder(log, nil)})(e)
	var inCode []string
	for _, r := range e.Routes() {
		if strings.HasPrefix(r.Path, "/api/") {
			inCode = append(inCode, r.Method+" "+ginParam.ReplaceAllString(r.Path, "{$1}"))
		}
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
		{"RegisterRequest", haccount.RegisterRequest{}},
		{"Account", haccount.AccountResponse{}},
		{"LoginRequest", haccount.LoginRequest{}},
		{"Session", haccount.SessionResponse{}},
		{"CreateCharacterRequest", hcharacter.CreateCharacterRequest{}},
		{"Character", hcharacter.CharacterResponse{}},
		{"Stats", hcharacter.StatsResponse{}},
		{"Characters", hcharacter.CharactersResponse{}},
		{"Settings", haccount.SettingsResponse{}},
		{"Wardrobe", hitem.WardrobeResponse{}},
		{"Piece", hitem.PieceResponse{}},
		{"SkillBar", hcharacter.SkillBarRequest{}},
		{"SkillBar", hcharacter.SkillBarResponse{}},
		{"ArenaBuild", hpvp.ArenaBuildRequest{}},
		{"ArenaBuild", hpvp.ArenaBuildResponse{}},
		{"PvpStanding", hpvp.PvpStandingResponse{}},
		{"PvpProfile", hpvp.PvpProfileResponse{}},
		{"Leaderboard", hpvp.LeaderboardResponse{}},
		{"LeaderboardRow", hpvp.LeaderboardRowResponse{}},
		{"Error", transport.ErrorResponse{}},
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
