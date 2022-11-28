package log

import (
	"io"
	"os"

	"github.com/rs/zerolog"
)

var logger zerolog.Logger

func init() {
	logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Timestamp().Logger().
		Level(zerolog.InfoLevel)
}

// Init configures the global logger.
func Init(verbose bool, jsonOutput bool) {
	var w io.Writer
	if jsonOutput {
		w = os.Stderr
	} else {
		w = zerolog.ConsoleWriter{Out: os.Stderr}
	}
	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	logger = zerolog.New(w).With().Timestamp().Logger().Level(level)
}

// L returns the global logger.
func L() *zerolog.Logger {
	return &logger
}
