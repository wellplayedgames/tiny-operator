package patch

import (
	"context"
	"fmt"
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

// MaybePatch will patch an object if the patch does not produce a no-op
func MaybePatch(ctx context.Context, client client.Client, newObj runtime.Object, patch client.Patch) (bool, error) {
	required, err := IsPatchRequired(newObj, patch)
	if err != nil {
		return false, fmt.Errorf("unable to build patch: %v", err)
	}

	if !required {
		return false, nil
	}

	return true, client.Patch(ctx, newObj, patch)
}

// MaybePatchStatus will patch an object's status if the patch does not produce a no-op
func MaybePatchStatus(ctx context.Context, client client.Client, newObj runtime.Object, patch client.Patch) (bool, error) {
	required, err := IsPatchRequired(newObj, patch)
	if err != nil {
		return false, fmt.Errorf("unable to build patch: %v", err)
	}

	if !required {
		return false, nil
	}

	return true, client.Status().Patch(ctx, newObj, patch)
}
