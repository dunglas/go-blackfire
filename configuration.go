package blackfire

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/go-ini/ini"
)

type BlackfireConfiguration struct {
	// Time before dropping an unresponsive agent connection (default 250ms)
	AgentTimeout time.Duration
	// The socket to use when connecting to the Blackfire agent (default depends on OS)
	AgentSocket string
	// The Blackfire query string to be sent with any profiles. This is either
	// provided by the `blackfire run` command in an ENV variable, or acquired
	// via a signing request to Blackfire. You won't need to set this manually.
	BlackfireQuery string
	// Client ID to authenticate with the Blackfire API
	ClientId string
	// Client token to authenticate with the Blackfire API
	ClientToken string
	// The Blackfire API endpoint the profile data will be sent to (default https://blackfire.io)
	HTTPEndpoint *url.URL
	// Path to the log file (default go-probe.log)
	LogFile string
	// Log verbosity 4: debug, 3: info, 2: warning, 1: error (default 1)
	LogLevel int
	// The maximum duration of a profile. A profile operation can never exceed
	// this duration (default 10 minutes).
	// This guards against runaway profile operations.
	MaxProfileDuration time.Duration
}

func (this *BlackfireConfiguration) setEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	this.HTTPEndpoint = u
	return nil
}

func (this *BlackfireConfiguration) getDefaultIniPath() string {
	getIniPath := func(dir string) string {
		if dir == "" {
			return ""
		}
		fileName := ".blackfire.ini"
		filePath := path.Join(path.Clean(dir), fileName)
		_, err := os.Stat(filePath)
		Log.Debug().Msgf("Blackfire: Does configuration file exist at %v: %v", filePath, err == nil)
		if err != nil {
			return ""
		}
		return filePath
	}

	if iniPath := getIniPath(os.Getenv("BLACKFIRE_HOME")); iniPath != "" {
		return iniPath
	}

	if runtime.GOOS == "linux" {
		if iniPath := getIniPath(os.Getenv("XDG_CONFIG_HOME")); iniPath != "" {
			return iniPath
		}
	}

	if iniPath := getIniPath(os.Getenv("HOME")); iniPath != "" {
		return iniPath
	}

	if runtime.GOOS == "windows" {
		homedrive := os.Getenv("HOMEDRIVE")
		homepath := os.Getenv("HOMEPATH")
		if homedrive != "" && homepath != "" {
			dir := path.Join(path.Dir(homedrive), homepath)
			if iniPath := getIniPath(dir); iniPath != "" {
				return iniPath
			}
		}
	}

	return ""
}

func (this *BlackfireConfiguration) configureFromDefaults() {
	switch runtime.GOOS {
	case "windows":
		this.AgentSocket = "tcp://127.0.0.1:8307"
	case "darwin":
		this.AgentSocket = "unix:///usr/local/var/run/blackfire-agent.sock"
	case "linux":
		this.AgentSocket = "unix:///var/run/blackfire/agent.sock"
	case "freebsd":
		this.AgentSocket = "unix:///var/run/blackfire/agent.sock"
	}

	this.setEndpoint("https://blackfire.io")
	this.LogFile = "go-probe.log"
	this.LogLevel = 3
	this.AgentTimeout = time.Millisecond * 250
	this.MaxProfileDuration = time.Minute * 10

	setLogFile(this.LogFile)
	setLogLevel(this.LogLevel)
}

func readEnvVar(name string) string {
	if v := os.Getenv(name); v != "" {
		Log.Debug().Msgf("Blackfire: Read ENV var %v: %v", name, v)
		return v
	}
	return ""
}

func (this *BlackfireConfiguration) readLoggingFromEnv() {
	if v := readEnvVar("BLACKFIRE_LOG_LEVEL"); v != "" {
		level, err := strconv.Atoi(v)
		if err != nil {
			Log.Error().Msgf("Blackfire: Unable to set from env var BLACKFIRE_LOG_LEVEL %v: %v", v, err)
		} else {
			this.LogLevel = level
		}
	}

	if v := readEnvVar("BLACKFIRE_LOG_FILE"); v != "" {
		this.LogFile = v
	}
}

func (this *BlackfireConfiguration) configureLoggingFromEnv() {
	this.readLoggingFromEnv()

	setLogFile(this.LogFile)
	setLogLevel(this.LogLevel)
}

func (this *BlackfireConfiguration) configureFromEnv() {
	this.readLoggingFromEnv()

	if v := readEnvVar("BLACKFIRE_AGENT_SOCKET"); v != "" {
		this.AgentSocket = v
	}

	if v := readEnvVar("BLACKFIRE_QUERY"); v != "" {
		this.BlackfireQuery = v
	}

	if v := readEnvVar("BLACKFIRE_CLIENT_ID"); v != "" {
		this.ClientId = v
	}

	if v := readEnvVar("BLACKFIRE_CLIENT_TOKEN"); v != "" {
		this.ClientToken = v
	}

	if v := readEnvVar("BLACKFIRE_ENDPOINT"); v != "" {
		if err := this.setEndpoint(v); err != nil {
			Log.Error().Msgf("Blackfire: Unable to set from env var BLACKFIRE_ENDPOINT %v: %v", v, err)
		}
	}

	setLogFile(this.LogFile)
	setLogLevel(this.LogLevel)
}

