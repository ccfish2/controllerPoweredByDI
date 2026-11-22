package gateway_api

import (
	"context"
	"io"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.
//
// The envtest bootstrap (BeforeSuite / AfterSuite, k8sClientforTest) lives in suite_test.go.

// newGatewayClass builds a (cluster-scoped) GatewayClass.
func newGatewayClass(name, ctrlName string) *gatewayv1.GatewayClass {
	return &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(ctrlName),
			Description:    ptrTo("test GatewayClass " + name),
		},
	}
}

func ptrTo[T any](v T) *T { return &v }

var _ = Describe("GatewayClass Controller", func() {
	var (
		ctx        context.Context
		reconciler *gatewayClassReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Reconcile is invoked directly; no manager is needed.
		reconciler = &gatewayClassReconciler{
			Client:         k8sClientforTest,
			Scheme:         scheme.Scheme,
			controllerName: controllerName,
			logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
	})

	reconcileByName := func(name string) (ctrl.Result, error) {
		// GatewayClass is cluster-scoped, so the request has no namespace.
		return reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
	}

	deleteAndWait := func(gc *gatewayv1.GatewayClass) {
		Expect(client.IgnoreNotFound(k8sClientforTest.Delete(ctx, gc))).To(Succeed())
		Eventually(func() bool {
			err := k8sClientforTest.Get(ctx, client.ObjectKeyFromObject(gc), &gatewayv1.GatewayClass{})
			return errors.IsNotFound(err)
		}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())
	}

	Context("when a GatewayClass owned by this controller exists", func() {
		const name = "dolphin"
		var gc *gatewayv1.GatewayClass

		BeforeEach(func() {
			gc = newGatewayClass(name, controllerName)
			Expect(k8sClientforTest.Create(ctx, gc)).To(Succeed())
		})
		AfterEach(func() { deleteAndWait(gc) })

		It("sets Accepted=True and the supported features", func() {
			By("having no Accepted condition before reconcile")
			before := &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, before)).To(Succeed())
			By("having only the default Pending condition before reconcile")
			cond := meta.FindStatusCondition(before.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
			Expect(cond.Reason).To(Equal("Pending"))

			By("invoking Reconcile")
			res, err := reconcileByName(name)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))

			By("observing the patched status")
			after := &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, after)).To(Succeed())

			cond = meta.FindStatusCondition(after.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(string(gatewayv1.GatewayClassReasonAccepted)))
			Expect(cond.Message).To(Equal(gatewayClassAcceptedMessage))
			Expect(cond.ObservedGeneration).To(Equal(after.Generation))

			Expect(after.Status.SupportedFeatures).To(Equal(gatewayClassSupportedFeatures))
		})

		It("is idempotent when reconciled repeatedly", func() {
			for i := 0; i < 3; i++ {
				_, err := reconcileByName(name)
				Expect(err).NotTo(HaveOccurred())
			}
			got := &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, got)).To(Succeed())
			Expect(got.Status.Conditions).To(HaveLen(1))
		})
	})

	Context("when a GatewayClass belongs to another controller", func() {
		const name = "other-controller"
		var gc *gatewayv1.GatewayClass

		BeforeEach(func() {
			gc = newGatewayClass(name, "example.com/some-other-controller")
			Expect(k8sClientforTest.Create(ctx, gc)).To(Succeed())
		})
		AfterEach(func() { deleteAndWait(gc) })

		It("ignores it and leaves its status untouched", func() {
			_, err := reconcileByName(name)
			Expect(err).NotTo(HaveOccurred())

			got := &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, got)).To(Succeed())
			// foreign controller: status must be unchanged from what the API server defaulted
			before := &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, before)).To(Succeed())

			_, err = reconcileByName(name)
			Expect(err).NotTo(HaveOccurred())

			got = &gatewayv1.GatewayClass{}
			Expect(k8sClientforTest.Get(ctx, types.NamespacedName{Name: name}, got)).To(Succeed())
			Expect(got.Status.Conditions).To(Equal(before.Status.Conditions))
			Expect(got.Status.SupportedFeatures).To(BeEmpty())

		})
	})

	Context("when the GatewayClass does not exist", func() {
		It("returns success without error", func() {
			res, err := reconcileByName("does-not-exist")
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))
		})
	})

	Context("when the GatewayClass is deleted after being reconciled", func() {
		It("is gone from the API server and a later reconcile is a no-op", func() {
			const name = "to-be-deleted"
			gc := newGatewayClass(name, controllerName)
			Expect(k8sClientforTest.Create(ctx, gc)).To(Succeed())

			_, err := reconcileByName(name)
			Expect(err).NotTo(HaveOccurred())

			deleteAndWait(gc)

			res, err := reconcileByName(name)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(ctrl.Result{}))
		})
	})
})
