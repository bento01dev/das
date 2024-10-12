package blob

import "github.com/bento01dev/das/internal/config"

type DummyStepStore struct{}

func (store DummyStepStore) UploadNewStep(appName string, step config.ResourceStep) (string, error) {
	return "", nil
}