func (this *BlackfireConfiguration) parseSeconds(value string) (time.Duration, error) {
	re := regexp.MustCompile(`([0-9.]+)`)
	found := re.FindStringSubmatch(value)

	if len(found) == 0 {
		return 0, fmt.Errorf("%v: No seconds value found", value)
	}

	seconds, err := strconv.ParseFloat(found[1], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(float64(time.Second) * seconds), nil
}

func getStringFromIniSection(section *ini.Section, key string) string {
	if v := section.Key(key).String(); v != "" {
		Log.Debug().Msgf("Blackfire: Read INI key %v: %v", key, v)
		return v
	}
	return ""
}

func (this *BlackfireConfiguration) configureFromIniFile(path string) {
	if path == "" {
		if path = this.getDefaultIniPath(); path == "" {
			return
		}
	}

	iniConfig, err := ini.Load(path)
	if err != nil {
		Log.Error().Msgf("Blackfire: Could not load Blackfire config file %v: %v", path, err)
		return
	}

	section := iniConfig.Section("blackfire")

	if section.HasKey("client-id") {
		this.ClientId = getStringFromIniSection(section, "client-id")
	}

	if section.HasKey("client-token") {
		this.ClientToken = getStringFromIniSection(section, "client-token")
	}

	if section.HasKey("endpoint") {
		endpoint := getStringFromIniSection(section, "endpoint")
		if err := this.setEndpoint(endpoint); err != nil {
			Log.Error().Msgf("Blackfire: Unable to set from ini file %v, endpoint %v: %v", path, endpoint, err)
		}
	}

	if section.HasKey("timeout") {
		timeout := getStringFromIniSection(section, "timeout")
		var err error
		if this.AgentTimeout, err = this.parseSeconds(timeout); err != nil {
			Log.Error().Msgf("Blackfire: Unable to set from ini file %v, timeout %v: %v", path, timeout, err)
		}
	}

	setLogFile(this.LogFile)
	setLogLevel(this.LogLevel)
}

// Necessary because go 1.12 doesn't have reflect.IsZero
func (this *BlackfireConfiguration) valueIsZero(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}

	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(v.Float()) == 0
	case reflect.Complex64, reflect.Complex128:
		c := v.Complex()
		return math.Float64bits(real(c)) == 0 && math.Float64bits(imag(c)) == 0
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice, reflect.UnsafePointer:
		return v.IsNil()
	case reflect.String:
		return v.Len() == 0
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !this.valueIsZero(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !this.valueIsZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

func (this *BlackfireConfiguration) configureFromConfiguration(srcConfig *BlackfireConfiguration) {
	if srcConfig == nil {
		Log.Debug().Msgf("Blackfire: Manual config not provided")
		return
	}

	sv := reflect.ValueOf(srcConfig).Elem()
	dv := reflect.ValueOf(this).Elem()
	for i := 0; i < sv.NumField(); i++ {
		sField := sv.Field(i)
		dField := dv.Field(i)
		if !this.valueIsZero(sField) {
			Log.Debug().Msgf("Blackfire: Set %v manually to %v", sField.Type().Name(), sField)
			dField.Set(sField)
		}
	}

	setLogFile(this.LogFile)
	setLogLevel(this.LogLevel)
}

// ----------
// Public API
// ----------

// Initialize this Blackfire configuration.
// Configuration is initialized in a set order, with later steps overriding
// earlier steps. Missing or zero values will not be applied.
// See: Zero values https://tour.golang.org/basics/12
//
// Initialization order:
// * Defaults
// * INI file
// * Environment variables
// * Manual configuration
//
// manualConfig will be ignored if nil.
// iniFilePath will be ignored if "".
func NewBlackfireConfiguration(manualConfig *BlackfireConfiguration, iniFilePath string) (this *BlackfireConfiguration) {
	this = new(BlackfireConfiguration)
	this.Init(manualConfig, iniFilePath)
	return this
}

// Initialize this Blackfire configuration.
// Configuration is initialized in a set order, with later steps overriding
// earlier steps. Missing or zero values will not be applied.
// See: Zero values https://tour.golang.org/basics/12
//
// Initialization order:
// * Defaults
// * INI file
// * Environment variables
// * Manual configuration
//
// manualConfig will be ignored if nil.
// iniFilePath will be ignored if "".
func (this *BlackfireConfiguration) Init(manualConfig *BlackfireConfiguration, iniFilePath string) {
	this.configureFromDefaults()

	// This allows us to debug ini file loading issues.
	this.configureLoggingFromEnv()

	Log.Debug().Msgf("Blackfire: Read configuration from INI file %v", iniFilePath)
	this.configureFromIniFile(iniFilePath)
	Log.Debug().Msgf("Blackfire: Read configuration from ENV")
	this.configureFromEnv()
	Log.Debug().Msgf("Blackfire: Read configuration from manual settings")
	this.configureFromConfiguration(manualConfig)

	Log.Debug().Interface("configuration", this).Msg("Finished configuration")
}
