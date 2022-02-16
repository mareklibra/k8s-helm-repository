package main

import (
	"crypto/tls"
	b64 "encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	oscrypto "github.com/openshift/library-go/pkg/crypto"
)

type HelmRequest struct {
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	Values    map[string]interface{} `json:"values"`
}

type HelmTemplateSpec struct {
	Name       string
	B64Content string `yaml:"b64Content"`
}

type HelmCR struct {
	ApiVersion string
	Kind       string
	Metadata   struct {
		Name      string
		Namespace string
	}
	Spec struct {
		Values    string
		Templates []HelmTemplateSpec
	}
}

type configFlagsWithTransport struct {
	*genericclioptions.ConfigFlags
	Transport *http.RoundTripper
}

func (c configFlagsWithTransport) ToRESTConfig() (*rest.Config, error) {
	return &rest.Config{
		Host:        *c.APIServer,
		BearerToken: *c.BearerToken,
		Transport:   *c.Transport,
	}, nil
}

func getActionConfigurations() *action.Configuration {

	apiServer := "" // TODO set k8s api
	token := ""     // TODO set token
	namespace := "default"

	serviceProxyTLSConfig := oscrypto.SecureTLSConfig(&tls.Config{
		InsecureSkipVerify: true,
	})

	var roundTripper http.RoundTripper = &http.Transport{
		TLSClientConfig: serviceProxyTLSConfig,
	}

	confFlags := &configFlagsWithTransport{
		ConfigFlags: &genericclioptions.ConfigFlags{
			APIServer:   &apiServer,
			BearerToken: &token,
			Namespace:   &namespace,
		},
		Transport: &roundTripper,
	}

	conf := new(action.Configuration)
	conf.Init(confFlags, "default", "secrets", klog.Infof)

	return conf
}

func loadHelmCR() (helmCR HelmCR) {
	filename, _ := filepath.Abs("./crds/test/test-chart.yaml")
	crFile, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Println("Failed to read cr")
	}

	err = yaml.Unmarshal(crFile, &helmCR)
	if err != nil {
		fmt.Println("Failed to parse cr")
	}
	return helmCR
}

func createHelmChart(helmCR HelmCR) *chart.Chart {
	c := new(chart.Chart)

	// Chart.yaml
	var metadata chart.Metadata
	metadata.APIVersion = chart.APIVersionV2
	metadata.Name = helmCR.Metadata.Name

	// should be added to CRD
	metadata.Description = "foo"
	metadata.Type = "application"
	metadata.Version = "0.0.1"
	metadata.AppVersion = "latest"

	c.Metadata = &metadata

	// values.yaml
	c.Values = make(map[string]interface{})
	values, err := b64.StdEncoding.DecodeString(helmCR.Spec.Values)
	if err != nil {
		fmt.Println("Failed to base64 decode values.yaml")
	}
	if err := yaml.Unmarshal(values, &c.Values); err != nil {
		fmt.Println("Failed to parse values.yaml")
	}

	// templates
	for _, t := range helmCR.Spec.Templates {
		template, err := b64.StdEncoding.DecodeString(t.B64Content)
		if err != nil {
			fmt.Println("Failed to base64 decode template")
		}
		c.Templates = append(c.Templates, &chart.File{Name: t.Name, Data: template})
	}

	if err := c.Validate(); err != nil {
		fmt.Println("Chart validation failed")
	}

	return c
}

func install(w http.ResponseWriter, req *http.Request) {
	helmCR := loadHelmCR()
	ch := createHelmChart(helmCR)

	conf := getActionConfigurations()
	cmd := action.NewInstall(conf)

	cmd.ReleaseName = fmt.Sprintf("%s-%d", "chart", time.Now().Unix())
	cmd.Namespace = "default"
	var vals = map[string]interface{}{
		"pullSecretB64": "eyJhdXRocyI6eyJmb28uY29tIjogeyJhdXRoIjogImIzQmxibk5vYVdaMExYSmxiR1ZoYzJVdFpHVjg9IiwiZW1haWwiOiJmb29AYmFyLmNvbSJ9fX0K",
	}
	_, err := cmd.Run(ch, vals)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	http.HandleFunc("/install", install)
	http.ListenAndServe(":8090", nil)
}
