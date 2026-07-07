package scalable

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestGenerateS3ClientNilConfig(t *testing.T) {
	params := logControllerParams{
		Logger: logrus.New(),
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generateS3Client panicked with nil config: %v", r)
		}
	}()

	if got := generateS3Client(params, nil); got != nil {
		t.Fatalf("expected nil client when config is nil, got %v", got)
	}
}
