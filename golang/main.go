package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Server struct {
	K8sClientSet *kubernetes.Clientset
}

type DeploymentInfo struct {
	Name          string `json:"deployment_name"`
	RequestedPods int32  `json:"requested_pods"`
	ReadyPods     int32  `json:"ready_pods"`
}

type ClusterDeploymentsInfo struct {
	ReadyDeployments  []DeploymentInfo `json:ready_deployments`
	FailedDeployments []DeploymentInfo `json:failed_deployments`
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	version, err := getKubernetesVersion(clientset)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connected to Kubernetes %s\n", version)
	server := Server{
		K8sClientSet: clientset,
	}
	//getDeploymentsHealth(clientset)
	if err := startServer(*listenAddr, server); err != nil {
		panic(err)
	}
}

// getKubernetesVersion returns a string GitVersion of the Kubernetes server defined by the clientset.
//
// If it can't connect an error will be returned, which makes it useful to check connectivity.
func getKubernetesVersion(clientset kubernetes.Interface) (string, error) {
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}

	return version.String(), nil
}

// startServer launches an HTTP server with defined handlers and blocks until it's terminated or fails with an error.
//
// Expects a listenAddr to bind to.
func startServer(listenAddr string, server Server) error {
	http.HandleFunc("/healthz", healthHandler)
	http.HandleFunc("/clusterdeploymentsinfo", server.clusterDeploymentsInfo)

	fmt.Printf("Server listening on %s\n", listenAddr)

	return http.ListenAndServe(listenAddr, nil)
}

// healthHandler responds with the health status of the application.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)

	_, err := w.Write([]byte("ok"))
	if err != nil {
		fmt.Println("failed writing to response")
	}
}

// Cluster Deployments Info returns the status of each deployment of the cluster
func (s *Server) clusterDeploymentsInfo(w http.ResponseWriter, r *http.Request) {

	clusterDeploymentsInfo, err := getDeploymentsHealth(s.K8sClientSet)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("Internal Server Error"))
		if err != nil {
			fmt.Println("failed writing to response")
		}
	}
	w.WriteHeader(http.StatusOK)

	err = json.NewEncoder(w).Encode(clusterDeploymentsInfo)
	if err != nil {
		fmt.Println("failed writing to response")
	}
}

// Lists Deployments Health
func getDeploymentsHealth(clientset kubernetes.Interface) (*ClusterDeploymentsInfo, error) {
	// List deployments in all namespaces
	deploymentsClient := clientset.AppsV1().Deployments(metav1.NamespaceAll)
	deployments, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	clusterInfo := new(ClusterDeploymentsInfo)

	fmt.Printf("Found %d deployments:\n", len(deployments.Items))
	for _, deployment := range deployments.Items {
		currentDeploymentInfo := DeploymentInfo{
			Name:          deployment.Name,
			RequestedPods: *deployment.Spec.Replicas,
			ReadyPods:     deployment.Status.ReadyReplicas,
		}
		fmt.Printf("%+v\n", currentDeploymentInfo)
		if *deployment.Spec.Replicas <= deployment.Status.ReadyReplicas {
			clusterInfo.ReadyDeployments = append(clusterInfo.ReadyDeployments, currentDeploymentInfo)
		} else {
			clusterInfo.FailedDeployments = append(clusterInfo.FailedDeployments, currentDeploymentInfo)
		}
	}
	fmt.Printf("%+v\n", clusterInfo)
	return clusterInfo, nil
}
