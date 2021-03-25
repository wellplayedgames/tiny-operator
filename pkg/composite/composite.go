package composite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tinyerrors "github.com/wellplayedgames/tiny-operator/pkg/errors"
)

const (
	StateAnnotation = "hive.wellplayed.games/composite-state"
	ParentLabel     = "hive.wellplayed.games/composite-parent"
)

type permanentError struct {
	error
}

// IsPermanentError returns true if the error should not result in a retry.
func IsPermanentError(err error) bool {
	if _, ok := err.(*permanentError); ok {
		return true
	}

	return false
}

func kindIndex(kinds []schema.GroupVersionKind, kind schema.GroupKind) int {
	for idx, k := range kinds {
		if k.Group == kind.Group && k.Kind == kind.Kind {
			return idx
		}
	}

	return -1
}

func hasUID(uids []types.UID, uid types.UID) bool {
	for _, i := range uids {
		if i == uid {
			return true
		}
	}

	return false
}

// State describes the state of the
type State struct {
	DeployedKinds []schema.GroupVersionKind `json:"deployedKinds,omitempty"`
}

// EnsureKinds makes sure the given kinds are included and returns true if
// any changes were made.
func (s *State) EnsureKinds(kinds []schema.GroupVersionKind) bool {
	madeChanges := false

	for _, k := range kinds {
		idx := kindIndex(s.DeployedKinds, k.GroupKind())
		if idx < 0 {
			madeChanges = true
			s.DeployedKinds = append(s.DeployedKinds, k)
			continue
		}

		gvk := s.DeployedKinds[idx]
		if gvk.Version != k.Version {
			madeChanges = true
			s.DeployedKinds[idx] = k
			continue
		}
	}

	return madeChanges
}

// StateAccessor is a type which can access the composite state of an object.
type StateAccessor interface {
	GetCompositeState() (*State, error)
	SetCompositeState(newState *State) error
}

type stateAccessor struct {
	metav1.Object
}

// AccessState provides an accessor for
func AccessState(obj metav1.Object) StateAccessor {
	return &stateAccessor{obj}
}

func (a *stateAccessor) GetCompositeState() (*State, error) {
	var state State

	anno := a.GetAnnotations()
	if anno == nil {
		return &state, nil
	}

	text, ok := anno[StateAnnotation]
	if !ok {
		return &state, nil
	}

	err := json.Unmarshal([]byte(text), &state)
	return &state, err
}

func (a *stateAccessor) SetCompositeState(state *State) error {
	by, err := json.Marshal(state)
	if err != nil {
		return err
	}

	anno := a.GetAnnotations()
	if anno == nil {
		anno = map[string]string{}
	}

	anno[StateAnnotation] = string(by)
	a.SetAnnotations(anno)
	return nil
}

// Reconciler reconciles composite resources.
type Reconciler struct {
	logger logr.Logger
	client client.Client
	scheme *runtime.Scheme
	parent runtime.Object
	owner  string

	assertedUIDs  []types.UID
	assertedKinds []schema.GroupVersionKind
	parentMeta    metav1.Object
	acc           StateAccessor
}

func New(logger logr.Logger, client client.Client, scheme *runtime.Scheme, parent runtime.Object, owner string) (*Reconciler, error) {
	parentMeta, err := meta.Accessor(parent)
	if err != nil {
		return nil, fmt.Errorf("unable to access parent meta: %w", err)
	}

	acc := AccessState(parentMeta)

	return &Reconciler{
		logger: logger,
		client: client,
		scheme: scheme,
		parent: parent,
		owner:  owner,

		parentMeta: parentMeta,
		acc:        acc,
	}, nil
}

// Reconcile child resources of a composite resource.
func (r *Reconciler) Reconcile(ctx context.Context, children []runtime.Object) error {
	state, err := r.acc.GetCompositeState()
	if err != nil {
		return &permanentError{err}
	}

	if err := r.markDesiredKinds(ctx, children, state); err != nil {
		return err
	}

	if err := r.assertChildren(ctx, children); err != nil {
		return err
	}

	if err := r.prune(ctx, state); err != nil {
		return err
	}

	return nil
}

// ReconcileWithoutPrune reconciles child resources of a composite resource without removing any existing children.
func (r *Reconciler) ReconcileWithoutPrune(ctx context.Context, children []runtime.Object) error {
	state, err := r.acc.GetCompositeState()
	if err != nil {
		return &permanentError{err}
	}

	if err := r.markDesiredKinds(ctx, children, state); err != nil {
		return err
	}

	if err := r.assertChildren(ctx, children); err != nil {
		return err
	}

	return nil
}

// Reconcile child resources of a composite resource.
func (r *Reconciler) Prune(ctx context.Context) error {
	state, err := r.acc.GetCompositeState()
	if err != nil {
		return &permanentError{err}
	}

	if err := r.prune(ctx, state); err != nil {
		return err
	}

	return nil
}

