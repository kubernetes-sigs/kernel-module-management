package controllers

import (
	"context"
	"errors"
	"time"

	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("JobGCReconciler_Reconcile", func() {
	ctx := context.Background()

	type testCase struct {
		deletionTimestamp     time.Time
		gcDelay               time.Duration
		podPhase              v1.PodPhase
		shouldRemoveFinalizer bool
		shouldSetRequeueAfter bool
	}

	DescribeTable(
		"should work as expected",
		func(tc testCase) {
			ctrl := gomock.NewController(GinkgoT())
			mockClient := testclient.NewMockClient(ctrl)

			pod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: tc.deletionTimestamp},
				},
				Status: v1.PodStatus{Phase: tc.podPhase},
			}

			if tc.shouldRemoveFinalizer {
				mockClient.EXPECT().Patch(ctx, &pod, gomock.Any())
			}

			res, err := NewJobGCReconciler(mockClient, time.Minute).Reconcile(ctx, &pod)

			Expect(err).NotTo(HaveOccurred())

			if tc.shouldSetRequeueAfter {
				Expect(res.RequeueAfter).NotTo(BeZero())
			} else {
				Expect(res.RequeueAfter).To(BeZero())
			}
		},
		Entry(
			"pod succeeded, before now+delay",
			testCase{
				deletionTimestamp:     time.Now(),
				gcDelay:               time.Hour,
				podPhase:              v1.PodSucceeded,
				shouldSetRequeueAfter: true,
			},
		),
		Entry(
			"pod succeeded, after now+delay",
			testCase{
				deletionTimestamp:     time.Now().Add(-time.Hour),
				gcDelay:               time.Minute,
				podPhase:              v1.PodSucceeded,
				shouldRemoveFinalizer: true,
			},
		),
		Entry(
			"pod failed, before now+delay",
			testCase{
				deletionTimestamp:     time.Now(),
				gcDelay:               time.Hour,
				podPhase:              v1.PodFailed,
				shouldRemoveFinalizer: true,
			},
		),
		Entry(
			"pod failed, after now+delay",
			testCase{
				deletionTimestamp:     time.Now().Add(-time.Hour),
				gcDelay:               time.Minute,
				podPhase:              v1.PodFailed,
				shouldRemoveFinalizer: true,
			},
		),
	)

	It("should return an error if the patch failed", func() {
		ctrl := gomock.NewController(GinkgoT())
		mockClient := testclient.NewMockClient(ctrl)

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{
					Time: time.Now().Add(-2 * time.Minute),
				},
			},
		}

		mockClient.EXPECT().Patch(ctx, &pod, gomock.Any()).Return(errors.New("random error"))

		_, err := NewJobGCReconciler(mockClient, time.Minute).Reconcile(ctx, &pod)

		Expect(err).To(HaveOccurred())
	})
})
