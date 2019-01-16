package framework

import (
	"fmt"
	"time"

	"github.com/kubedb/postgres/pkg/controller"
	"github.com/kubedb/postgres/pkg/leader_election"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

func (f *Framework) GetPrimaryPodName(meta metav1.ObjectMeta) string {
	postgres, err := f.GetPostgres(meta)
	Expect(err).NotTo(HaveOccurred())
	Expect(postgres.Spec.Replicas).NotTo(BeNil())

	if *postgres.Spec.Replicas == 1 {
		return fmt.Sprintf("%v-0", postgres.Name)
	}

	pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				controller.NodeRole: leader_election.RolePrimary,
			},
		}),
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(len(pods.Items)).To(Equal(1))

	return pods.Items[0].Name
}

func (f *Framework) GetArbitraryStandbyPodName(meta metav1.ObjectMeta) string {
	postgres, err := f.GetPostgres(meta)
	Expect(err).NotTo(HaveOccurred())
	Expect(postgres.Spec.Replicas).NotTo(BeNil())

	if *postgres.Spec.Replicas == 1 {
		return ""
	}

	pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: map[string]string{
				controller.NodeRole: leader_election.RolePrimary,
			},
		}),
	})
	Expect(err).NotTo(HaveOccurred())

	return pods.Items[0].Name
}

func (f *Framework) MakeNewLeaderManually(meta metav1.ObjectMeta, newLeaderPodName string) {
	configMapLock := resourcelock.ConfigMapLock{
		Client: f.kubeClient.CoreV1(),
		ConfigMapMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%v-leader-lock", meta.Name),
			Namespace: meta.Namespace,
		},
	}

	// LeaderElectionRecord
	ler, err := configMapLock.Get()
	Expect(err).NotTo(HaveOccurred())

	ler.HolderIdentity = newLeaderPodName
	err = configMapLock.Update(*ler)
	Expect(err).NotTo(HaveOccurred())
}

func (f *Framework) EventuallyLeader(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() string {
			pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						controller.NodeRole: leader_election.RolePrimary,
					},
				}),
			})
			Expect(err).NotTo(HaveOccurred())
			if len(pods.Items) != 1 {
				return ""
			}
			return pods.Items[0].Name
		},
		time.Minute*5,
		time.Second*5,
	)
}

func (f *Framework) EventuallyLeaderExists(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{
						controller.NodeRole: leader_election.RolePrimary,
					},
				}),
			})
			Expect(err).NotTo(HaveOccurred())
			return len(pods.Items) == 1
		},
		time.Minute*5,
		time.Second*5,
	)
}

// CountFromAllPods only works for Hot-Standby
func (f *Framework) CountFromAllPods(meta metav1.ObjectMeta, dbName, dbUser string, count int) {

	postgres, err := f.GetPostgres(meta)
	Expect(err).NotTo(HaveOccurred())

	for i := int32(0); i < *postgres.Spec.Replicas; i++ {
		f.EventuallyCountTable(
			postgres.ObjectMeta,
			fmt.Sprintf("%v-%v", meta.Name, i),
			dbName, dbUser,
		).Should(Equal(count))
	}
}
