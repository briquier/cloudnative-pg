/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package controller

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/machinery/pkg/log"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/logtest"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
)

var _ = Describe("Peering", func() {
	Context("defaultHashToPeerID", func() {
		It("returns a value in PgBouncer's valid range [1, 16383]", func() {
			for _, ip := range []string{"10.0.0.1", "10.0.0.2", "192.168.1.100", "172.16.0.255"} {
				id := defaultHashToPeerID(ip)
				Expect(id).To(BeNumerically(">=", 1))
				Expect(id).To(BeNumerically("<=", config.MaxPeerID))
			}
		})

		It("returns deterministic results", func() {
			id1 := defaultHashToPeerID("10.0.0.1")
			id2 := defaultHashToPeerID("10.0.0.1")
			Expect(id1).To(Equal(id2))
		})

		It("returns different IDs for different IPs", func() {
			id1 := defaultHashToPeerID("10.0.0.1")
			id2 := defaultHashToPeerID("10.0.0.2")
			Expect(id1).NotTo(Equal(id2))
		})
	})

	Context("assignPeerIDs", func() {
		var logger log.Logger

		BeforeEach(func() {
			logger = logtest.NewSpy()
		})

		AfterEach(func() {
			hashToPeerID = defaultHashToPeerID
		})

		It("assigns unique IDs in [1, MaxPeerID] to each peer", func() {
			ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
			idMap, err := assignPeerIDs(ips, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(idMap).To(HaveLen(3))

			seen := make(map[int]bool)
			for _, id := range idMap {
				Expect(id).To(BeNumerically(">=", 1))
				Expect(id).To(BeNumerically("<=", config.MaxPeerID))
				Expect(seen[id]).To(BeFalse(), "duplicate peer_id %d", id)
				seen[id] = true
			}
		})

		It("is deterministic regardless of input order", func() {
			ips1 := []string{"10.0.0.3", "10.0.0.1", "10.0.0.2"}
			ips2 := []string{"10.0.0.2", "10.0.0.3", "10.0.0.1"}
			m1, err1 := assignPeerIDs(ips1, logger)
			m2, err2 := assignPeerIDs(ips2, logger)
			Expect(err1).NotTo(HaveOccurred())
			Expect(err2).NotTo(HaveOccurred())
			Expect(m1).To(Equal(m2))
		})

		It("resolves hash collisions via probing", func() {
			hashToPeerID = func(s string) int {
				stubMap := map[string]int{
					"10.0.0.1":    42,
					"10.0.0.2":    42, // collision with 10.0.0.1
					"10.0.0.2#1":  99, // probe resolves here
				}
				if v, ok := stubMap[s]; ok {
					return v
				}
				return 1
			}

			ips := []string{"10.0.0.1", "10.0.0.2"}
			idMap, err := assignPeerIDs(ips, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(idMap).To(HaveLen(2))
			Expect(idMap["10.0.0.1"]).To(Equal(42))
			Expect(idMap["10.0.0.2"]).To(Equal(99))
		})

		It("handles multiple chained collisions", func() {
			hashToPeerID = func(s string) int {
				stubMap := map[string]int{
					"10.0.0.1":    7,
					"10.0.0.2":    7,  // collides with .1
					"10.0.0.2#1":  7,  // still collides
					"10.0.0.2#2":  50, // resolved
					"10.0.0.3":    50, // collides with .2's resolved id
					"10.0.0.3#1":  77, // resolved
				}
				if v, ok := stubMap[s]; ok {
					return v
				}
				return 1
			}

			ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
			idMap, err := assignPeerIDs(ips, logger)
			Expect(err).NotTo(HaveOccurred())
			Expect(idMap).To(HaveLen(3))

			seen := make(map[int]bool)
			for _, id := range idMap {
				Expect(seen[id]).To(BeFalse(), "duplicate peer_id %d", id)
				seen[id] = true
			}
			Expect(idMap["10.0.0.1"]).To(Equal(7))
			Expect(idMap["10.0.0.2"]).To(Equal(50))
			Expect(idMap["10.0.0.3"]).To(Equal(77))
		})

		It("returns error when peer_id cannot be assigned (exhaustion)", func() {
			// Stub so that one IP gets an id and all probes collide with it
			hashToPeerID = func(s string) int {
				if s == "10.0.0.1" {
					return 1
				}
				// 10.0.0.2 and all 10.0.0.2#n return 1, so we never find a free id
				return 1
			}

			ips := []string{"10.0.0.1", "10.0.0.2"}
			idMap, err := assignPeerIDs(ips, logger)
			Expect(err).To(HaveOccurred())
			Expect(idMap).To(BeNil())
		})
	})

	Context("collectEndpointIPs", func() {
		It("returns empty for empty endpoints", func() {
			endpoints := &corev1.Endpoints{}
			ips := collectEndpointIPs(endpoints)
			Expect(ips).To(BeEmpty())
		})

		It("collects IPs from ready addresses", func() {
			endpoints := &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{IP: "10.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			}
			ips := collectEndpointIPs(endpoints)
			Expect(ips).To(ConsistOf("10.0.0.1", "10.0.0.2"))
		})

		It("collects IPs from not-ready addresses", func() {
			endpoints := &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{
						NotReadyAddresses: []corev1.EndpointAddress{
							{IP: "10.0.0.3"},
						},
					},
				},
			}
			ips := collectEndpointIPs(endpoints)
			Expect(ips).To(ConsistOf("10.0.0.3"))
		})

		It("deduplicates IPs", func() {
			endpoints := &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{IP: "10.0.0.1"},
						},
						NotReadyAddresses: []corev1.EndpointAddress{
							{IP: "10.0.0.1"},
						},
					},
				},
			}
			ips := collectEndpointIPs(endpoints)
			Expect(ips).To(HaveLen(1))
			Expect(ips).To(ConsistOf("10.0.0.1"))
		})

		It("returns sorted IPs", func() {
			endpoints := &corev1.Endpoints{
				Subsets: []corev1.EndpointSubset{
					{
						Addresses: []corev1.EndpointAddress{
							{IP: "10.0.0.3"},
							{IP: "10.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			}
			ips := collectEndpointIPs(endpoints)
			Expect(ips).To(Equal([]string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}))
		})
	})
})
