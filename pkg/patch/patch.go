package patch

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsPatchRequired determines if a patch will produce a no-op
func IsPatchRequired(newObj runtime.Object, patch client.Patch) (bool, error) {
	p, err := patch.Data(newObj)
	if err != nil {
		return false, err
	}

	return string(p) != "{}", nil
}
