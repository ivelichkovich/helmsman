package main

import (
	"io/ioutil"
	"strings"

	"gopkg.in/yaml.v2"
)

// addNamespaces creates a set of namespaces in your k8s cluster.
// If a namespace with the same name exists, it will skip it.
// If --ns-override flag is used, it only creates the provided namespace in that flag
func addNamespaces(namespaces map[string]namespace) {
	if nsOverride == "" {
		for nsName, ns := range namespaces {
			createNamespace(nsName)
			labelNamespace(nsName, ns.Labels)
			annotateNamespace(nsName, ns.Annotations)
			setLimits(nsName, ns.Limits)
		}
	} else {
		createNamespace(nsOverride)
		overrideAppsNamespace(nsOverride)
	}
}

// overrideAppsNamespace replaces all apps namespaces with one specific namespace
func overrideAppsNamespace(newNs string) {
	logs.Info("Overriding apps namespaces with [ " + newNs + " ] ...")
	for _, r := range s.Apps {
		overrideNamespace(r, newNs)
	}
}

// createNamespace creates a namespace in the k8s cluster
func createNamespace(ns string) {
	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"create", "namespace", ns},
		Description: "creating namespace  " + ns,
	}
	exitCode, _, _ := cmd.exec(debug, verbose)
	if exitCode != 0 && verbose {
		logs.Debug("Namespace [ " + ns + " ] is created.")
	}
}

// labelNamespace labels a namespace with provided labels
func labelNamespace(ns string, labels map[string]string) {
	for k, v := range labels {
		cmd := command{
			Cmd:         "kubectl",
			Args:        []string{"label", "--overwrite", "namespace/" + ns, k + "=" + v},
			Description: "labeling namespace  " + ns,
		}

		exitCode, _, _ := cmd.exec(debug, verbose)
		if exitCode != 0 && verbose {
			logs.Warning("Can't label namespace [ " + ns + " with " + k + "=" + v +
				" ]. It already exists.")
		}
	}
}

// annotateNamespace annotates a namespace with provided annotations
func annotateNamespace(ns string, labels map[string]string) {
	for k, v := range labels {
		cmd := command{
			Cmd:         "kubectl",
			Args:        []string{"annotate", "--overwrite", "namespace/" + ns, k + "=" + v},
			Description: "annotating namespace  " + ns,
		}

		exitCode, _, _ := cmd.exec(debug, verbose)
		if exitCode != 0 && verbose {
			logs.Info("Can't annotate namespace [ " + ns + " with " + k + "=" + v +
				" ]. It already exists.")
		}
	}
}

// setLimits creates a LimitRange resource in the provided Namespace
func setLimits(ns string, lims limits) {

	if len(lims) == 0 {
		return
	}

	definition := `
---
apiVersion: v1
kind: LimitRange
metadata:
  name: limit-range
spec:
  limits:
`
	d, err := yaml.Marshal(&lims)
	if err != nil {
		logError(err.Error())
	}

	definition = definition + Indent(string(d), strings.Repeat(" ", 4))

	if err := ioutil.WriteFile("temp-LimitRange.yaml", []byte(definition), 0666); err != nil {
		logError(err.Error())
	}

	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"apply", "-f", "temp-LimitRange.yaml", "-n", ns},
		Description: "creating LimitRange in namespace [ " + ns + " ]",
	}

	exitCode, e, _ := cmd.exec(debug, verbose)

	if exitCode != 0 {
		logError("failed to create LimitRange in namespace [ " + ns + " ]: " + e)
	}

	deleteFile("temp-LimitRange.yaml")

}

