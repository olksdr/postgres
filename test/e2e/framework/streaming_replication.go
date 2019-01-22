package framework

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/appscode/kutil/core/v1"
	"github.com/appscode/kutil/tools/exec"
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
			MatchLabels: v1.UpsertMap(map[string]string{
				controller.NodeRole: leader_election.RolePrimary,
			}, postgres.OffshootSelectors()),
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
			MatchLabels: v1.UpsertMap(map[string]string{
				controller.NodeRole: leader_election.RoleReplica,
			}, postgres.OffshootSelectors()),
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
	postgres, err := f.GetPostgres(meta)
	Expect(err).NotTo(HaveOccurred())

	return Eventually(
		func() string {
			pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
					MatchLabels: v1.UpsertMap(map[string]string{
						controller.NodeRole: leader_election.RolePrimary,
					}, postgres.OffshootSelectors()),
				}),
			})
			Expect(err).NotTo(HaveOccurred())
			if len(pods.Items) != 1 {
				return ""
			}
			return pods.Items[0].Name
		},
		time.Minute*10,
		time.Second*5,
	)
}

func (f *Framework) EventuallyLeaderExists(meta metav1.ObjectMeta) GomegaAsyncAssertion {
	postgres, err := f.GetPostgres(meta)
	Expect(err).NotTo(HaveOccurred())

	return Eventually(
		func() bool {
			pods, err := f.kubeClient.CoreV1().Pods(meta.Namespace).List(metav1.ListOptions{
				LabelSelector: metav1.FormatLabelSelector(&metav1.LabelSelector{
					MatchLabels: v1.UpsertMap(map[string]string{
						controller.NodeRole: leader_election.RolePrimary,
					}, postgres.OffshootSelectors()),
				}),
			})
			Expect(err).NotTo(HaveOccurred())
			return len(pods.Items) == 1
		},
		time.Minute*10,
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

// PromotePodToMaster
func (f *Framework) PromotePodToMaster(meta metav1.ObjectMeta, clientPodName string) {
	pod, err := f.kubeClient.CoreV1().Pods(meta.Namespace).Get(clientPodName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	str, err := exec.ExecIntoPod(f.restConfig, pod, "su", "postgres", "-c", "pg_ctl -D ${PGDATA} promote")
	Expect(err).NotTo(HaveOccurred())

	fmt.Println(str)
}

func (f *Framework) EventuallyStreamingReplication(meta metav1.ObjectMeta, clientPodName, dbName, userName string) GomegaAsyncAssertion {
	return Eventually(
		func() int {
			tunnel, err := f.ForwardPort(meta, clientPodName)
			if err != nil {
				return -1
			}
			defer tunnel.Close()

			db, err := f.GetPostgresClient(tunnel, dbName, userName)
			if err != nil {
				return -1
			}
			defer db.Close()

			if err := f.CheckPostgres(db); err != nil {
				return -1
			}

			results, err := db.Query("select * from pg_stat_replication;")
			if err != nil {
				return -1
			}

			for _, result := range results {
				applicationName := string(result["application_name"])
				state := string(result["state"])
				if state != "streaming" || !strings.HasPrefix(applicationName, meta.Name) {
					return -1
				}
			}
			return len(results)
		},
		time.Minute*10,
		time.Second*5,
	)
}
