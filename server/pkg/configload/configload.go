// Package configload loads a typed config from YAML plus env overrides, renamed by envprefix tags, and validates it.
package configload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// Options controls a Load.
type Options struct {
	Path      string // the file Load reads; LoadFrom ignores it
	EnvPrefix string // "APP": server.addr is overridden by APP_SERVER_ADDR
	Strict    bool   // fail on a YAML key that no field names
}

// Validator is implemented by a schema with rules no validate tag can express.
type Validator interface{ Validate() error }

var errDurationUnit = errors.New("a duration needs a unit, such as 30s")

const (
	tagKey       = "mapstructure"
	tagEnvPrefix = "envprefix"
	tagSkip      = "-"

	envPathSep = "_"
	keySep     = "."
	listSep    = ","
	pairSep    = "="

	configPathEnv = "CONFIG_PATH"
	localFile     = "config.local.yaml"
	defaultFile   = "config.yaml"
)

// Load reads opts.Path and decodes it into T.
func Load[T any](opts Options) (*T, error) {
	f, err := os.Open(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	defer f.Close()
	return LoadFrom[T](f, opts.Path, opts)
}

// LoadFrom loads T from r; validation errors never echo values.
func LoadFrom[T any](r io.Reader, source string, opts Options) (*T, error) {
	cfg, err := loadFrom[T](r, opts)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", source, err)
	}
	return cfg, nil
}

func loadFrom[T any](r io.Reader, opts Options) (*T, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(r); err != nil {
		return nil, err
	}
	settings := v.AllSettings()

	var cfg T
	applyEnv(settings, reflect.TypeOf(cfg), opts.EnvPrefix)

	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &cfg,
		TagName:          tagKey,
		ErrorUnused:      opts.Strict,
		WeaklyTypedInput: true, // environment values are strings
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			numberToDurationHook,
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(listSep),
			stringToMapHook,
		),
	})
	if err != nil {
		return nil, err
	}
	if err := dec.Decode(settings); err != nil {
		return nil, err
	}
	if err := validateTags(&cfg); err != nil {
		return nil, err
	}
	if val, ok := any(&cfg).(Validator); ok {
		if err := val.Validate(); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func applyEnv(m map[string]any, t reflect.Type, envBase string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for f := range t.Fields() {
		key := fieldKey(f)
		if key == "" {
			continue
		}
		envName := envBase + envPathSep + strings.ToUpper(key)
		if own, ok := f.Tag.Lookup(tagEnvPrefix); ok {
			envName = own
		}
		if isSection(f.Type) {
			sub, ok := m[key].(map[string]any)
			if !ok {
				sub = map[string]any{}
				m[key] = sub
			}
			applyEnv(sub, f.Type, envName)
			continue
		}
		if val, ok := os.LookupEnv(envName); ok {
			m[key] = val
		}
	}
}

func fieldKey(f reflect.StructField) string {
	if !f.IsExported() {
		return ""
	}
	name, _, _ := strings.Cut(f.Tag.Get(tagKey), listSep)
	switch name {
	case tagSkip:
		return ""
	case "":
		return strings.ToLower(f.Name)
	}
	return name
}

func isSection(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t != reflect.TypeFor[time.Time]()
}

// numberToDurationHook refuses a bare number for a duration, which would otherwise be nanoseconds.
func numberToDurationHook(from, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[time.Duration]() {
		return data, nil
	}
	switch from.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return nil, errDurationUnit
	}
	return data, nil
}

func stringToMapHook(from, to reflect.Type, data any) (any, error) {
	if from.Kind() != reflect.String || to.Kind() != reflect.Map {
		return data, nil
	}
	s := strings.TrimSpace(data.(string))
	switch {
	case s == "":
		return map[string]any{}, nil
	case strings.HasPrefix(s, "{"):
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return nil, fmt.Errorf("map from JSON: %w", err)
		}
		return m, nil
	}
	m := map[string]any{}
	for pair := range strings.SplitSeq(s, listSep) {
		k, val, ok := strings.Cut(pair, pairSep)
		if !ok {
			return nil, fmt.Errorf("map entry %q: want key%svalue", k, pairSep)
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return m, nil
}

// validateTags reports failing fields by YAML key, never by Go name or value.
func validateTags(cfg any) error {
	val := validator.New(validator.WithRequiredStructEnabled())
	val.RegisterTagNameFunc(fieldKey)
	if err := val.RegisterValidation("host_port", hostPort); err != nil {
		return err
	}
	err := val.Struct(cfg)
	if err == nil {
		return nil
	}
	var fieldErrs validator.ValidationErrors
	if !errors.As(err, &fieldErrs) {
		return err
	}
	errs := make([]error, 0, len(fieldErrs))
	for _, fe := range fieldErrs {
		errs = append(errs, fmt.Errorf("%s: fails %q", yamlKey(fe.Namespace()), fe.ActualTag()+paramSuffix(fe.Param())))
	}
	return errors.Join(errs...)
}

// hostName is a DNS name whose labels may hold underscores, as container and service names do.
var hostName = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?(\.[A-Za-z0-9_]([A-Za-z0-9_-]*[A-Za-z0-9_])?)*\.?$`)

// hostPort is the host_port tag: host:port with no scheme, the host an IP literal (IPv6 in brackets) or a name, the port 1 to 65535.
func hostPort(fl validator.FieldLevel) bool {
	host, port, err := net.SplitHostPort(fl.Field().String())
	if err != nil || host == "" {
		return false
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return hostName.MatchString(host)
}

// yamlKey turns schema.peers[sales].base_url into peers.sales.base_url.
func yamlKey(namespace string) string {
	_, path, _ := strings.Cut(namespace, keySep) // drop the root struct's name
	r := strings.NewReplacer("[", keySep, "]", "")
	return r.Replace(path)
}

func paramSuffix(p string) string {
	if p == "" {
		return ""
	}
	return pairSep + p
}

// ResolvePath picks $CONFIG_PATH, else dir/config.local.yaml, else dir/config.yaml.
func ResolvePath(dir string) string {
	if p := os.Getenv(configPathEnv); p != "" {
		return p
	}
	local := filepath.Join(dir, localFile)
	if _, err := os.Stat(local); err == nil {
		return local
	}
	return filepath.Join(dir, defaultFile)
}