// createContext creates a context -connecting to a k8s cluster- in kubectl config.
// It returns true if successful, false otherwise
func createContext() (bool, string) {
	if s.Settings.BearerToken && s.Settings.BearerTokenPath == "" {
		logs.Info("Creating kube context with bearer token from K8S service account.")
		s.Settings.BearerTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	} else if s.Settings.BearerToken && s.Settings.BearerTokenPath != "" {
		logs.Info("Creating kube context with bearer token from " + s.Settings.BearerTokenPath)
	} else if s.Settings.Password == "" || s.Settings.Username == "" || s.Settings.ClusterURI == "" {
		return false, "missing information to create context [ " + s.Settings.KubeContext + " ] " +
			"you are either missing PASSWORD, USERNAME or CLUSTERURI in the Settings section of your desired state file."
	} else if !s.Settings.BearerToken && (s.Certificates == nil || s.Certificates["caCrt"] == "" || s.Certificates["caKey"] == "") {
		return false, "missing information to create context [ " + s.Settings.KubeContext + " ] " +
			"you are either missing caCrt or caKey or both in the Certifications section of your desired state file."
	} else if s.Settings.BearerToken && (s.Certificates == nil || s.Certificates["caCrt"] == "") {
		return false, "missing information to create context [ " + s.Settings.KubeContext + " ] " +
			"caCrt is missing in the Certifications section of your desired state file."
	}

	// set certs locations (relative filepath, GCS bucket, AWS bucket)
	caCrt := s.Certificates["caCrt"]
	caKey := s.Certificates["caKey"]
	caClient := s.Certificates["caClient"]

	// download certs and keys
	// GCS bucket+file format should be: gs://bucket-name/dir.../filename.ext
	// S3 bucket+file format should be: s3://bucket-name/dir.../filename.ext

	// CA cert
	if caCrt != "" {

		caCrt = downloadFile(caCrt, "ca.crt")

	}

	// CA key
	if caKey != "" {

		caKey = downloadFile(caKey, "ca.key")

	}

	// client certificate
	if caClient != "" {

		caClient = downloadFile(caClient, "client.crt")

	}

	// bearer token
	tokenPath := "bearer.token"
	if s.Settings.BearerToken && s.Settings.BearerTokenPath != "" {
		downloadFile(s.Settings.BearerTokenPath, tokenPath)
	}

	// connecting to the cluster
	setCredentialsCmdArgs := []string{}
	if s.Settings.BearerToken {
		token := readFile(tokenPath)
		if s.Settings.Username == "" {
			s.Settings.Username = "helmsman"
		}
		setCredentialsCmdArgs = append(setCredentialsCmdArgs, "config", "set-credentials", s.Settings.Username, "--token="+token)
	} else {
		setCredentialsCmdArgs = append(setCredentialsCmdArgs, "config", "set-credentials", s.Settings.Username, "--username="+s.Settings.Username,
			"--password="+s.Settings.Password, "--client-key="+caKey)
		if caClient != "" {
			setCredentialsCmdArgs = append(setCredentialsCmdArgs, "--client-certificate="+caClient)
		}
	}
	cmd := command{
		Cmd:         "kubectl",
		Args:        setCredentialsCmdArgs,
		Description: "creating kubectl context - setting credentials.",
	}

	if exitCode, err, _ := cmd.exec(debug, verbose); exitCode != 0 {
		return false, "failed to create context [ " + s.Settings.KubeContext + " ]:  " + err
	}

	cmd = command{
		Cmd:         "kubectl",
		Args:        []string{"config", "set-cluster", s.Settings.KubeContext, "--server=" + s.Settings.ClusterURI, "--certificate-authority=" + caCrt},
		Description: "creating kubectl context - setting cluster.",
	}

	if exitCode, err, _ := cmd.exec(debug, verbose); exitCode != 0 {
		return false, "failed to create context [ " + s.Settings.KubeContext + " ]: " + err
	}

	cmd = command{
		Cmd:         "kubectl",
		Args:        []string{"config", "set-context", s.Settings.KubeContext, "--cluster=" + s.Settings.KubeContext, "--user=" + s.Settings.Username},
		Description: "creating kubectl context - setting context.",
	}

	if exitCode, err, _ := cmd.exec(debug, verbose); exitCode != 0 {
		return false, "failed to create context [ " + s.Settings.KubeContext + " ]: " + err
	}

	if setKubeContext(s.Settings.KubeContext) {
		return true, ""
	}

	return false, "something went wrong while setting the kube context to the newly created one."
}

// setKubeContext sets your kubectl context to the one specified in the desired state file.
// It returns false if it fails to set the context. This means the context does not exist.
func setKubeContext(context string) bool {
	if context == "" {
		return getKubeContext()
	}

	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"config", "use-context", context},
		Description: "setting kubectl context to [ " + context + " ]",
	}

	exitCode, _, _ := cmd.exec(debug, verbose)

	if exitCode != 0 {
		logs.Info("KubeContext: " + context + " does not exist. I will try to create it.")
		return false
	}

	return true
}

// getKubeContext gets your kubectl context.
// It returns false if no context is set.
func getKubeContext() bool {
	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"config", "current-context"},
		Description: "getting kubectl context",
	}

	exitCode, result, _ := cmd.exec(debug, verbose)

	if exitCode != 0 || result == "" {
		logs.Info("Kubectl context is not set")
		return false
	}

	return true
}

// createServiceAccount creates a service account in a given namespace and associates it with a cluster-admin role
func createServiceAccount(saName string, namespace string) (bool, string) {
	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"create", "serviceaccount", "-n", namespace, saName},
		Description: "creating service account [ " + saName + " ] in namespace [ " + namespace + " ]",
	}

	exitCode, err, _ := cmd.exec(debug, verbose)

	if exitCode != 0 {
		//logError("failed to create service account " + saName + " in namespace [ " + namespace + " ]: " + err)
		return false, err
	}

	return true, ""
}

