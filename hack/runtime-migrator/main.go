package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardener_types "github.com/gardener/gardener/pkg/client/core/clientset/versioned/typed/core/v1beta1"
	v1 "github.com/kyma-project/infrastructure-manager/api/v1"
	migrator "github.com/kyma-project/infrastructure-manager/hack/runtime-migrator/internal"
	"github.com/kyma-project/infrastructure-manager/internal/gardener"
	"github.com/kyma-project/infrastructure-manager/internal/gardener/kubeconfig"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	migratorLabel                      = "operator.kyma-project.io/created-by-migrator"
	expirationTime                     = 60 * time.Minute
	ShootNetworkingFilterExtensionType = "shoot-networking-filter"
	runtimeCrFullPath                  = "%sshoot-%s.yaml"
	runtimeIDAnnotation                = "kcp.provisioner.kyma-project.io/runtime-id"
)

func main() {
	cfg := migrator.NewConfig()

	runtimeIDs := getRuntimeIDsFromStdin(cfg)
	gardenerNamespace := fmt.Sprintf("garden-%s", cfg.GardenerProjectName)
	list := getShootList(cfg, gardenerNamespace)
	provider, err := setupKubernetesKubeconfigProvider(cfg.GardenerKubeconfigPath, gardenerNamespace, expirationTime)
	if err != nil {
		log.Fatal("failed to create kubeconfig provider - ", err)
	}

	kcpClient, err := migrator.CreateKcpClient(&cfg)
	if err != nil {
		log.Fatal("failed to create kcp client - ", kcpClient)
	}

	results := make([]migrator.MigrationResult, 0)
	for _, shoot := range list.Items {
		if cfg.InputType == migrator.InputTypeJSON {
			var shootRuntimeID = shoot.Annotations[runtimeIDAnnotation]
			if !slices.Contains(runtimeIDs, shootRuntimeID) {
				log.Printf("Skipping shoot %s, as it is not in the list of runtime IDs\n", shoot.Name)
				appendToResults(&results,
					migrator.MigrationResult{
						RuntimeID: shootRuntimeID,
						ShootName: shoot.Name,
						Status:    migrator.StatusRuntimeIDNotFound})
				continue
			}
			log.Printf("Shoot %s found, continuing with creation of Runtime CR \n", shoot.Name)
		}

		invalidShoot, validationErr := validateShoot(shoot)
		if validationErr != nil {
			results = append(results, invalidShoot)
			log.Print(validationErr.Error())
			continue
		}
		runtime := createRuntime(shoot, cfg, provider)
		err := saveRuntime(cfg, runtime, kcpClient)
		if err != nil {
			log.Printf("Failed to create runtime CR, %s\n", err)

			status := migrator.StatusError
			if k8serrors.IsAlreadyExists(err) {
				status = migrator.StatusAlreadyExists
			}
			results = append(results, migrator.MigrationResult{
				RuntimeID:    shoot.Annotations[runtimeIDAnnotation],
				ShootName:    shoot.Name,
				Status:       status,
				ErrorMessage: err.Error(),
			})
			continue
		}

		shootAsYaml, err := getYamlSpec(runtime)
		if err != nil {
			log.Printf("Failed to converte spec to yaml, %s", err)
		}
		writeSpecToFile(cfg.OutputPath, shoot, shootAsYaml)

		results = append(results, migrator.MigrationResult{
			RuntimeID:    shoot.Annotations[runtimeIDAnnotation],
			ShootName:    shoot.Name,
			Status:       migrator.StatusSuccess,
			PathToCRYaml: fmt.Sprintf(runtimeCrFullPath, cfg.OutputPath, shoot.Name),
		})
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(&results); err != nil {
		log.Printf("Failed to convert migration results to json, %s", err)
	}
}

func validateShoot(shoot v1beta1.Shoot) (migrator.MigrationResult, error) {
	if shoot.Spec.Networking.Pods == nil {
		return migrator.MigrationResult{
			RuntimeID:    shoot.Annotations[runtimeIDAnnotation],
			ShootName:    shoot.Name,
			Status:       migrator.StatusError,
			ErrorMessage: "Shoot networking pods is nil",
		}, fmt.Errorf("Shoot networking pods is nil")
	}
	return migrator.MigrationResult{}, nil
}

func appendToResults(list *[]migrator.MigrationResult, result migrator.MigrationResult) {
	*list = append(*list, result)
}

func getRuntimeIDsFromStdin(cfg migrator.Config) []string {
	var runtimeIDs []string
	if cfg.InputType == migrator.InputTypeJSON {
		decoder := json.NewDecoder(os.Stdin)

		if err := decoder.Decode(&runtimeIDs); err != nil {
			log.Printf("Could not load list of RuntimeIds - %s", err)
		}
	}
	return runtimeIDs
}

func saveRuntime(cfg migrator.Config, runtime v1.Runtime, getClient client.Client) error {
	if !cfg.IsDryRun {
		err := getClient.Create(context.Background(), &runtime)

		if err != nil {
			return err
		}

		log.Printf("Runtime CR %s created\n", runtime.Name)
	}
	return nil
}

func createRuntime(shoot v1beta1.Shoot, cfg migrator.Config, provider kubeconfig.Provider) v1.Runtime {
	var subjects = getAdministratorsList(provider, shoot.Name)
	var hAFailureToleranceType = getFailureToleranceType(shoot)
	var licenceType = shoot.Annotations["kcp.provisioner.kyma-project.io/licence-type"]

	var isShootNetworkFilteringEnabled = checkIfShootNetworkFilteringEnabled(shoot)

	var runtime = v1.Runtime{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Runtime",
			APIVersion: "infrastructuremanager.kyma-project.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:                       shoot.Name,
			GenerateName:               shoot.GenerateName,
			Namespace:                  "kcp-system",
			DeletionTimestamp:          shoot.DeletionTimestamp,
			DeletionGracePeriodSeconds: shoot.DeletionGracePeriodSeconds,
			Labels:                     getAllRuntimeLabels(shoot, cfg.Client),
			Annotations:                shoot.Annotations,
			OwnerReferences:            nil, // deliberately left empty, as without that we will not be able to delete Runtime CRs
			Finalizers:                 nil, // deliberately left empty, as without that we will not be able to delete Runtime CRs
			ManagedFields:              nil, // deliberately left empty "This is mostly for migrator housekeeping, and users typically shouldn't need to set or understand this field."
		},
		Spec: v1.RuntimeSpec{
			Shoot: v1.RuntimeShoot{
				Name:              shoot.Name,
				Purpose:           *shoot.Spec.Purpose,
				Region:            shoot.Spec.Region,
				LicenceType:       &licenceType,
				SecretBindingName: *shoot.Spec.SecretBindingName,
				Kubernetes: v1.Kubernetes{
					Version: &shoot.Spec.Kubernetes.Version,
					KubeAPIServer: v1.APIServer{
						OidcConfig: v1beta1.OIDCConfig{
							CABundle:             nil, // deliberately left empty
							ClientAuthentication: shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.ClientAuthentication,
							ClientID:             shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.ClientID,
							GroupsClaim:          shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.GroupsClaim,
							GroupsPrefix:         shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.GroupsPrefix,
							IssuerURL:            shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.IssuerURL,
							RequiredClaims:       shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.RequiredClaims,
							SigningAlgs:          shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.SigningAlgs,
							UsernameClaim:        shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.UsernameClaim,
							UsernamePrefix:       shoot.Spec.Kubernetes.KubeAPIServer.OIDCConfig.UsernamePrefix,
						},
						AdditionalOidcConfig: nil, // deliberately left empty for now
					},
				},
				Provider: v1.Provider{
					Type: shoot.Spec.Provider.Type,
					ControlPlaneConfig: runtime.RawExtension{
						Raw:    shoot.Spec.Provider.ControlPlaneConfig.Raw,
						Object: shoot.Spec.Provider.ControlPlaneConfig.Object,
					},
					InfrastructureConfig: runtime.RawExtension{
						Raw:    shoot.Spec.Provider.InfrastructureConfig.Raw,
						Object: shoot.Spec.Provider.InfrastructureConfig.Object,
					},
					Workers: shoot.Spec.Provider.Workers,
				},
				Networking: v1.Networking{
					Pods:     *shoot.Spec.Networking.Pods,
					Nodes:    *shoot.Spec.Networking.Nodes,
					Services: *shoot.Spec.Networking.Services,
				},
				ControlPlane: v1beta1.ControlPlane{
					HighAvailability: &v1beta1.HighAvailability{
						FailureTolerance: v1beta1.FailureTolerance{
							Type: hAFailureToleranceType,
						},
					},
				},
			},
			Security: v1.Security{
				Administrators: subjects,
				Networking: v1.NetworkingSecurity{
					Filter: v1.Filter{
						Ingress: &v1.Ingress{
							// deliberately left empty for now, as it was a feature implemented in the Provisioner
						},
						Egress: v1.Egress{
							Enabled: isShootNetworkFilteringEnabled,
						},
					},
				},
			},
		},
		Status: v1.RuntimeStatus{
			State:      "",  // deliberately left empty by our migrator to show that controller has not picked it yet
			Conditions: nil, // deliberately left nil by our migrator to show that controller has not picked it yet
		},
	}
	return runtime
}

