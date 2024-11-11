package logger

import (
	"os"

	"github.com/rs/zerolog"
)

type Logger = zerolog.Logger

func NewLogger() *Logger {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	return &logger
}