// createRoleBinding creates a role binding in a given namespace for a service account with a cluster-role/role in the cluster.
func createRoleBinding(role string, saName string, namespace string) (bool, string) {
	clusterRole := false
	resource := "rolebinding"

	if role == "cluster-admin" {
		clusterRole = true
		resource = "clusterrolebinding"
	}

	bindingName := saName + "-binding"
	bindingOption := "--role=" + role
	if clusterRole {
		bindingOption = "--clusterrole=" + role
		bindingName = namespace + ":" + saName + "-binding"
	}

	logs.Info("Creating " + resource + " for service account [ " + saName + " ] in namespace [ " + namespace + " ] with role: " + role + ".")
	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"create", resource, bindingName, bindingOption, "--serviceaccount", namespace + ":" + saName, "-n", namespace},
		Description: "creating " + resource + " for service account [ " + saName + " ] in namespace [ " + namespace + " ] with role: " + role,
	}

	exitCode, err, _ := cmd.exec(debug, verbose)

	if exitCode != 0 {
		return false, err
	}

	return true, ""
}

// createRole creates a k8s Role in a given namespace from a template
func createRole(namespace string, role string, roleTemplateFile string) (bool, string) {
	var resource []byte
	var e error

	if roleTemplateFile != "" {
		// load file from path of TillerRoleTemplateFile
		resource, e = ioutil.ReadFile(roleTemplateFile)
	} else {
		// load static resource
		resource, e = Asset("data/role.yaml")
	}
	if e != nil {
		logError(e.Error())
	}
	replaceStringInFile(resource, "temp-modified-role.yaml", map[string]string{"<<namespace>>": namespace, "<<role-name>>": role})

	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"apply", "-f", "temp-modified-role.yaml"},
		Description: "creating role [" + role + "] in namespace [ " + namespace + " ]",
	}

	exitCode, err, _ := cmd.exec(debug, verbose)

	if exitCode != 0 {
		return false, err
	}

	deleteFile("temp-modified-role.yaml")

	return true, ""
}

// labelResource applies Helmsman specific labels to Helm's state resources (secrets/configmaps)
func labelResource(r *release) {
	if r.Enabled {
		logs.Info("Applying Helmsman labels to [ " + r.Name + " ] in namespace [ " + r.Namespace + " ] ")
		storageBackend := "secret"

		if s.Settings.StorageBackend != "" {
			storageBackend = s.Settings.StorageBackend
		}

		cmd := command{
			Cmd:         "kubectl",
			Args:        []string{"label", storageBackend, "-n", r.Namespace, "-l", "owner=helm,name=" + r.Name, "MANAGED-BY=HELMSMAN", "NAMESPACE=" + r.Namespace, "--overwrite"},
			Description: "applying labels to Helm state for " + r.Name,
		}

		exitCode, err, _ := cmd.exec(debug, verbose)

		if exitCode != 0 {
			logError(err)
		}
	}
}

// getHelmsmanReleases returns a map of all releases that are labeled with "MANAGED-BY=HELMSMAN"
// The releases are categorized by the namespaces in which their Tiller is running
// The returned map format is: map[<Tiller namespace>:map[<releases managed by Helmsman and deployed using this Tiller>:true]]
func getHelmsmanReleases() map[string]map[*release]bool {
	var lines []string
	releases := make(map[string]map[*release]bool)
	storageBackend := "secret"

	if s.Settings.StorageBackend != "" {
		storageBackend = s.Settings.StorageBackend
	}

	for ns, _ := range s.Namespaces {
		cmd := command{
			Cmd:         "kubectl",
			Args:        []string{"get", storageBackend, "-n", ns, "-l", "MANAGED-BY=HELMSMAN"},
			Description: "getting helm releases which are managed by Helmsman in namespace [[ " + ns + " ]].",
		}

		exitCode, output, _ := cmd.exec(debug, verbose)

		if exitCode != 0 {
			logError(output)
		}
		if strings.ToUpper("No resources found.") != strings.ToUpper(strings.TrimSpace(output)) {
			lines = strings.Split(output, "\n")
		}

		for i := 0; i < len(lines); i++ {
			if lines[i] == "" || strings.HasSuffix(strings.TrimSpace(lines[i]), "AGE") {
				continue
			} else {
				fields := strings.Fields(lines[i])
				if _, ok := releases[ns]; !ok {
					releases[ns] = make(map[*release]bool)
				}
				for _, r := range s.Apps {
					if r.Name == fields[0][0:strings.LastIndex(fields[0], ".v")] {
						releases[ns][r] = true
					}
				}
			}
		}
	}

	return releases
}

// getKubectlClientVersion returns kubectl client version
func getKubectlClientVersion() string {
	cmd := command{
		Cmd:         "kubectl",
		Args:        []string{"version", "--client", "--short"},
		Description: "checking kubectl version ",
	}

	exitCode, result, _ := cmd.exec(debug, false)
	if exitCode != 0 {
		logError("while checking kubectl version: " + result)
	}
	return result
}
