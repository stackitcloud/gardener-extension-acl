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
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
				EgressCIDRs: []string{"52.53.54.55/32"},
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

		It("should return true if any other status field changed", func() {
			newInfra := infra.DeepCopy()
			newInfra.Status.NodesCIDR = ptr.To("100.212.123.18/27")

			Expect(p.Update(event.TypedUpdateEvent[*extensionsv1alpha1.Infrastructure]{ObjectNew: newInfra, ObjectOld: infra})).To(BeTrue())
		})
	})
})
