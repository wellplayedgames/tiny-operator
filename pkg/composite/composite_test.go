package composite_test

import (
	"context"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/wellplayedgames/tiny-operator/pkg/composite"
)

const (
	owner = "games.wellplayed.composite"
)

var _ = Describe("Composite", func() {
	var ctx context.Context
	var logger logr.Logger
	var err error

	BeforeEach(func() {
		ctx = context.Background()
		logger = zap.New(zap.UseDevMode(true))
	})

	Context("reconciling newly created resource", func() {
		var parentResource unstructured.Unstructured
		var parentKey types.NamespacedName
		var sourceChildren []client.Object
		var children []client.Object

		BeforeEach(func() {
			parentResource = unstructured.Unstructured{}
			parentResource.SetGroupVersionKind(customResourceGVK)
			parentResource.SetNamespace("default")
			parentResource.SetGenerateName("my-resource-")

			err := k8sClient.Create(ctx, &parentResource)
			Expect(err).ToNot(HaveOccurred())

			parentKey = types.NamespacedName{
				Namespace: parentResource.GetNamespace(),
				Name:      parentResource.GetName(),
			}

			sourceChildren = []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-1",
						Namespace: parentResource.GetNamespace(),
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "http",
								Protocol: corev1.ProtocolTCP,
								Port:     80,
							},
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-2",
						Namespace: parentResource.GetNamespace(),
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     "http",
								Protocol: corev1.ProtocolTCP,
								Port:     80,
							},
						},
					},
				},
			}

			children = make([]client.Object, len(sourceChildren))
			for idx, child := range sourceChildren {
				children[idx] = child.DeepCopyObject().(client.Object)
			}
		})

		AfterEach(func() {
			// GC should remove all children
			propagationPolicy := metav1.DeletePropagationForeground
			err := k8sClient.Delete(ctx, &parentResource, &client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			})
			Expect(err).ToNot(HaveOccurred())
		})

		var reconciler *composite.Reconciler
		BeforeEach(func() {
			reconciler, err = composite.New(logger, k8sClient, scheme.Scheme, &parentResource, owner)
			Expect(err).ToNot(HaveOccurred())

			err := reconciler.Reconcile(ctx, children)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Get(ctx, parentKey, &parentResource)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should write deployed kinds", func() {
			accessor := composite.AccessState(&parentResource)
			state, err := accessor.GetCompositeState()
			Expect(err).ToNot(HaveOccurred())

			Expect(state.DeployedKinds).To(Equal([]schema.GroupVersionKind{
				{Group: "", Version: "v1", Kind: "Service"},
			}))
		})

		It("should have created both services", func() {
			svc := corev1.Service{}

			err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: parentKey.Namespace,
				Name:      "service-1",
			}, &svc)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: parentKey.Namespace,
				Name:      "service-2",
			}, &svc)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have set the parent label", func() {
			svc := corev1.Service{}

			err := k8sClient.Get(ctx, types.NamespacedName{
				Namespace: parentKey.Namespace,
				Name:      "service-1",
			}, &svc)
			Expect(err).NotTo(HaveOccurred())

			Expect(svc.Labels[composite.ParentLabel]).To(Equal(string(parentResource.GetUID())))
		})

		It("shouldn't update ResourceVersion on next reconciliation", func() {
			rvs := make([]string, len(children))
			for idx, child := range children {
				m, err := meta.Accessor(child)
				Expect(err).ToNot(HaveOccurred())
				Expect(m.GetResourceVersion()).ToNot(BeEmpty())

				rvs[idx] = m.GetResourceVersion()
			}

			for idx, child := range sourceChildren {
				children[idx] = child.DeepCopyObject().(client.Object)
			}

			err := reconciler.Reconcile(ctx, children)
			Expect(err).ToNot(HaveOccurred())

			for idx, child := range children {
				m, err := meta.Accessor(child)
				Expect(err).ToNot(HaveOccurred())
				Expect(m.GetResourceVersion()).ToNot(BeEmpty())
				Expect(m.GetResourceVersion()).To(Equal(rvs[idx]))
			}
		})

		It("should not override non-applied fields", func() {
			svc := children[0].(*corev1.Service)
			svc.Spec.Type = corev1.ServiceTypeLoadBalancer
			err := k8sClient.Update(ctx, svc)
			Expect(err).ToNot(HaveOccurred())

			for idx, child := range sourceChildren {
				children[idx] = child.DeepCopyObject().(client.Object)
			}

			err = reconciler.Reconcile(ctx, children)
			Expect(err).ToNot(HaveOccurred())

			svc = children[0].(*corev1.Service)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
		})
	})

	It("should clean up old kinds", func() {
		By("initial setup")

		parentResource := unstructured.Unstructured{}
		parentResource.SetGroupVersionKind(customResourceGVK)
		parentResource.SetNamespace("default")
		parentResource.SetGenerateName("my-resource-")

		accessor := composite.AccessState(&parentResource)

		err := k8sClient.Create(ctx, &parentResource)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			propagationPolicy := metav1.DeletePropagationForeground
			err := k8sClient.Delete(ctx, &parentResource, &client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			})
			Expect(err).ToNot(HaveOccurred())
		}()

		parentKey := types.NamespacedName{
			Namespace: parentResource.GetNamespace(),
			Name:      parentResource.GetName(),
		}

		reconciler, err := composite.New(logger, k8sClient, scheme.Scheme, &parentResource, owner)
		Expect(err).ToNot(HaveOccurred())

		By("reconciling initial children")

		childrenA := []client.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: parentResource.GetNamespace(),
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "http",
							Protocol: corev1.ProtocolTCP,
							Port:     80,
						},
					},
				},
			},
		}

		err = reconciler.Reconcile(ctx, childrenA)
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Get(ctx, parentKey, &parentResource)
		Expect(err).ToNot(HaveOccurred())

		state, err := accessor.GetCompositeState()
		Expect(err).ToNot(HaveOccurred())

		Expect(state.DeployedKinds).To(Equal([]schema.GroupVersionKind{
			{Group: "", Version: "v1", Kind: "Service"},
		}))

		By("reconciling second set of children")

		reconciler, err = composite.New(logger, k8sClient, scheme.Scheme, &parentResource, owner)
		Expect(err).ToNot(HaveOccurred())

		childrenB := []client.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config-map",
					Namespace: parentResource.GetNamespace(),
				},
			},
		}

		err = reconciler.Reconcile(ctx, childrenB)
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Get(ctx, parentKey, &parentResource)
		Expect(err).ToNot(HaveOccurred())

		state, err = accessor.GetCompositeState()
		Expect(err).ToNot(HaveOccurred())

		Expect(state.DeployedKinds).To(Equal([]schema.GroupVersionKind{
			{Group: "", Version: "v1", Kind: "ConfigMap"},
		}))

		svc := corev1.Service{}
		err = k8sClient.Get(ctx, types.NamespacedName{
			Namespace: parentResource.GetNamespace(),
			Name:      "my-service",
		}, &svc)
		Expect(errors.IsNotFound(err)).To(Equal(true))
	})
})
