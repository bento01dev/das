package blob

import (
	"log/slog"

	"github.com/bento01dev/das/internal/config"
)

type DummyStepStore struct{}

func (store DummyStepStore) UploadNewSteps(appName string, steps map[string]config.ResourceStep) (string, error) {
	slog.Info("dummy upload invoked for updating new steps. enable a blob store if needed.", "app_name", appName, "steps", steps)
	return "", nil
}
