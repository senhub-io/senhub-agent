package logger

import (
	"os"

	"github.com/rs/zerolog"
)

type Logger = zerolog.Logger

func NewLogger() *Logger {
	logger := zerolog.
		New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().
		Timestamp().
		Logger()

	return &logger
}
