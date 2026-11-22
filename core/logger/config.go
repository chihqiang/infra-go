package logger

import (
	"strings"

	"github.com/chihqiang/ten-go/core/mapping"
)

const (
	// JSONEncoding outputs JSON.
	JSONEncoding = "json"
	// ConsoleEncoding outputs a plain, human-readable line. It is the value used by
	// Encoding="plain" in Config.
	ConsoleEncoding = "plain"

	// ModeConsole writes to stdout (access) and stderr (error/severe).
	ModeConsole = "console"
	// ModeFile writes rotating files under Config.Path.
	ModeFile = "file"
	// ModeVolume writes under Config.Path/<ServiceName>/<hostname>.
	ModeVolume = "volume"

	// RotationDaily rotates at midnight; the default.
	RotationDaily = "daily"
	// RotationSize rotates once the file reaches Config.MaxSize megabytes.
	RotationSize = "size"

	defaultTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

// Log level names accepted by Config.Level. The Level type itself is numeric;
// these are the configuration values.
const (
	levelDebug   = "debug"
	levelInfo    = "info"
	levelWarn    = "warn"
	levelWarning = "warning"
	levelError   = "error"
	levelSevere  = "severe"
)

// Config is the log configuration.
type Config struct {
	// ServiceName is the service name; it is emitted as the "app" field and volume
	// mode uses it to build the log directory.
	ServiceName string `json:",optional"`
	// Mode selects console, file or volume output; default is console.
	Mode string `json:",default=console,options=[console,file,volume]"`
	// Encoding selects json or plain output; default is json.
	Encoding string `json:",default=json,options=[json,plain]"`
	// TimeFormat is the timestamp layout; an empty value uses ISO8601.
	TimeFormat string `json:",optional"`
	// Path is the log directory for file and volume modes; default is "logs".
	Path string `json:",default=logs"`
	// Level is one of debug, info, error or severe; default is info. "severe" keeps only
	// fatal entries, which are the ones written to severe.log.
	Level string `json:",default=info,options=[debug,info,error,severe]"`
	// MaxContentLength truncates the message to this many bytes; zero means no limit.
	MaxContentLength uint32 `json:",optional"`
	// Compress reports whether rotated log files are gzip-compressed.
	Compress bool `json:",optional"`
	// Stat enables logging statistics; kept for configuration compatibility.
	Stat bool `json:",default=true"`
	// KeepDays is how many days backups are kept; zero keeps them forever.
	KeepDays int `json:",optional"`
	// StackCooldownMillis limits how often stack traces are attached; default 100ms.
	StackCooldownMillis int `json:",default=100"`
	// MaxBackups is the maximum number of backups kept; zero keeps all.
	MaxBackups int `json:",default=0"`
	// MaxSize is the maximum file size in MB for size rotation; zero means no limit.
	MaxSize int `json:",default=0"`
	// Rotation selects daily or size rotation; default is daily.
	Rotation string `json:",default=daily,options=[daily,size]"`
}

// withDefaults fills the tag defaults, then overlays the caller's non-zero
// fields, and finally applies the ISO8601 time format when it is still empty.
func (c Config) withDefaults() Config {
	var filled Config
	mapping.MustFillAndOverride(&filled, c)
	if filled.TimeFormat == "" {
		filled.TimeFormat = defaultTimeFormat
	}
	return filled
}

// parseLevel maps a configured level name onto the numeric Level.
func parseLevel(level string) Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case levelDebug:
		return DebugLevel
	case levelWarn, levelWarning:
		return WarnLevel
	case levelError:
		return ErrorLevel
	case levelSevere:
		return FatalLevel
	case "dpanic":
		return DPanicLevel
	case "panic":
		return PanicLevel
	case "fatal":
		return FatalLevel
	default:
		return InfoLevel
	}
}
