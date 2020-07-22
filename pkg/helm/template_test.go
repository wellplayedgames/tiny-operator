package helm

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("RenderChart", func() {
	var err error
	var chrt *chart.Chart
	scheme := scheme.Scheme
	namespace := "default"

	BeforeEach(func() {
		chrt, err = loader.LoadDir("testdata/test_chart")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should succeed and produce the right number of resources", func() {
		values := map[string]interface{} {
			"ham": "crossword",
		}
		objects, err := RenderChart(scheme, chrt, values, namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(objects).To(HaveLen(2))

		for _, obj := range objects {
			if obj.GetObjectKind().GroupVersionKind().Kind == "Service" {
				s := obj.(*corev1.Service)
				Expect(s.Name).To(Equal("ham-hands-mcgee-crossword"))
			}
		}
	})
})
