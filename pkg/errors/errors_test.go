package errors

import (
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	errTest = errors.New("error message")
)

var _ = Describe("CompositeError", func() {
	It("should defer its message to a single error", func() {
		err := &compositeError{
			InnerErrors: []error{errTest},
		}
		Expect(err.Error()).To(Equal(errTest.Error()))
	})

	It("should combine messages for more than one error", func() {
		err := &compositeError{
			InnerErrors: []error{errTest, errTest},
		}
		msg := fmt.Sprintf(
			"multiple errors occurred: %s, %s",
			errTest.Error(),
			errTest.Error())
		Expect(err.Error()).To(Equal(msg))
	})

	It("should return something for no errors", func() {
		err := &compositeError{}
		Expect(err.Error()).ToNot(BeEmpty())
	})

	Context("Append", func() {
		It("should return the new error if appending to nil", func() {
			Expect(Append(nil, errTest)).To(BeIdenticalTo(errTest))
		})

		It("should produce a CompositeError when combining two errors", func() {
			_, ok := Append(errTest, errTest).(CompositeError)
			Expect(ok).To(BeTrue())
		})

		It("should combine 5 errors", func() {
			err := Append(
				errTest,
				errTest,
				errTest,
				errTest,
				errTest)
			_, ok := err.(CompositeError)
			Expect(ok).To(BeTrue())
		})
	})

	Context("APIStatuses", func() {
		It("should return a single status for k8s error types", func() {
			statuses, onlyStatuses := APIStatuses(k8serrors.NewBadRequest("Oh no"))
			getReason := func(s metav1.Status) metav1.StatusReason { return s.Reason }
			Expect(onlyStatuses).To(BeTrue())
			Expect(statuses).To(HaveLen(1))
			Expect(statuses[0]).To(WithTransform(getReason, Equal(metav1.StatusReasonBadRequest)))
		})

		It("should return a list of errors for a list of k8s errors", func() {
			err := Append(
				k8serrors.NewBadRequest("My bad"),
				k8serrors.NewServiceUnavailable("oh no"))
			statuses, onlyStatuses := APIStatuses(err)
			Expect(onlyStatuses).To(BeTrue())
			Expect(statuses).To(HaveLen(2))
		})

		It("should return an empty list for an empty composite", func() {
			statuses, onlyStatuses := APIStatuses(&compositeError{})
			Expect(onlyStatuses).To(BeTrue())
			Expect(statuses).To(HaveLen(0))
		})

		It("should return false if it is not a status error", func() {
			statuses, onlyStatuses := APIStatuses(errTest)
			Expect(onlyStatuses).To(BeFalse())
			Expect(statuses).To(HaveLen(0))
		})

		It("should return false if any elements are not statuses", func() {
			err := Append(
				k8serrors.NewBadRequest("My bad"),
				errTest)
			statuses, onlyStatuses := APIStatuses(err)
			Expect(onlyStatuses).To(BeFalse())
			Expect(statuses).To(HaveLen(1))
		})
	})

	Context("AllErrors", func() {
		It("should return true for an empty list", func() {
			Expect(AllErrors(nil, nil)).To(BeTrue())
		})

		It("should return true if all statuses match", func() {
			err := Append(
				k8serrors.NewBadRequest("alas"),
				k8serrors.NewBadRequest("bacon"))
			Expect(AllErrors(err, k8serrors.IsBadRequest)).To(BeTrue())
		})

		It("should return false if any statuses do not match", func() {
			err := Append(
				k8serrors.NewBadRequest("alas"),
				errTest)
			Expect(AllErrors(err, k8serrors.IsBadRequest)).To(BeFalse())
		})
	})
})
