package composite

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = Describe("Composite", func() {
	ctx := context.Background()

	Context("reconciling newly created resource", func() {
		var parentResource unstructured.Unstructured
		var parentKey types.NamespacedName
		var children []TypedObject

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

			children = []TypedObject{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-1",
						Namespace: parentResource.GetNamespace(),
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name: "http",
								Port: 80,
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
								Name: "http",
								Port: 80,
							},
						},
					},
				},
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

		BeforeEach(func() {
			reconciler := Reconciler{
				Client: k8sClient,
				Log:    zap.New(zap.UseDevMode(true)),
				Scheme: scheme.Scheme,
			}

			err := reconciler.Reconcile(ctx, &parentResource, children)
			Expect(err).ToNot(HaveOccurred())

			err = k8sClient.Get(ctx, parentKey, &parentResource)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should write deployed kinds", func() {
			accessor := AccessState(&parentResource)
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

			Expect(svc.Labels[parentLabel]).To(Equal(string(parentResource.GetUID())))
		})
	})

	It("should clean up old kinds", func() {
		By("initial setup")

		parentResource := unstructured.Unstructured{}
		parentResource.SetGroupVersionKind(customResourceGVK)
		parentResource.SetNamespace("default")
		parentResource.SetGenerateName("my-resource-")

		accessor := AccessState(&parentResource)

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

		reconciler := Reconciler{
			Client: k8sClient,
			Log:    zap.New(zap.UseDevMode(true)),
			Scheme: scheme.Scheme,
		}

		By("reconciling initial children")

		childrenA := []TypedObject{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service",
					Namespace: parentResource.GetNamespace(),
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
		}

		err = reconciler.Reconcile(ctx, &parentResource, childrenA)
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Get(ctx, parentKey, &parentResource)
		Expect(err).ToNot(HaveOccurred())

		state, err := accessor.GetCompositeState()
		Expect(err).ToNot(HaveOccurred())

		Expect(state.DeployedKinds).To(Equal([]schema.GroupVersionKind{
			{Group: "", Version: "v1", Kind: "Service"},
		}))

		By("reconciling second set of children")

		childrenB := []TypedObject{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config-map",
					Namespace: parentResource.GetNamespace(),
				},
			},
		}

		err = reconciler.Reconcile(ctx, &parentResource, childrenB)
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
