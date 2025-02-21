package main

import (
	"context"
	"flag"
	"fmt"
	gardener "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	imv1 "github.com/kyma-project/infrastructure-manager/api/v1"
	v1 "github.com/kyma-project/infrastructure-manager/api/v1"
	kimConfig "github.com/kyma-project/infrastructure-manager/pkg/config"
	converter "github.com/kyma-project/infrastructure-manager/pkg/gardener/shoot"
	"io"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
	"strings"
)

func main() {

	var runtimePath string
	var shootPath string
	var kubeconfigPath string
	var outputPath string

	flag.StringVar(&runtimePath, "runtime-path", "", "Path to the runtime CR file.")
	flag.StringVar(&shootPath, "shoot-path", "", "Path to the shoot CR file.")
	flag.StringVar(&kubeconfigPath, "kubeconfig-path", "", "Path to the kubeconfig file.")
	flag.StringVar(&outputPath, "outputPath", "", "Path to the resulting shoot CR file.")
	flag.Parse()

	runtime, err := readRuntimeCRFromFile(runtimePath)
	if err != nil {
		panic(err)
	}

	shoot, err := readShootFromFile(shootPath)
	if err != nil {
		panic(err)
	}

	kcpClient, err := createKcpClient(kubeconfigPath)
	if err != nil {
		panic(err)
	}

	converterConfig, err := getConverterConfig(kcpClient)
	if err != nil {
		panic(err)
	}

	patchConverter := converter.NewConverterPatch(converter.PatchOpts{
		ConverterConfig:      converterConfig,
		Workers:              shoot.Spec.Provider.Workers,
		ShootK8SVersion:      shoot.Spec.Kubernetes.Version,
		Extensions:           shoot.Spec.Extensions,
		Resources:            shoot.Spec.Resources,
		InfrastructureConfig: shoot.Spec.Provider.InfrastructureConfig,
		ControlPlaneConfig:   shoot.Spec.Provider.ControlPlaneConfig,
	})

	updatedShoot, err := patchConverter.ToShoot(*runtime)
	if err != nil {
		panic(err)
	}

	err = saveOutputShootFile(outputPath, &updatedShoot)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Shoot CR generated successfully and saved to %s\n", outputPath)
}

func readRuntimeCRFromFile(filePath string) (*imv1.Runtime, error) {
	return readObjectFromFile[imv1.Runtime](filePath)
}

func readShootFromFile(filePath string) (*gardener.Shoot, error) {
	return readObjectFromFile[gardener.Shoot](filePath)
}

func readObjectFromFile[T any](filePath string) (*T, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var obj T
	err = yaml.Unmarshal(fileBytes, &obj)
	if err != nil {
		return nil, err
	}

	return &obj, nil
}

func createKcpClient(kcpKubeconfigPath string) (client.Client, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kcpKubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch rest config: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := addToScheme(scheme); err != nil {
		return nil, err
	}

	return client.New(restCfg, client.Options{
		Scheme: scheme,
	})
}

func addToScheme(s *runtime.Scheme) error {
	for _, add := range []func(s *runtime.Scheme) error{
		corev1.AddToScheme,
		v1.AddToScheme,
	} {
		if err := add(s); err != nil {
			return fmt.Errorf("unable to add scheme: %w", err)
		}
	}
	return nil
}

func getConverterConfig(kcpClient client.Client) (kimConfig.ConverterConfig, error) {
	var cm v12.ConfigMap
	key := types.NamespacedName{
		Name:      "infrastructure-manager-converter-config",
		Namespace: "kcp-system",
	}

	err := kcpClient.Get(context.Background(), key, &cm)
	if err != nil {
		return kimConfig.ConverterConfig{}, err
	}

	getReader := func() (io.Reader, error) {
		data, found := cm.Data["converter_config.json"]
		if !found {
			return nil, fmt.Errorf("converter_config.json not found in ConfigMap")
		}
		return strings.NewReader(data), nil
	}

	var cfg kimConfig.Config
	if err = cfg.Load(getReader); err != nil {
		return kimConfig.ConverterConfig{}, err
	}

	return cfg.ConverterConfig, nil
}

func saveOutputShootFile(outputPath string, updatedShoot *gardener.Shoot) error {
	outputBytes, err := yaml.Marshal(updatedShoot)
	if err != nil {
		return err
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	_, err = outputFile.Write(outputBytes)
	if err != nil {
		return err
	}

	return nil
}
