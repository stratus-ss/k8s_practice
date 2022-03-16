package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Generate a random uuid to attach to the pod name
// so that this can be called multiple times without conflicting with previous runs
func randomString(length int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)[:length]
}

// Creates a debug pod from the nodeName passed in
// Pod is based on the ose-cli pod and runs an etcd backup
// in the future may take namespace and other arguments to make this more flexible
func createBackupPod(nodeName string, projectName string, imageURL string, pvcName string) *batchv1.Job {
	// this command should be run as a prefix to all commands in the debug pod
	cmd := "oc debug node/" + nodeName + " -- chroot /host"
	// create a temporary tarball which will eventually be moved to the pod's PVC
	tempTarball := "/tmp/etcd_backup.tar.gz"
	tempBackupDir := "/tmp/assets/backup"
	// generate a random UUID for the job name
	randomUUID := randomString(4)
	backupCMD := cmd + " /usr/local/bin/cluster-backup.sh " + tempBackupDir
	tarCMD := cmd + " tar czf " + tempTarball + " " + tempBackupDir
	// using cat to stream the tarball from one host to another is one way to transfer without mounting
	// any mounts on the debug host
	moveTarballCMD := cmd + " cat " + tempTarball + " > /backups/backup_$(date +%Y-%m-%d_%H-%M_%Z).db.tgz"
	cleanupCMD := cmd + " rm -rf " + tempBackupDir + " && " + cmd + " rm -f " + tempTarball
	jobSpec := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "etcd-backup-" + randomUUID,
			Namespace: projectName,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "etcd-backup-" + randomUUID,
							Image:           imageURL,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command: []string{
								"/bin/bash",
								"-c",
								backupCMD + " && " + tarCMD + " && " + moveTarballCMD + " && " + cleanupCMD,
							},
							VolumeMounts: []corev1.VolumeMount{
								corev1.VolumeMount{
									Name:      "etcd-backup-mount",
									MountPath: "/backups",
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						{
							Name: "etcd-backup-mount",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
					NodeSelector: map[string]string{
						"node-role.kubernetes.io/master": "",
					},
				},
			},
		},
	}

	return (jobSpec)
}

func main() {
	var ns, label, field string
	flag.StringVar(&ns, "namespace", "", "namespace")
	flag.StringVar(&label, "l", "", "Label selector")
	flag.StringVar(&field, "f", "", "Field selector")
	imageURL := "registry.redhat.io/openshift4/ose-cli:v4.8"
	backupProject := "ocp-etcd-backup"
	pvcName := "etcd-backup-pvc"
	// This is a temporary holder until I find a better way to pass in this config
	config, err := clientcmd.BuildConfigFromFlags("", "/home/stratus/temp/go_practice/k8s_practice/auth/kubeconfig")

	if err != nil {
		panic(err)
	}

	client, _ := kubernetes.NewForConfig(config)
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/master="})

	if err != nil {
		fmt.Println(err)
		return
	}
	// It should be safe to assume that at least 1 item exists since the above error should have exited the program
	// if no results were found
	debug_node := nodes.Items[0].Name
	backupJob := createBackupPod(debug_node, backupProject, imageURL, pvcName)
	_, err1 := client.BatchV1().Jobs(backupProject).Create(context.TODO(), backupJob, metav1.CreateOptions{})

	if err1 != nil {
		panic(err1)
	}

}
