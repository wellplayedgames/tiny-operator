package patch

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type brokenPatcher struct{}

func (b brokenPatcher) Type() types.PatchType {
	return "broken"
}

func (b brokenPatcher) Data(obj client.Object) ([]byte, error) {
	return nil, fmt.Errorf("why was I made to feel pain")
}

var _ client.Patch = &brokenPatcher{}

type fakeClientState struct {
	patchCalled       bool
	patchStatusCalled bool
}

type fakeClient struct {
	state        *fakeClientState
	statusWriter fakeStatusWriter
}

type fakeStatusWriter struct {
	state *fakeClientState
}

func newFakeClient() fakeClient {
	state := &fakeClientState{
		patchCalled:       false,
		patchStatusCalled: false,
	}
	return fakeClient{
		state: state,
		statusWriter: fakeStatusWriter{
			state: state,
		},
	}
}

func (f fakeStatusWriter) Update(context.Context, client.Object, ...client.UpdateOption) error {
	panic("unimplemented")
}

func (f *fakeStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	f.state.patchStatusCalled = true
	return nil
}

func (f fakeClient) Get(context.Context, client.ObjectKey, client.Object) error {
	panic("unimplemented")
}

func (f fakeClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	panic("unimplemented")
}

func (f fakeClient) Create(context.Context, client.Object, ...client.CreateOption) error {
	panic("unimplemented")
}

func (f fakeClient) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	panic("unimplemented")
}

func (f fakeClient) Update(context.Context, client.Object, ...client.UpdateOption) error {
	panic("unimplemented")
}

func (f fakeClient) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	f.state.patchCalled = true
	return nil
}

func (f fakeClient) DeleteAllOf(context.Context, client.Object, ...client.DeleteAllOfOption) error {
	panic("unimplemented")
}

func (f fakeClient) Status() client.StatusWriter {
	return &f.statusWriter
}

func (f fakeClient) Scheme() *runtime.Scheme {
	//TODO implement me
	panic("implement me")
}

func (f fakeClient) RESTMapper() meta.RESTMapper {
	//TODO implement me
	panic("implement me")
}

var _ client.Client = &fakeClient{}

var _ = Describe("IsPatchRequired", func() {
	var originalObject *corev1.Service

	BeforeEach(func() {
		originalObject = makeTestService()
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

var _ = Describe("MaybePatch", func() {
	var originalObject *corev1.Service
	var k8sClient fakeClient

	BeforeEach(func() {
		originalObject = makeTestService()
		k8sClient = newFakeClient()
	})

	When("there is no difference", func() {
		var unchangedObject *corev1.Service

		BeforeEach(func() {
			unchangedObject = originalObject.DeepCopy()
		})

		It("should not patch the object", func() {
			patched, err := MaybePatch(context.Background(), k8sClient, unchangedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(patched).To(BeFalse(), "should not patch an identical object")
			Expect(k8sClient.state.patchCalled).To(BeFalse(), "should not have called patch")
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

		It("should patch the object", func() {
			patched, err := MaybePatch(context.Background(), k8sClient, changedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(patched).To(BeTrue(), "should patch a changed object")
			Expect(k8sClient.state.patchCalled).To(BeTrue(), "should have called patch")
		})
	})

	When("the patch fails to generate", func() {
		It("should return an error", func() {
			patched, err := MaybePatch(context.Background(), k8sClient, nil, brokenPatcher{})
			Expect(err).ToNot(Succeed(), "should fail")
			Expect(patched).To(BeFalse(), "should not patch if unable to generate patch")
			Expect(k8sClient.state.patchCalled).To(BeFalse(), "should not have called patch")
		})
	})
})

var _ = Describe("MaybePatchStatus", func() {
	var originalObject *corev1.Service
	var k8sClient fakeClient

	BeforeEach(func() {
		originalObject = makeTestService()
		k8sClient = newFakeClient()
	})

	When("there is no difference", func() {
		var unchangedObject *corev1.Service

		BeforeEach(func() {
			unchangedObject = originalObject.DeepCopy()
		})

		It("should not patch the object", func() {
			patched, err := MaybePatchStatus(context.Background(), k8sClient, unchangedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(patched).To(BeFalse(), "should not patch an identical object")
			Expect(k8sClient.state.patchStatusCalled).To(BeFalse(), "should not have called patch")
		})
	})

	When("there is a difference", func() {
		var changedObject *corev1.Service

		BeforeEach(func() {
			changedObject = originalObject.DeepCopy()
			changedObject.Status.LoadBalancer = corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP:       "1.2.3.4",
						Hostname: "website.cool",
					},
				},
			}
		})

		It("should patch the object", func() {
			patched, err := MaybePatchStatus(context.Background(), k8sClient, changedObject, client.MergeFrom(originalObject))
			Expect(err).To(Succeed(), "should not fail")
			Expect(patched).To(BeTrue(), "should patch an changed object")
			Expect(k8sClient.state.patchStatusCalled).To(BeTrue(), "should have called patch")
		})
	})

	When("the patch fails to generate", func() {
		It("should return an error", func() {
			patched, err := MaybePatchStatus(context.Background(), k8sClient, nil, brokenPatcher{})
			Expect(err).ToNot(Succeed(), "should fail")
			Expect(patched).To(BeFalse(), "should not patch if unable to generate patch")
			Expect(k8sClient.state.patchStatusCalled).To(BeFalse(), "should not have called patch")
		})
	})
})

func makeTestService() *corev1.Service {
	return &corev1.Service{
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
}
