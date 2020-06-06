package patch

import (
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type brokenPatcher struct {}

func (b brokenPatcher) Type() types.PatchType {
	return "broken"
}

func (b brokenPatcher) Data(obj runtime.Object) ([]byte, error) {
	return nil, fmt.Errorf("why was I made to feel pain")
}

var _ client.Patch = &brokenPatcher{}

var _ = Describe("IsPatchRequired", func() {
	var originalObject *corev1.Service

	BeforeEach(func() {
		originalObject = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test-namespace",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 80,
					},
				},
				Selector: map[string]string{
					"app":     "backend",
					"version": "v1",
				},
			},
		}
	})

	When("there is no difference", func() {
		var unchangedObject *corev1.Service

		BeforeEach(func() {
			unchangedObject = originalObject.DeepCopy()
		})

		It("should state no patch is required", func() {
			required, err := IsPatchRequired(unchangedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(required).To(BeFalse(), "identical objects should not require patching")
		})
	})

	When("there is a difference", func() {
		var changedObject *corev1.Service

		BeforeEach(func() {
			changedObject = originalObject.DeepCopy()
			changedObject.Labels = labels.Set{
				"deployment": "best-deployment",
			}
		})

		It("should state a patch is required", func() {
			required, err := IsPatchRequired(changedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(required).To(BeTrue(), "changed objects should require patching")
		})
	})

	When("the patch fails to generate", func() {
		It("should return an error", func() {
			_, err := IsPatchRequired(nil, &brokenPatcher{})
			Expect(err).ToNot(Succeed(), "failing to generate a patch should return an error")
		})
	})
})
