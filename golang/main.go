package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	v3 "github.com/projectcalico/api/pkg/apis/projectcalico/v3"
	"github.com/projectcalico/api/pkg/client/clientset_generated/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Server struct {
	K8sClientSet    *kubernetes.Clientset
	CalicoClientSet *clientset.Clientset
}

type DeploymentInfo struct {
	Name          string `json:"deployment_name"`
	RequestedPods int32  `json:"requested_pods"`
	ReadyPods     int32  `json:"ready_pods"`
}

type ClusterDeploymentsInfo struct {
	ReadyDeployments  []DeploymentInfo `json:"ready_deployments"`
	FailedDeployments []DeploymentInfo `json:"failed_deployments"`
}

type DenyNetworkRequestWorkload struct {
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels"`
}

type DenyNetworkRequest struct {
	A DenyNetworkRequestWorkload `json:"workload_a"`
	B DenyNetworkRequestWorkload `json:"workload_b"`
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientsetVanilla, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	clientsetCalico, err := clientset.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	version, err := getKubernetesVersion(clientsetVanilla)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Connected to Kubernetes %s\n", version)
	server := Server{
		K8sClientSet:    clientsetVanilla,
		CalicoClientSet: clientsetCalico,
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
	http.HandleFunc("/clusterdeploymentsinfo", server.clusterDeploymentsInfoHandler)
	http.HandleFunc("/denyNetworkPolicy", server.denyNetworkPolicyHandler)

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
func (s *Server) clusterDeploymentsInfoHandler(w http.ResponseWriter, r *http.Request) {

	clusterDeploymentsInfo, err := getDeploymentsHealth(s.K8sClientSet)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

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
		return nil, err
	}

	clusterInfo := new(ClusterDeploymentsInfo)

	for _, deployment := range deployments.Items {
		currentDeploymentInfo := DeploymentInfo{
			Name:          deployment.Name,
			RequestedPods: *deployment.Spec.Replicas,
			ReadyPods:     deployment.Status.ReadyReplicas,
		}

		if *deployment.Spec.Replicas <= deployment.Status.ReadyReplicas {
			clusterInfo.ReadyDeployments = append(clusterInfo.ReadyDeployments, currentDeploymentInfo)
		} else {
			clusterInfo.FailedDeployments = append(clusterInfo.FailedDeployments, currentDeploymentInfo)
		}
	}
	fmt.Printf("%+v\n", clusterInfo)
	return clusterInfo, nil
}

// Handler to create deny policy based on post request
func (s *Server) denyNetworkPolicyHandler(w http.ResponseWriter, r *http.Request) {
	// Check if the request method is POST
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}

	defer r.Body.Close()

	var denyNetworkRequest DenyNetworkRequest
	err = json.Unmarshal(body, &denyNetworkRequest)
	if err != nil {
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		return
	}

	n, err := createDenyNetworkPolicy(s.K8sClientSet, *s.CalicoClientSet, denyNetworkRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(n))
	if err != nil {
		fmt.Println("failed writing to response")
	}
}

// Render string map to Calico selector string
func renderMap(m map[string]string) string {
	var sb strings.Builder
	first := true
	for key, value := range m {
		if !first {
			sb.WriteString(" && ")
		}
		sb.WriteString(fmt.Sprintf("%s == '%s'", key, value))
		first = false
	}
	return sb.String()
}

// Creates Network Policy to stop connections between two workloads by label and namespace
func createDenyNetworkPolicy(clientset kubernetes.Interface, calicoClientset clientset.Clientset, requestdetails DenyNetworkRequest) (string, error) {
	namespaceB, err := clientset.CoreV1().Namespaces().Get(context.TODO(), requestdetails.B.Namespace, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	fmt.Println(renderMap(requestdetails.B.Labels))
	fmt.Println(renderMap(namespaceB.Labels))
	networkPolicy := &v3.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("deny-network-policy-%s", uuid.New().String()),
			Namespace: requestdetails.A.Namespace,
		},
		Spec: v3.NetworkPolicySpec{
			Selector: renderMap(requestdetails.A.Labels),
			Ingress: []v3.Rule{{
				Action: v3.Deny,
				Source: v3.EntityRule{
					Selector:          renderMap(requestdetails.B.Labels),
					NamespaceSelector: renderMap(namespaceB.Labels),
				},
			}},
			Egress: []v3.Rule{{
				Action: v3.Deny,
				Destination: v3.EntityRule{
					Selector:          renderMap(requestdetails.B.Labels),
					NamespaceSelector: renderMap(namespaceB.Labels),
				},
			}},
		},
	}

	n, err := calicoClientset.ProjectcalicoV3().NetworkPolicies(requestdetails.A.Namespace).Create(context.TODO(), networkPolicy, metav1.CreateOptions{})
	if err != nil {
		fmt.Println("Error in here" + err.Error())
		return "", err
	}

	fmt.Println("NetworkPolicy created with name:", networkPolicy.Name)
	return n.Name, nil
}