func checkIfShootNetworkFilteringEnabled(shoot v1beta1.Shoot) bool {
	for _, extension := range shoot.Spec.Extensions {
		if extension.Type == ShootNetworkingFilterExtensionType {
			return !(*extension.Disabled)
		}
	}
	return false
}

func getShootList(cfg migrator.Config, gardenerNamespace string) *v1beta1.ShootList {
	gardenerShootClient := setupGardenerShootClient(cfg.GardenerKubeconfigPath, gardenerNamespace)
	list, err := gardenerShootClient.List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal("Failed to retrieve shoots from Gardener - ", err)
	}

	return list
}

func getFailureToleranceType(shoot v1beta1.Shoot) v1beta1.FailureToleranceType {
	if shoot.Spec.ControlPlane != nil {
		if shoot.Spec.ControlPlane.HighAvailability != nil {
			return shoot.Spec.ControlPlane.HighAvailability.FailureTolerance.Type
		}
	}
	return ""
}

func getAdministratorsList(provider kubeconfig.Provider, shootName string) []string {
	var kubeconfig, err = provider.Fetch(context.Background(), shootName)
	if kubeconfig == "" {
		log.Printf("Failed to get dynamic kubeconfig for shoot %s, %s\n", shootName, err.Error())
		return []string{}
	}

	restClientConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		log.Printf("Failed to create REST client from kubeconfig - %s\n", err)
		return []string{}
	}

	clientset, err := kubernetes.NewForConfig(restClientConfig)
	if err != nil {
		log.Printf("Failed to create clientset from restconfig - %s\n", err)
	}

	var clusterRoleBindings, _ = clientset.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{
		LabelSelector: "reconciler.kyma-project.io/managed-by=reconciler,app=kyma",
	})

	var subjects = []string{}
	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		for _, subject := range clusterRoleBinding.Subjects {
			subjects = append(subjects, subject.Name)
		}
	}

	return subjects
}

