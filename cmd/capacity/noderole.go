/*
Copyright © 2021 Alex Krzos akrzos@redhat.com

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package capacity

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/akrzos/kubeSize/internal/kube"
	"github.com/akrzos/kubeSize/internal/output"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"
)

var nodeRoleCmd = &cobra.Command{
	Use:     "node-role",
	Aliases: []string{"nr"},
	Short:   "Get cluster capacity grouped by node role",
	Long:    `Get Kubernetes cluster size and capacity metrics grouped by node role`,
	PreRun: func(cmd *cobra.Command, args []string) {
		viper.BindPFlags(cmd.Flags())
		if err := output.ValidateOutput(*cmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {

		clientset, err := kube.CreateClientSet(KubernetesConfigFlags)
		if err != nil {
			return errors.Wrap(err, "failed to create clientset")
		}

		// Fix perf idea:
		// Get nodes, get pods
		// Create map of cluster capacity data mapped to each noderole
		// Create list of node roles, create map of node to node role list
		// iterate through each pod checking the node, appending data for each role in list

		nodes, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to list nodes")
		}

		nodeRoleCapacityData := make(map[string]*output.ClusterCapacityData)
		roleNames := make([]string, 0)

		for _, node := range nodes.Items {

			roles := sets.NewString()
			for labelKey, labelValue := range node.Labels {
				switch {
				case strings.HasPrefix(labelKey, "node-role.kubernetes.io/"):
					if role := strings.TrimPrefix(labelKey, "node-role.kubernetes.io/"); len(role) > 0 {
						roles.Insert(role)
					}
				case labelKey == "kubernetes.io/role" && labelValue != "":
					roles.Insert(labelValue)
				}
			}
			if len(roles) == 0 {
				roles.Insert("<none>")
			}

			nodeFieldSelector, err := fields.ParseSelector("spec.nodeName=" + node.Name)
			if err != nil {
				return errors.Wrap(err, "failed to create fieldSelector")
			}
			nodePodsList, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: nodeFieldSelector.String()})
			totalPodCount := len(nodePodsList.Items)

			nonTerminatedFieldSelector, err := fields.ParseSelector("spec.nodeName=" + node.Name + ",status.phase!=" + string(corev1.PodSucceeded) + ",status.phase!=" + string(corev1.PodFailed))
			if err != nil {
				return errors.Wrap(err, "failed to create fieldSelector")
			}
			totalNonTermPodsList, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: nonTerminatedFieldSelector.String()})
			nonTerminatedPodCount := len(totalNonTermPodsList.Items)

			var totalRequestsCPU, totalLimitssCPU, totalRequestsMemory, totalLimitsMemory resource.Quantity

			for _, pod := range totalNonTermPodsList.Items {
				for _, container := range pod.Spec.Containers {
					totalRequestsCPU.Add(*container.Resources.Requests.Cpu())
					totalLimitssCPU.Add(*container.Resources.Limits.Cpu())
					totalRequestsMemory.Add(*container.Resources.Requests.Memory())
					totalLimitsMemory.Add(*container.Resources.Limits.Memory())
				}
			}

			for role := range roles {
				if nodeRoleData, ok := nodeRoleCapacityData[role]; ok {
					nodeRoleData.TotalNodeCount++
					for _, condition := range node.Status.Conditions {
						if (condition.Type == "Ready") && condition.Status == corev1.ConditionTrue {
							nodeRoleData.TotalReadyNodeCount++
						}
					}
					nodeRoleData.TotalUnreadyNodeCount = nodeRoleData.TotalNodeCount - nodeRoleData.TotalReadyNodeCount
					if node.Spec.Unschedulable {
						nodeRoleData.TotalUnschedulableNodeCount++
					}
					nodeRoleData.TotalCapacityPods.Add(*node.Status.Capacity.Pods())
					nodeRoleData.TotalCapacityCPU.Add(*node.Status.Capacity.Cpu())
					nodeRoleData.TotalCapacityMemory.Add(*node.Status.Capacity.Memory())
					nodeRoleData.TotalAllocatablePods.Add(*node.Status.Allocatable.Pods())
					nodeRoleData.TotalAllocatableCPU.Add(*node.Status.Allocatable.Cpu())
					nodeRoleData.TotalAllocatableMemory.Add(*node.Status.Allocatable.Memory())
					nodeRoleData.TotalRequestsCPU.Add(totalRequestsCPU)
					nodeRoleData.TotalLimitsCPU.Add(totalLimitssCPU)
					nodeRoleData.TotalRequestsMemory.Add(totalRequestsMemory)
					nodeRoleData.TotalLimitsMemory.Add(totalLimitsMemory)
					nodeRoleData.TotalPodCount += totalPodCount
					nodeRoleData.TotalNonTermPodCount += nonTerminatedPodCount
				} else {
					roleNames = append(roleNames, role)
					newNodeRoleCapacityData := new(output.ClusterCapacityData)
					newNodeRoleCapacityData.TotalNodeCount = 1
					for _, condition := range node.Status.Conditions {
						if (condition.Type == "Ready") && condition.Status == corev1.ConditionTrue {
							newNodeRoleCapacityData.TotalReadyNodeCount = 1
							newNodeRoleCapacityData.TotalUnreadyNodeCount = 0
						}
					}
					if node.Spec.Unschedulable {
						newNodeRoleCapacityData.TotalUnschedulableNodeCount = 1
					}
					newNodeRoleCapacityData.TotalCapacityPods.Add(*node.Status.Capacity.Pods())
					newNodeRoleCapacityData.TotalCapacityCPU.Add(*node.Status.Capacity.Cpu())
					newNodeRoleCapacityData.TotalCapacityMemory.Add(*node.Status.Capacity.Memory())
					newNodeRoleCapacityData.TotalAllocatablePods.Add(*node.Status.Allocatable.Pods())
					newNodeRoleCapacityData.TotalAllocatableCPU.Add(*node.Status.Allocatable.Cpu())
					newNodeRoleCapacityData.TotalAllocatableMemory.Add(*node.Status.Allocatable.Memory())
					newNodeRoleCapacityData.TotalRequestsCPU.Add(totalRequestsCPU)
					newNodeRoleCapacityData.TotalLimitsCPU.Add(totalLimitssCPU)
					newNodeRoleCapacityData.TotalRequestsMemory.Add(totalRequestsMemory)
					newNodeRoleCapacityData.TotalLimitsMemory.Add(totalLimitsMemory)
					newNodeRoleCapacityData.TotalPodCount += totalPodCount
					newNodeRoleCapacityData.TotalNonTermPodCount += nonTerminatedPodCount
					nodeRoleCapacityData[role] = newNodeRoleCapacityData
				}
			}

		}

		for _, role := range roleNames {
			nodeRoleCapacityData[role].TotalAvailablePods = int(nodeRoleCapacityData[role].TotalAllocatablePods.Value()) - nodeRoleCapacityData[role].TotalNonTermPodCount
			nodeRoleCapacityData[role].TotalAvailableCPU = nodeRoleCapacityData[role].TotalAllocatableCPU
			nodeRoleCapacityData[role].TotalAvailableCPU.Sub(nodeRoleCapacityData[role].TotalRequestsCPU)
			nodeRoleCapacityData[role].TotalAvailableMemory = nodeRoleCapacityData[role].TotalAllocatableMemory
			nodeRoleCapacityData[role].TotalAvailableMemory.Sub(nodeRoleCapacityData[role].TotalRequestsMemory)
		}

		displayDefault, _ := cmd.Flags().GetBool("default-format")

		displayNoHeaders, _ := cmd.Flags().GetBool("no-headers")

		displayFormat, _ := cmd.Flags().GetString("output")

		sort.Strings(roleNames)

		output.DisplayNodeRoleData(nodeRoleCapacityData, roleNames, displayDefault, displayNoHeaders, displayFormat)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(nodeRoleCmd)
}
