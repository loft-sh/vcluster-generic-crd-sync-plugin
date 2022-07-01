package diff

import (
	"github.com/wI2L/jsondiff"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Compare(from, to client.Object) ([]byte, error) {
	jsondiff.Compare(from, to)
}
