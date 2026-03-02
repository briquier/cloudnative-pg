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

package pgbouncer

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	pgBouncerConfig "github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/servicespec"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/hash"
)

// HeadlessServiceSuffix is the suffix appended to the pooler name
// to form the headless service used for peer discovery
const HeadlessServiceSuffix = "-peers"

// HeadlessServiceName returns the name of the headless service for a given pooler
func HeadlessServiceName(pooler *apiv1.Pooler) string {
	return pooler.Name + HeadlessServiceSuffix
}

// HeadlessService creates the headless service specification used by
// PgBouncer pods to discover each other for cancel-request forwarding
// via the [peers] configuration section
func HeadlessService(pooler *apiv1.Pooler, cluster *apiv1.Cluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(pooler),
			Namespace: pooler.Namespace,
			Labels: map[string]string{
				utils.PgbouncerNameLabel:              pooler.Name,
				utils.ClusterLabelName:                cluster.Name,
				utils.PodRoleLabelName:                string(utils.PodRolePooler),
				utils.KubernetesAppLabelName:          utils.AppName,
				utils.KubernetesAppInstanceLabelName:  cluster.Name,
				utils.KubernetesAppComponentLabelName: utils.PoolerComponentName,
				utils.KubernetesAppManagedByLabelName: utils.ManagerName,
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				utils.PgbouncerNameLabel: pooler.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       pgBouncerConfig.PgBouncerPortName,
					Port:       pgBouncerConfig.PgBouncerPort,
					TargetPort: intstr.FromString(pgBouncerConfig.PgBouncerPortName),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			PublishNotReadyAddresses: true,
		},
	}
}

// Service create the specification for the service of
// pgbouncer
func Service(pooler *apiv1.Pooler, cluster *apiv1.Cluster) (*corev1.Service, error) {
	poolerHash, err := hash.ComputeVersionedHash(pooler.Spec, 3)
	if err != nil {
		return nil, err
	}

	serviceTemplate := servicespec.NewFrom(pooler.Spec.ServiceTemplate).
		WithLabel(utils.PgbouncerNameLabel, pooler.Name).
		WithLabel(utils.ClusterLabelName, cluster.Name).
		WithLabel(utils.PodRoleLabelName, string(utils.PodRolePooler)).
		WithLabel(utils.KubernetesAppLabelName, utils.AppName).
		WithLabel(utils.KubernetesAppInstanceLabelName, cluster.Name).
		WithLabel(utils.KubernetesAppComponentLabelName, utils.PoolerComponentName).
		WithLabel(utils.KubernetesAppManagedByLabelName, utils.ManagerName).
		WithAnnotation(utils.PoolerSpecHashAnnotationName, poolerHash).
		WithServiceType(corev1.ServiceTypeClusterIP, false).
		WithServicePortNoOverwrite(&corev1.ServicePort{
			Name:       pgBouncerConfig.PgBouncerPortName,
			Port:       pgBouncerConfig.PgBouncerPort,
			TargetPort: intstr.FromString(pgBouncerConfig.PgBouncerPortName),
			Protocol:   corev1.ProtocolTCP,
		}).
		SetPGBouncerSelector(pooler.Name).
		Build()

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pooler.Name,
			Namespace:   pooler.Namespace,
			Labels:      serviceTemplate.ObjectMeta.Labels,
			Annotations: serviceTemplate.ObjectMeta.Annotations,
		},
		Spec: serviceTemplate.Spec,
	}, nil
}
