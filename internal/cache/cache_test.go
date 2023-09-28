package cache

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("New", func() {
	It("should return a new cache with the given item expiration duration", func() {
		c := New[string](5 * time.Nanosecond)

		Expect(c.expiration).To(Equal(5 * time.Nanosecond))
	})
})

var _ = Describe("DeleteExpired", func() {
	It("should remove all expired items", func() {
		c := New[string](100 * time.Millisecond)

		c.items["not-expired"] = item{
			Object:     1,
			Expiration: time.Now().Add(1 * time.Minute),
		}
		c.items["expired"] = item{
			Object:     2,
			Expiration: time.Now().Add(-1 * time.Millisecond),
		}

		c.DeleteExpired()

		item, found := c.items["not-expired"]
		Expect(found).To(BeTrue())
		Expect(item).ToNot(BeNil())
		Expect(item.Object).To(Equal(1))

		item, found = c.items["expired"]
		Expect(found).To(Equal(false))
		Expect(item.Object).To(BeNil())
		Expect(c.items).To(HaveLen(1))

	})
})

var _ = Describe("Get", func() {
	var (
		c *cache[string]
	)

	BeforeEach(func() {
		c = New[string](10 * time.Minute)
	})

	It("should return an existing item", func() {
		c.Set("a", 1)
		item, found := c.Get("a")

		Expect(found).To(BeTrue())
		Expect(item).To(Equal(1))
	})

	It("should return nil and false when the key is not found", func() {
		c.Set("a", 1)
		item, found := c.Get("b")

		Expect(found).To(BeFalse())
		Expect(item).To(BeNil())
	})

	It("should return nil and false when the key is expired", func() {
		c.items["expired"] = item{
			Object:     1,
			Expiration: time.Now().Add(-1 * time.Millisecond),
		}

		item, found := c.Get("expired")
		Expect(found).To(BeFalse())
		Expect(item).To(BeNil())
	})
})

var _ = Describe("Set", func() {
	var (
		c *cache[string]
	)

	BeforeEach(func() {
		c = New[string](10 * time.Minute)
	})

	It("should insert a new item", func() {
		c.Set("a", "value")

		Expect(c.items).To(HaveLen(1))
		Expect(c.items["a"]).ToNot(BeNil())
		Expect(c.items["a"].Object).To(Equal("value"))
	})

	It("should replace an existing item", func() {
		c.Set("a", 1)
		c.Set("a", 3)

		Expect(c.items).To(HaveLen(1))
		Expect(c.items["a"]).ToNot(BeNil())
		Expect(c.items["a"].Object).To(Equal(3))
	})
})

var _ = Describe("StartCollecting", func() {
	It("should remove all expired items at the given interval", func() {
		ctx := context.Background()
		c := New[string](1 * time.Nanosecond)

		c.items["not-expired"] = item{
			Object:     1,
			Expiration: time.Now().Add(1 * time.Minute),
		}
		c.items["expired"] = item{
			Object:     1,
			Expiration: time.Now().Add(-1 * time.Minute),
		}

		c.StartCollecting(ctx, 10*time.Millisecond)

		Eventually(func() bool {
			_, found := c.items["expired"]
			return found
		}).Should(BeFalse())

		// this key should not be removed yet
		item, found := c.items["not-expired"]
		Expect(found).To(BeTrue())
		Expect(item).ToNot(BeNil())
		Expect(item.Object).To(Equal(1))
	})

	It("should return when its context is cancelled", func() {
		ctx, terminate := context.WithCancel(context.Background())

		c := New[string](1 * time.Nanosecond)

		c.StartCollecting(ctx, 10*time.Millisecond)

		terminate()
		c.wg.Wait()
	})
})

var _ = Describe("WaitForTermination", func() {
	It("should wait until the StartCollecting goroutine has returned", func() {
		ctx, terminate := context.WithCancel(context.Background())

		c := New[string](1 * time.Nanosecond)

		c.StartCollecting(ctx, 10*time.Millisecond)

		terminate()
		c.WaitForTermination()
	})
})