func getYamlSpec(shoot v1.Runtime) ([]byte, error) {
	shootAsYaml, err := yaml.Marshal(shoot)
	return shootAsYaml, err
}

func writeSpecToFile(outputPath string, shoot v1beta1.Shoot, shootAsYaml []byte) {
	var fileName = fmt.Sprintf(runtimeCrFullPath, outputPath, shoot.Name)

	const writePermissions = 0644
	err := os.WriteFile(fileName, shootAsYaml, writePermissions)

	if err != nil {
		log.Printf("Failed to save %s - %s", runtimeCrFullPath, err)
	}
	log.Printf("Runtime CR has been saved to %s\n", fileName)
}

func setupGardenerShootClient(kubeconfigPath, gardenerNamespace string) gardener_types.ShootInterface {
	restConfig, err := gardener.NewRestConfigFromFile(kubeconfigPath)
	if err != nil {
		return nil
	}

	gardenerClientSet, err := gardener_types.NewForConfig(restConfig)
	if err != nil {
		return nil
	}

	shootClient := gardenerClientSet.Shoots(gardenerNamespace)

	return shootClient
}

func setupKubernetesKubeconfigProvider(kubeconfigPath string, namespace string, expirationTime time.Duration) (kubeconfig.Provider, error) {
	restConfig, err := gardener.NewRestConfigFromFile(kubeconfigPath)
	if err != nil {
		return kubeconfig.Provider{}, err
	}

	gardenerClientSet, err := gardener_types.NewForConfig(restConfig)
	if err != nil {
		return kubeconfig.Provider{}, err
	}

	gardenerClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return kubeconfig.Provider{}, err
	}

	shootClient := gardenerClientSet.Shoots(namespace)
	dynamicKubeconfigAPI := gardenerClient.SubResource("adminkubeconfig")

	err = v1beta1.AddToScheme(gardenerClient.Scheme())
	if err != nil {
		return kubeconfig.Provider{}, errors.Wrap(err, "failed to register Gardener schema")
	}

	return kubeconfig.NewKubeconfigProvider(shootClient,
		dynamicKubeconfigAPI,
		namespace,
		int64(expirationTime.Seconds())), nil
}

