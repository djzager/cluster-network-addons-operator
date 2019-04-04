/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	components "github.com/kubevirt/cluster-network-addons-operator/pkg/components"
	names "github.com/kubevirt/cluster-network-addons-operator/pkg/names"
	extv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

type operatorData struct {
	Deployment        string
	DeploymentSpec    string
	ClusterRoleString string
	Rules             string
	CRD               *extv1beta1.CustomResourceDefinition
	CRDString         string
	CRString          string
}

type templateData struct {
	Namespace       string
	CsvVersion      string
	ContainerPrefix string
	ContainerTag    string
	ImagePullPolicy string
	CNA             *operatorData
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func fixResourceString(in string, indention int) string {
	out := strings.Builder{}
	scanner := bufio.NewScanner(strings.NewReader(in))
	for scanner.Scan() {
		line := scanner.Text()
		// remove separator lines
		if !strings.HasPrefix(line, "---") {
			// indent so that it fits into the manifest
			// spaces is is indention - 2, because we want to have 2 spaces less for being able to start an array
			spaces := strings.Repeat(" ", indention-2)
			if strings.HasPrefix(line, "apiGroups") {
				// spaces + array start
				out.WriteString(spaces + "- " + line + "\n")
			} else {
				// 2 more spaces
				out.WriteString(spaces + "  " + line + "\n")
			}
		}
	}
	return out.String()
}

func marshallObject(obj interface{}, writer io.Writer) error {
	jsonBytes, err := json.Marshal(obj)
	check(err)

	var r unstructured.Unstructured
	if err := json.Unmarshal(jsonBytes, &r.Object); err != nil {
		return err
	}

	// remove status and metadata.creationTimestamp
	unstructured.RemoveNestedField(r.Object, "template", "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "status")

	jsonBytes, err = json.Marshal(r.Object)
	if err != nil {
		return err
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return err
	}

	// fix templates by removing quotes...
	s := string(yamlBytes)
	s = strings.Replace(s, "'{{", "{{", -1)
	s = strings.Replace(s, "}}'", "}}", -1)
	yamlBytes = []byte(s)

	_, err = writer.Write([]byte("---\n"))
	if err != nil {
		return err
	}

	_, err = writer.Write(yamlBytes)
	if err != nil {
		return err
	}

	return nil
}

func getCNA(data *templateData) {
	writer := strings.Builder{}

	// Get CNA Deployment
	cnadeployment := components.GetDeployment(
		data.ContainerPrefix,
		data.ContainerTag,
		data.ImagePullPolicy,
	)
	err := marshallObject(cnadeployment, &writer)
	check(err)
	deployment := writer.String()

	// Get CNA DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(cnadeployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get CNA ClusterRole
	writer = strings.Builder{}
	clusterRole := components.GetClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of CNA's ClusterRole
	writer = strings.Builder{}
	cnarules := clusterRole.Rules
	for _, rule := range cnarules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get CNA CRD
	writer = strings.Builder{}
	crd := components.GetCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	cnaData := operatorData{
		Deployment:        deployment,
		DeploymentSpec:    deploymentSpec,
		ClusterRoleString: clusterRoleString,
		Rules:             rules,
		CRD:               crd,
		CRDString:         crdString,
	}
	data.CNA = &cnaData
}

func main() {
	csvVersion := flag.String("csv-version", "0.0.1", "")
	containerPrefix := flag.String("container-prefix", "kubevirt", "")
	containerTag := flag.String("container-tag", "latest", "")
	imagePullPolicy := flag.String("image-pull-policy", "Always", "")
	inputFile := flag.String("input-file", "", "")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.ParseErrorsWhitelist.UnknownFlags = true
	pflag.Parse()

	data := templateData{
		Namespace:       names.APPLIED_NAMESPACE,
		CsvVersion:      *csvVersion,
		ContainerPrefix: *containerPrefix,
		ContainerTag:    *containerTag,
		ImagePullPolicy: *imagePullPolicy,
	}

	// Load in all CNA Resources
	getCNA(&data)

	if *inputFile == "" {
		panic("Must specify input file")
	}

	manifestTemplate := template.Must(template.ParseFiles(*inputFile))
	err := manifestTemplate.Execute(os.Stdout, data)
	check(err)
}