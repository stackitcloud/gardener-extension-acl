// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved.
// This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package controller

import (
	"context"
	"slices"

	"github.com/gardener/gardener/pkg/api/indexer"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/apis/seedmanagement"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("infrastructurePredicate", func() {
	var (
		p     predicate.TypedPredicate[*extensionsv1alpha1.Infrastructure]
		infra *extensionsv1alpha1.Infrastructure
	)

	BeforeEach(func() {
		p = infrastructurePredicate()

		infra = &extensionsv1alpha1.Infrastructure{
			Status: extensionsv1alpha1.InfrastructureStatus{
				EgressCIDRs: []string{"52.53.54.55/32", "152.153.154.155/32"},
			},
		}
	})

	Describe("#Create", func() {
		It("should return false", func() {
			Expect(p.Create(event.TypedCreateEvent[*extensionsv1alpha1.Infrastructure]{})).To(BeFalse())
		})
	})

	Describe("#Delete", func() {
		It("should return false", func() {
			Expect(p.Delete(event.TypedDeleteEvent[*extensionsv1alpha1.Infrastructure]{})).To(BeFalse())
		})
	})

	Describe("#Generic", func() {
		It("should return false", func() {
			Expect(p.Generic(event.TypedGenericEvent[*extensionsv1alpha1.Infrastructure]{})).To(BeFalse())
		})
	})

	Describe("#Update", func() {
		It("should return true if EgressCIDRs changed", func() {
			newInfra := infra.DeepCopy()
			newInfra.Status.EgressCIDRs = append(newInfra.Status.EgressCIDRs, "42.43.44.45/32")

			Expect(p.Update(event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]{ObjectNew: newInfra, ObjectOld: infra})).To(BeTrue())
		})

		It("should return false if EgressCIDRs have not changed", func() {
			newInfra := infra.DeepCopy()

			Expect(p.Update(event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]{ObjectNew: newInfra, ObjectOld: infra})).To(BeFalse())
		})

		It("should return false if EgressCIDRs contain the same values in different order", func() {
			newInfra := infra.DeepCopy()
			slices.Reverse(newInfra.Status.EgressCIDRs)

			Expect(p.Update(event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]{ObjectNew: newInfra, ObjectOld: infra})).To(BeFalse())
		})

		It("should return false if any other status field changed", func() {
			newInfra := infra.DeepCopy()
			newInfra.Status.LastOperation = &gardencorev1beta1.LastOperation{Progress: 42}

			Expect(p.Update(event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]{ObjectNew: newInfra, ObjectOld: infra})).To(BeFalse())
		})
	})
})

var _ = Describe("shootsOfManagedSeeds", func() {
	Describe("predicate", func() {
		var (
			p     predicate.TypedPredicate[*gardencorev1beta1.Shoot]
			shoot *gardencorev1beta1.Shoot
			ms    *seedmanagementv1alpha1.ManagedSeed
			c     client.Client
		)
		BeforeEach(func(ctx context.Context) {
			scheme := runtime.NewScheme()
			Expect(gardencorev1beta1.AddToScheme(scheme)).To(Succeed())
			Expect(seedmanagementv1alpha1.AddToScheme(scheme)).To(Succeed())
			c = fake.NewClientBuilder().
				WithScheme(scheme).
				WithIndex(&seedmanagementv1alpha1.ManagedSeed{}, seedmanagement.ManagedSeedShootName, indexer.ManagedSeedShootNameIndexerFunc).
				Build()

			p = shootsOfManagedSeedsPredicate(c)
			shoot = &gardencorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Status: gardencorev1beta1.ShootStatus{
					Networking: &gardencorev1beta1.NetworkingStatus{},
				},
			}
			ms = &seedmanagementv1alpha1.ManagedSeed{
				ObjectMeta: metav1.ObjectMeta{
					Name: "seed",
				},
				Spec: seedmanagementv1alpha1.ManagedSeedSpec{
					Shoot: &seedmanagementv1alpha1.Shoot{
						Name: shoot.Name,
					},
				},
			}
			Expect(c.Create(ctx, shoot)).To(Succeed())
			Expect(c.Create(ctx, ms)).To(Succeed())
		})

		It("should return true if egressCIDR of shoot with managedseed changed", func() {
			shootNew := shoot.DeepCopy()
			shootNew.Status.Networking.EgressCIDRs = []string{"192.168.3.4/32"}
			Expect(p.Update(event.TypedUpdateEvent[*gardencorev1beta1.Shoot]{
				ObjectNew: shootNew,
				ObjectOld: shoot,
			})).To(BeTrue(), "update predicate")
		})

		It("should return false if managedseed is not found for shoot", func() {
			Expect(c.Delete(ctx, ms)).To(Succeed())
			Expect(p.Update(event.TypedUpdateEvent[*gardencorev1beta1.Shoot]{
				ObjectNew: shoot,
				ObjectOld: shoot,
			})).To(BeFalse(), "update predicate")
		})
	})

	Describe("mapToExtensions", func() {
		var (
			shoot *gardencorev1beta1.Shoot
			c     client.Client
		)
		BeforeEach(func() {
			scheme := runtime.NewScheme()
			Expect(gardencorev1beta1.AddToScheme(scheme)).To(Succeed())
			Expect(extensionsv1alpha1.AddToScheme(scheme)).To(Succeed())
			c = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			shoot = &gardencorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			}
		})

		It("should return only acl extensions and extensions of class garden inside garden namespace", func(ctx context.Context) {
			extensions := []*extensionsv1alpha1.Extension{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-in-garden-namespace",
					},
					Spec: extensionsv1alpha1.ExtensionSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Class: ptr.To(extensionsv1alpha1.ExtensionClassGarden),
							Type:  "acl",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "not-garden-extension",
						Namespace: "garden",
					},

					Spec: extensionsv1alpha1.ExtensionSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type: "acl",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "not-acl-extension",
						Namespace: "garden",
					},
					Spec: extensionsv1alpha1.ExtensionSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type:  "foo",
							Class: ptr.To(extensionsv1alpha1.ExtensionClassGarden),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "garden-acl-extension",
						Namespace: "garden",
					},
					Spec: extensionsv1alpha1.ExtensionSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							Type:  "acl",
							Class: ptr.To(extensionsv1alpha1.ExtensionClassGarden),
						},
					},
				},
			}

			for _, e := range extensions {
				Expect(c.Create(ctx, e)).To(Succeed())
			}
			requests := mapShootsOfManagedSeedsToExtensions(c)(ctx, shoot)
			Expect(requests).To(HaveExactElements(reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: "garden", Name: "garden-acl-extension"},
			}))
		})
	})
})
