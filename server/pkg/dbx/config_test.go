package dbx_test

import (
	"strings"
	"testing"

	"finga/pkg/configload"
	"finga/pkg/dbx"
)

// A database URL that isn't Postgres fails validation, so a binary pointed at another database stops at startup.
func TestTheURLMustBePostgres(t *testing.T) {
	type binary struct {
		Database dbx.Config `mapstructure:"database"`
	}
	for url, valid := range map[string]bool{"postgres://finga@localhost:5432/finga": true, "mysql://finga@localhost/finga": false, "not a url": false} {
		t.Run(url, func(t *testing.T) {
			yaml := "database:\n  url: " + url + "\n  max_conns: 1\n"
			_, err := configload.LoadFrom[binary](strings.NewReader(yaml), "config.yaml", configload.Options{EnvPrefix: "DBX_TEST", Strict: true})
			if (err == nil) != valid || !valid && !strings.Contains(err.Error(), "database.url") {
				t.Fatalf("url %q: %v", url, err)
			}
		})
	}
}
