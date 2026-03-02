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
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"sort"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	pgbouncerSpecs "github.com/cloudnative-pg/cloudnative-pg/pkg/specs/pgbouncer"
)

// hashToPeerID maps a string to a candidate peer_id in [1, config.MaxPeerID].
// Exposed as a variable so tests can inject a stub to force collisions.
var hashToPeerID = defaultHashToPeerID

func defaultHashToPeerID(s string) int {
	h := fnv.New32a()
	fmt.Fprint(h, s)
	return int(h.Sum32()%config.MaxPeerID) + 1
}

// assignPeerIDs assigns a unique peer_id in [1, config.MaxPeerID] to every IP.
// Algorithm: sort IPs for determinism, then for each IP assign hash(ip); on
// collision re-hash with "<ip>#1", "<ip>#2", … until an unused ID is found.
// Returns (nil, error) if a unique ID cannot be assigned (e.g. probe exhausted),
// so the caller can disable peering instead of pushing an invalid config.
func assignPeerIDs(ips []string, logger log.Logger) (map[string]int, error) {
	sorted := make([]string, len(ips))
	copy(sorted, ips)
	sort.Strings(sorted)

	used := make(map[int]string, len(sorted))
	out := make(map[string]int, len(sorted))

	for _, ip := range sorted {
		id := hashToPeerID(ip)
		if other, ok := used[id]; ok && other != ip {
			probe := 1
			for {
				candidate := hashToPeerID(fmt.Sprintf("%s#%d", ip, probe))
				if _, taken := used[candidate]; !taken {
					logger.Info("peer_id collision resolved",
						"peer", ip, "collides_with", other,
						"initial_id", id, "new_id", candidate, "probe", probe)
					id = candidate
					break
				}
				probe++
				if probe > config.MaxPeerID {
					logger.Error(nil, "unable to assign unique peer_id, disabling peering", "peer", ip)
					return nil, fmt.Errorf("peer_id exhaustion for %s", ip)
				}
			}
		}
		used[id] = ip
		out[ip] = id
	}
	return out, nil
}

// getPeeringInfo discovers peer PgBouncer instances via the headless service
// Endpoints and builds PeeringInfo for the configuration.
// Returns nil when peering cannot be determined (e.g. Endpoints not yet available).
func (r *PgBouncerReconciler) getPeeringInfo(ctx context.Context) *config.PeeringInfo {
	contextLogger := log.FromContext(ctx)

	localIP, err := getLocalPodIP()
	if err != nil {
		contextLogger.Info("Cannot determine local pod IP, skipping peering (ensure POD_IP env is set)", "error", err)
		return nil
	}

	headlessServiceName := r.poolerNamespacedName.Name + pgbouncerSpecs.HeadlessServiceSuffix

	var endpoints corev1.Endpoints
	err = r.client.Get(ctx, types.NamespacedName{
		Name:      headlessServiceName,
		Namespace: r.poolerNamespacedName.Namespace,
	}, &endpoints)
	if err != nil {
		contextLogger.Info("Cannot get headless service endpoints, skipping peering",
			"service", headlessServiceName, "error", err)
		return nil
	}

	allIPs := collectEndpointIPs(&endpoints)
	if len(allIPs) <= 1 {
		contextLogger.Info("Skipping peering: need more than one peer", "endpointIPs", len(allIPs))
		return nil
	}

	idMap, err := assignPeerIDs(allIPs, contextLogger)
	if err != nil {
		contextLogger.Error(err, "Peer ID assignment failed, skipping peering")
		return nil
	}

	localPeerID, ok := idMap[localIP]
	if !ok {
		contextLogger.Info("Local IP not found in endpoints, skipping peering", "localIP", localIP)
		return nil
	}

	// Include ALL peers (including the local pod) in the [peers] list.
	// PgBouncer explicitly allows the local peer_id in [peers] and ignores it,
	// but having it prevents "dropping peer  as it does not exist anymore"
	// warnings on RELOAD when PgBouncer's internal self-entry is missing
	// from the config. See https://www.pgbouncer.org/config.html#section-peers
	var peers []config.PeerInfo
	for _, ip := range allIPs {
		if ip == "" {
			continue
		}
		id := idMap[ip]
		if id < 1 || id > config.MaxPeerID {
			continue
		}
		peers = append(peers, config.PeerInfo{
			ID:   id,
			Host: ip,
			Port: config.PgBouncerPort,
		})
	}

	if len(peers) <= 1 {
		return nil
	}

	sort.Slice(peers, func(i, j int) bool {
		return peers[i].ID < peers[j].ID
	})

	return &config.PeeringInfo{
		PeerID: localPeerID,
		Peers:  peers,
	}
}

// collectEndpointIPs extracts all ready and not-ready IP addresses from Endpoints subsets
func collectEndpointIPs(endpoints *corev1.Endpoints) []string {
	seen := make(map[string]struct{})
	var ips []string

	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			if _, ok := seen[addr.IP]; !ok {
				seen[addr.IP] = struct{}{}
				ips = append(ips, addr.IP)
			}
		}
		for _, addr := range subset.NotReadyAddresses {
			if _, ok := seen[addr.IP]; !ok {
				seen[addr.IP] = struct{}{}
				ips = append(ips, addr.IP)
			}
		}
	}

	sort.Strings(ips)
	return ips
}

// getLocalPodIP determines this pod's IP address
func getLocalPodIP() (string, error) {
	if podIP := os.Getenv("POD_IP"); podIP != "" {
		return podIP, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("while getting hostname: %w", err)
	}

	addrs, err := net.LookupHost(hostname)
	if err != nil {
		return "", fmt.Errorf("while resolving hostname %q: %w", hostname, err)
	}

	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses found for hostname %q", hostname)
	}

	return addrs[0], nil
}
