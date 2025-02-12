package auto_update

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestAutoUpdate_GetName(t *testing.T) {
	logger := zerolog.New(os.Stderr)

	au := NewAutoUpdate(AutoUpdateConfig{
		Logger: &logger,
	})

	if au.GetName() != "AutoUpdate" {
		t.Errorf("Expected AutoUpdate, got %s", au.GetName())
	}
}
