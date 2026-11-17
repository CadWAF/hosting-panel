package sites

import (
	"fmt"
	"regexp"
)

// Allowed php.ini overrides and the value shape each accepts. Anything else is rejected,
// so a value can never break out of the pool file line.
var allowedSettings = map[string]*regexp.Regexp{
	"memory_limit":           regexp.MustCompile(`^(-1|[0-9]{1,5}[KMG])$`),
	"upload_max_filesize":    regexp.MustCompile(`^[0-9]{1,5}[KMG]$`),
	"post_max_size":          regexp.MustCompile(`^[0-9]{1,5}[KMG]$`),
	"max_execution_time":     regexp.MustCompile(`^[0-9]{1,5}$`),
	"max_input_time":         regexp.MustCompile(`^(-1|[0-9]{1,5})$`),
	"max_input_vars":         regexp.MustCompile(`^[0-9]{1,6}$`),
	"display_errors":         regexp.MustCompile(`^(On|Off)$`),
	"error_reporting":        regexp.MustCompile(`^[A-Z_&|~^ ()0-9]{1,120}$`),
	"date.timezone":          regexp.MustCompile(`^[A-Za-z_]+(/[A-Za-z_+\-0-9]+){0,2}$`),
	"opcache.enable":         regexp.MustCompile(`^(0|1)$`),
	"session.gc_maxlifetime": regexp.MustCompile(`^[0-9]{1,7}$`),
	"short_open_tag":         regexp.MustCompile(`^(On|Off)$`),
	"allow_url_fopen":        regexp.MustCompile(`^(On|Off)$`),
	"disable_functions":      regexp.MustCompile(`^[a-z0-9_,]{0,500}$`),
	"max_file_uploads":       regexp.MustCompile(`^[0-9]{1,4}$`),
	"default_charset":        regexp.MustCompile(`^[A-Za-z0-9\-]{1,20}$`),
}

// AllowedSettingKeys lists the overridable php.ini keys (for the UI).
func AllowedSettingKeys() []string {
	out := make([]string, 0, len(allowedSettings))
	for k := range allowedSettings {
		out = append(out, k)
	}
	return out
}

func validateSettings(m map[string]string) error {
	for k, v := range m {
		re, ok := allowedSettings[k]
		if !ok {
			return fmt.Errorf("%w: setting %q is not allowed", ErrInvalid, k)
		}
		if !re.MatchString(v) {
			return fmt.Errorf("%w: bad value for %s", ErrInvalid, k)
		}
	}
	return nil
}

type presetSpec struct {
	maxChildren int
	terminate   string
	memory      string
}

var presets = map[string]presetSpec{
	"small":  {4, "60s", "128M"},
	"medium": {10, "120s", "256M"},
	"large":  {24, "300s", "512M"},
}
