package resource

import (
	"context"
	"time"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kubevirt.io/cloud-provider-kubevirt/test/e2e/naming"
)

func Delete(c client.Client, resource client.Object, opts ...client.DeleteOption) {
	err := c.Delete(context.TODO(), resource, opts...)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func() error {
		return c.Get(context.TODO(), naming.NamespacedName(resource), resource)
	}, 120*time.Second, time.Second).Should(WithTransform(errors.IsNotFound, BeTrue()))
}

func Create(c client.Client, resource client.Object, opts ...client.CreateOption) {
	err := c.Create(context.TODO(), resource, opts...)
	Expect(err).NotTo(HaveOccurred())
	Eventually(func() error {
		return c.Get(context.TODO(), naming.NamespacedName(resource), resource)
	}, 120*time.Second, time.Second).Should(BeNil())
}