func getAllRuntimeLabels(shoot v1beta1.Shoot, getClient migrator.GetClient) map[string]string {
	enrichedRuntimeLabels := map[string]string{}

	// add all labels from the shoot
	for labelKey, labelValue := range shoot.Labels {
		enrichedRuntimeLabels[labelKey] = labelValue
	}

	// add agreed labels from the GardenerCluster CR
	k8sClient, clientErr := getClient()

	if clientErr != nil {
		log.Printf("Failed to get GardenerClient for shoot %s - %s\n", shoot.Name, clientErr)
	}
	gardenerCluster := v1.GardenerCluster{}
	shootKey := types.NamespacedName{Name: "runtime-id", Namespace: "kcp-system"}
	getGardenerCRerr := k8sClient.Get(context.Background(), shootKey, &gardenerCluster)
	if getGardenerCRerr != nil {
		log.Printf("Failed to retrieve GardenerCluster CR for shoot %s - %s\n", shoot.Name, getGardenerCRerr)
	}

	enrichedRuntimeLabels["kyma-project.io/broker-plan-id"] = gardenerCluster.Labels["kyma-project.io/broker-plan-id"]
	enrichedRuntimeLabels["kyma-project.io/runtime-id"] = gardenerCluster.Labels["kyma-project.io/runtime-id"]
	enrichedRuntimeLabels["kyma-project.io/subaccount-id"] = gardenerCluster.Labels["kyma-project.io/subaccount-id"]
	enrichedRuntimeLabels["kyma-project.io/broker-plan-name"] = gardenerCluster.Labels["kyma-project.io/broker-plan-name"]
	enrichedRuntimeLabels["kyma-project.io/global-account-id"] = gardenerCluster.Labels["kyma-project.io/global-account-id"]
	enrichedRuntimeLabels["kyma-project.io/instance-id"] = gardenerCluster.Labels["kyma-project.io/instance-id"]
	enrichedRuntimeLabels["kyma-project.io/region"] = gardenerCluster.Labels["kyma-project.io/region"]
	enrichedRuntimeLabels["kyma-project.io/shoot-name"] = gardenerCluster.Labels["kyma-project.io/shoot-name"]
	enrichedRuntimeLabels["operator.kyma-project.io/kyma-name"] = gardenerCluster.Labels["operator.kyma-project.io/kyma-name"]

	// add custom label for the migrator
	enrichedRuntimeLabels[migratorLabel] = "true"

	return enrichedRuntimeLabels
}
