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

	"github.com/akrzos/kubeSize/internal/kube"
	"github.com/akrzos/kubeSize/internal/output"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

var clusterCmd = &cobra.Command{
	Use:     "cluster",
	Aliases: []string{"c"},
	Short:   "Get cluster size and capacity",
	Long:    `Get Kubernetes cluster size and capacity metrics`,
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

		nodes, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to list nodes")
		}

		clusterCapacityData := new(output.ClusterCapacityData)

		for _, node := range nodes.Items {
			clusterCapacityData.TotalNodeCount++
			for _, condition := range node.Status.Conditions {
				if (condition.Type == "Ready") && condition.Status == corev1.ConditionTrue {
					clusterCapacityData.TotalReadyNodeCount++
				}
			}
			if node.Spec.Unschedulable {
				clusterCapacityData.TotalUnschedulableNodeCount++
			}
			clusterCapacityData.TotalCapacityPods.Add(*node.Status.Capacity.Pods())
			clusterCapacityData.TotalCapacityCPU.Add(*node.Status.Capacity.Cpu())
			clusterCapacityData.TotalCapacityMemory.Add(*node.Status.Capacity.Memory())
			clusterCapacityData.TotalAllocatablePods.Add(*node.Status.Allocatable.Pods())
			clusterCapacityData.TotalAllocatableCPU.Add(*node.Status.Allocatable.Cpu())
			clusterCapacityData.TotalAllocatableMemory.Add(*node.Status.Allocatable.Memory())
		}
		clusterCapacityData.TotalUnreadyNodeCount = clusterCapacityData.TotalNodeCount - clusterCapacityData.TotalReadyNodeCount

		totalPodsList, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
		clusterCapacityData.TotalPodCount = len(totalPodsList.Items)

		// Note you can have non-terminated pod not assigned to a node (Ex Pending) thus cluster vs node/node-role counts can differ
		fieldSelector, err := fields.ParseSelector("status.phase!=" + string(corev1.PodSucceeded) + ",status.phase!=" + string(corev1.PodFailed))
		if err != nil {
			return errors.Wrap(err, "failed to create fieldSelector")
		}
		totalNonTermPodsList, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{FieldSelector: fieldSelector.String()})
		clusterCapacityData.TotalNonTermPodCount = len(totalNonTermPodsList.Items)

		for _, pod := range totalNonTermPodsList.Items {
			for _, container := range pod.Spec.Containers {
				clusterCapacityData.TotalRequestsCPU.Add(*container.Resources.Requests.Cpu())
				clusterCapacityData.TotalLimitsCPU.Add(*container.Resources.Limits.Cpu())
				clusterCapacityData.TotalRequestsMemory.Add(*container.Resources.Requests.Memory())
				clusterCapacityData.TotalLimitsMemory.Add(*container.Resources.Limits.Memory())
			}
		}

		displayReadable, _ := cmd.Flags().GetBool("readable")

		displayFormat, _ := cmd.Flags().GetString("output")

		output.DisplayClusterData(*clusterCapacityData, displayReadable, displayFormat)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(clusterCmd)
}