// assertChildren updates or creates all child objects.
func (r *Reconciler) assertChildren(ctx context.Context, children []runtime.Object) error {
	var passError error

	var patchOptions []client.PatchOption

	applyOptions := append(patchOptions, client.ForceOwnership, client.FieldOwner(r.owner))

	for _, child := range children {
		objToPatch := child

		err := r.client.Patch(ctx, objToPatch, client.Apply, applyOptions...)
		if err != nil {
			passError = tinyerrors.Append(passError, err)
		}

		acc, err := meta.Accessor(objToPatch)
		if err != nil {
			r.logger.Error(err, "failed to access child metadata")
			return &permanentError{err}
		}

		r.assertedUIDs = append(r.assertedUIDs, acc.GetUID())
	}

	return passError
}

// markDesiredKinds marks all new kinds, to make sure they can't get forgotten.
func (r *Reconciler) markDesiredKinds(ctx context.Context, children []runtime.Object, state *State) error {
	var updateOptions []client.UpdateOption

	parentKey := string(r.parentMeta.GetUID())

	for _, child := range children {
		// Add GVK of resource to the list of GVKs we are processing.
		gvk, err := apiutil.GVKForObject(child, r.scheme)
		if err != nil {
			return &permanentError{err}
		}

		idx := kindIndex(r.assertedKinds, gvk.GroupKind())

		if idx >= 0 {
			r.assertedKinds[idx] = gvk
		} else {
			r.assertedKinds = append(r.assertedKinds, gvk)
		}

		meta, err := meta.Accessor(child)
		if err != nil {
			return &permanentError{err}
		}

		// Associate with parent.
		labels := meta.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[ParentLabel] = parentKey
		meta.SetLabels(labels)

		// Set resource owner to parent.
		if meta.GetNamespace() != "" {
			err = controllerutil.SetControllerReference(r.parentMeta, meta, r.scheme)
			if err != nil {
				return &permanentError{err}
			}
		}

		// Ensure child GVK is set. (For structs this isn't true by default, but needed for apply.)
		child.GetObjectKind().SetGroupVersionKind(gvk)
	}

	if state.EnsureKinds(r.assertedKinds) {
		r.acc.SetCompositeState(state)
		if err := r.client.Update(ctx, r.parent, updateOptions...); err != nil {
			return err
		}
	}

	return nil
}

// getDesiredUIDs retrieves the UIDs of all desired objects
func (r *Reconciler) getDesiredUIDs(ctx context.Context, children []runtime.Object) ([]types.UID, error) {
	var passError error

	desiredUIDs := make([]types.UID, 0, len(children))
	for _, child := range children {
		objToGet := child

		key, err := client.ObjectKeyFromObject(objToGet)
		if err != nil {
			passError = tinyerrors.Append(passError, err)
		}

		if err := r.client.Get(ctx, key, objToGet); err != nil {
			passError = tinyerrors.Append(passError, err)
		}

		acc, err := meta.Accessor(objToGet)
		if err != nil {
			r.logger.Error(err, "failed to access child metadata")
			return nil, &permanentError{err}
		}

		desiredUIDs = append(desiredUIDs, acc.GetUID())
	}

	return desiredUIDs, passError
}

// prune all old objects.
func (r *Reconciler) prune(ctx context.Context, state *State) error {
	var updateOptions []client.UpdateOption
	var deleteOptions []client.DeleteOption

	parentKey := string(r.parentMeta.GetUID())
	selector := labels.SelectorFromSet(labels.Set{
		ParentLabel: parentKey,
	})

	var passError error

	for _, gvk := range state.DeployedKinds {
		var list unstructured.UnstructuredList
		list.SetGroupVersionKind(gvk)

		match := client.MatchingLabelsSelector{Selector: selector}
		err := r.client.List(ctx, &list, match)
		if err != nil {
			return err
		}

		err = list.EachListItem(func(obj runtime.Object) error {
			kind := obj.GetObjectKind()
			kind.SetGroupVersionKind(gvk)

			acc, err := meta.Accessor(obj)
			if err != nil {
				r.logger.Error(err, "failed to access child metadata")
				return &permanentError{err}
			}

			if hasUID(r.assertedUIDs, acc.GetUID()) {
				return nil
			}

			err = r.client.Delete(ctx, obj, deleteOptions...)
			if err != nil {
				passError = tinyerrors.Append(passError, err)
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	// If deleting any resources failed, fail now.
	if passError != nil {
		return passError
	}

	// Remove old types from state.
	if len(state.DeployedKinds) != len(r.assertedKinds) {
		state.DeployedKinds = r.assertedKinds
		r.acc.SetCompositeState(state)
		if err := r.client.Update(ctx, r.parent, updateOptions...); err != nil {
			return err
		}
	}

	return nil
}
