package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/BurntSushi/toml"
	"github.com/Praqma/helmsman/aws"
	"github.com/Praqma/helmsman/azure"
	"helmsman/gcs"
)

// printMap prints to the console any map of string keys and values.
func printMap(m map[string]string, indent int) {
	for key, value := range m {
		fmt.Println(strings.Repeat("\t", indent)+key, " : ", value)
	}
}

// printObjectMap prints to the console any map of string keys and object values.
func printNamespacesMap(m map[string]namespace) {
	for key, value := range m {
		fmt.Println(key, " : protected = ", value)
	}
}

// fromTOML reads a toml file and decodes it to a state type.
// It uses the BurntSuchi TOML parser which throws an error if the TOML file is not valid.
func fromTOML(file string, s *state) (bool, string) {
	rawTomlFile, err := ioutil.ReadFile(file)
	if err != nil {
		return false, err.Error()
	}

	tomlFile := string(rawTomlFile)
	if !noEnvSubst {
		logs.Info("Substituting env variables in file: " + file)
		tomlFile = substituteEnv(tomlFile)
	}
	if !noSSMSubst {
		logs.Debug("Substituting SSM variables in file: " + file)
		tomlFile = substituteSSM(tomlFile)
	}

	if _, err := toml.Decode(tomlFile, s); err != nil {
		return false, err.Error()
	}
	addDefaultHelmRepos(s)
	resolvePaths(file, s)
	substituteVarsInValuesFiles(s)

	return true, "Parsed TOML [[ " + file + " ]] successfully and found [ " + strconv.Itoa(len(s.Apps)) + " ] apps."
}

// toTOML encodes a state type into a TOML file.
// It uses the BurntSuchi TOML parser.
func toTOML(file string, s *state) {
	logs.Info("Printing generated toml ... ")
	var buff bytes.Buffer
	var (
		newFile *os.File
		err     error
	)

	if err := toml.NewEncoder(&buff).Encode(s); err != nil {
		logError(err.Error())
		os.Exit(1)
	}
	newFile, err = os.Create(file)
	if err != nil {
		logError(err.Error())
	}
	bytesWritten, err := newFile.Write(buff.Bytes())
	if err != nil {
		logError(err.Error())
	}
	log.Printf("Wrote %d bytes.\n", bytesWritten)
	newFile.Close()
}

// fromYAML reads a yaml file and decodes it to a state type.
// parser which throws an error if the YAML file is not valid.
func fromYAML(file string, s *state) (bool, string) {
	rawYamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		return false, err.Error()
	}

	yamlFile := string(rawYamlFile)
	if !noEnvSubst {
		logs.Debug("Substituting env variables in file: " + file)
		yamlFile = substituteEnv(yamlFile)
	}
	if !noSSMSubst {
		logs.Debug("Substituting SSM variables in file: " + file)
		yamlFile = substituteSSM(yamlFile)
	}

	if err = yaml.UnmarshalStrict([]byte(yamlFile), s); err != nil {
		return false, err.Error()
	}
	addDefaultHelmRepos(s)
	resolvePaths(file, s)
	substituteVarsInValuesFiles(s)

	return true, "Parsed YAML [[ " + file + " ]] successfully and found [ " + strconv.Itoa(len(s.Apps)) + " ] apps."
}

// toYaml encodes a state type into a YAML file
func toYAML(file string, s *state) {
	logs.Info("Printing generated yaml ... ")
	var buff bytes.Buffer
	var (
		newFile *os.File
		err     error
	)

	if err := yaml.NewEncoder(&buff).Encode(s); err != nil {
		logError(err.Error())
		os.Exit(1)
	}
	newFile, err = os.Create(file)
	if err != nil {
		logError(err.Error())
	}
	bytesWritten, err := newFile.Write(buff.Bytes())
	if err != nil {
		logError(err.Error())
	}
	log.Printf("Wrote %d bytes.\n", bytesWritten)
	newFile.Close()
}

// substituteVarsInValuesFiles loops through the values/secrets files and substitutes variables into them.
func substituteVarsInValuesFiles(s *state) {
	for _, v := range s.Apps {
		if v.ValuesFile != "" {
			v.ValuesFile = substituteVarsInYaml(v.ValuesFile)
		}
		if v.SecretsFile != "" {
			v.SecretsFile = substituteVarsInYaml(v.SecretsFile)
		}
		for i := range v.ValuesFiles {
			v.ValuesFiles[i] = substituteVarsInYaml(v.ValuesFiles[i])
		}
		for i := range v.SecretsFiles {
			v.SecretsFiles[i] = substituteVarsInYaml(v.SecretsFiles[i])
		}
	}
}

// substituteVarsInYaml substitutes variables in a Yaml file and creates a temp file with these values.
// Returns the path for the temp file
func substituteVarsInYaml(file string) string {
	rawYamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		logError(err.Error())
	}

	yamlFile := string(rawYamlFile)
	if !noEnvSubst && !noEnvValuesSubst {
		logs.Debug("Substituting env variables in file: " + file)
		yamlFile = substituteEnv(yamlFile)
	}
	if !noSSMSubst && !noSSMValuesSubst {
		logs.Debug("Substituting SSM variables in file: " + file)
		yamlFile = substituteSSM(yamlFile)
	}

	dir, err := ioutil.TempDir(tempFilesDir, "tmp")
	if err != nil {
		logError(err.Error())
	}

	// output file contents with env variables substituted into temp files
	outFile := path.Join(dir, filepath.Base(file))
	err = ioutil.WriteFile(outFile, []byte(yamlFile), 0644)
	if err != nil {
		logError(err.Error())
	}
	return outFile
}

// invokes either yaml or toml parser considering file extension
func fromFile(file string, s *state) (bool, string) {
	if isOfType(file, []string{".toml"}) {
		return fromTOML(file, s)
	} else if isOfType(file, []string{".yaml", ".yml"}) {
		return fromYAML(file, s)
	} else {
		return false, "State file does not have toml/yaml extension."
	}
}

func toFile(file string, s *state) {
	if isOfType(file, []string{".toml"}) {
		toTOML(file, s)
	} else if isOfType(file, []string{".yaml", ".yml"}) {
		toYAML(file, s)
	} else {
		logError("State file does not have toml/yaml extension.")
	}
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// addDefaultHelmRepos adds stable and incubator helm repos to the state if they are not already defined
func addDefaultHelmRepos(s *state) {
	if noDefaultRepos {
		logs.Info("Default helm repo set disabled, 'stable' and 'incubator' repos unset.")
		return
	}
	if s.HelmRepos == nil || len(s.HelmRepos) == 0 {
		s.HelmRepos = map[string]string{
			"stable":    stableHelmRepo,
			"incubator": incubatorHelmRepo,
		}
		logs.Info("No helm repos provided, using the default 'stable' and 'incubator' repos.")
	}
	if _, ok := s.HelmRepos["stable"]; !ok {
		s.HelmRepos["stable"] = stableHelmRepo
	}
	if _, ok := s.HelmRepos["incubator"]; !ok {
		s.HelmRepos["incubator"] = incubatorHelmRepo
	}
}

// resolvePaths resolves relative paths of certs/keys/chart and replace them with a absolute paths
func resolvePaths(relativeToFile string, s *state) {
	dir := filepath.Dir(relativeToFile)
	for ns, v := range s.Namespaces {
		s.Namespaces[ns] = v
	}
	for k, v := range s.Apps {
		if v.ValuesFile != "" {
			v.ValuesFile, _ = filepath.Abs(filepath.Join(dir, v.ValuesFile))
		}
		if v.SecretsFile != "" {
			v.SecretsFile, _ = filepath.Abs(filepath.Join(dir, v.SecretsFile))
		}
		for i, f := range v.ValuesFiles {
			v.ValuesFiles[i], _ = filepath.Abs(filepath.Join(dir, f))
		}
		for i, f := range v.SecretsFiles {
			v.SecretsFiles[i], _ = filepath.Abs(filepath.Join(dir, f))
		}

		if v.Chart != "" {
			var repoOrDir = filepath.Dir(v.Chart)
			_, isRepo := s.HelmRepos[repoOrDir]
			isRepo = isRepo || stringInSlice(repoOrDir, s.PreconfiguredHelmRepos)
			if !isRepo {
				// if there is no repo for the chart, we assume it's intended to be a local path

				// support env vars in path
				v.Chart = os.ExpandEnv(v.Chart)
				// respect absolute paths to charts but resolve relative paths
				if !filepath.IsAbs(v.Chart) {
					v.Chart, _ = filepath.Abs(filepath.Join(dir, v.Chart))
				}
			}
		}
		s.Apps[k] = v
	}
	// resolving paths for Bearer Token path in settings
	if s.Settings.BearerTokenPath != "" {
		if _, err := url.ParseRequestURI(s.Settings.BearerTokenPath); err != nil {
			s.Settings.BearerTokenPath, _ = filepath.Abs(filepath.Join(dir, s.Settings.BearerTokenPath))
		}
	}
	// resolving paths for k8s certificate files
	for k, v := range s.Certificates {
		if _, err := url.ParseRequestURI(v); err != nil {
			v, _ = filepath.Abs(filepath.Join(dir, v))
		}
		s.Certificates[k] = v
	}
}

// isOfType checks if the file extension of a filename/path is the same as "filetype".
// isisOfType is case insensitive. filetype should contain the "." e.g. ".yaml"
func isOfType(filename string, filetypes []string) bool {
	lowerMap := make(map[string]struct{})
	for _, v := range filetypes {
		lowerMap[strings.ToLower(v)] = struct{}{}
	}
	_, result := lowerMap[filepath.Ext(strings.ToLower(filename))]
	return result
}

// readFile returns the content of a file as a string.
// takes a file path as input. It throws an error and breaks the program execution if it fails to read the file.
func readFile(filepath string) string {
	data, err := ioutil.ReadFile(filepath)
	if err != nil {
		logError("failed to read [ " + filepath + " ] file content: " + err.Error())
	}
	return string(data)
}

// logVersions prints the versions of kubectl and helm to the logs
func logVersions() {
	logs.Debug("kubectl client version: " + kubectlVersion)
	logs.Debug("Helm client version: " + helmVersion)
}

// substituteEnv checks if a string has an env variable (contains '$'), then it returns its value
// if the env variable is empty or unset, an empty string is returned
// if the string does not contain '$', it is returned as is.
func substituteEnv(name string) string {
	if strings.Contains(name, "$") {
		// add $$ escaping for $ strings
		os.Setenv("HELMSMAN_DOLLAR", "$")
		return os.ExpandEnv(strings.Replace(name, "$$", "${HELMSMAN_DOLLAR}", -1))
	}
	return name
}

// substituteSSM checks if a string has an SSM parameter variable (contains '{{ssm: '), then it returns its value
// if the env variable is empty or unset, an empty string is returned
// if the string does not contain '$', it is returned as is.
func substituteSSM(name string) string {
	if strings.Contains(name, "{{ssm: ") {
		re := regexp.MustCompile(`{{ssm: ([^~}]+)(~(true))?}}`)
		matches := re.FindAllSubmatch([]byte(name), -1)
		for _, match := range matches {
			placeholder := string(match[0])
			paramPath := string(match[1])
			withDecryption, err := strconv.ParseBool(string(match[3]))
			if err != nil {
				fmt.Printf("Invalid decryption argument %T \n", string(match[3]))
			}
			value := aws.ReadSSMParam(paramPath, withDecryption, noColors)
			name = strings.ReplaceAll(name, placeholder, value)
		}
	}
	return name
}

// sliceContains checks if a string slice contains a given string
func sliceContains(slice []string, s string) bool {
	for _, a := range slice {
		if strings.TrimSpace(a) == s {
			return true
		}
	}
	return false
}

// downloadFile downloads a file from GCS or AWS buckets and name it with a given outfile
// if downloaded, returns the outfile name. If the file path is local file system path, it is copied to current directory.
func downloadFile(path string, outfile string) string {
	if strings.HasPrefix(path, "s3") {

		tmp := getBucketElements(path)
		aws.ReadFile(tmp["bucketName"], tmp["filePath"], outfile, noColors)

	} else if strings.HasPrefix(path, "gs") {

		tmp := getBucketElements(path)
		msg, err := gcs.ReadFile(tmp["bucketName"], tmp["filePath"], outfile, noColors)
		if err != nil {
			logs.Fatal(msg)
		}

	} else if strings.HasPrefix(path, "az") {

		tmp := getBucketElements(path)
		azure.ReadFile(tmp["bucketName"], tmp["filePath"], outfile, noColors)

	} else {

		logs.Info("" + outfile + " will be used from local file system.")
		copyFile(path, outfile)
	}
	return outfile
}

// copyFile copies a file from source to destination
func copyFile(source string, destination string) {
	from, err := os.Open(source)
	if err != nil {
		logError("while copying " + source + " to " + destination + " : " + err.Error())
	}
	defer from.Close()

	to, err := os.OpenFile(destination, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		logError("while copying " + source + " to " + destination + " : " + err.Error())
	}
	defer to.Close()

	_, err = io.Copy(to, from)
	if err != nil {
		logError("while copying " + source + " to " + destination + " : " + err.Error())
	}
}

// deleteFile deletes a file
func deleteFile(path string) {
	logs.Info("Cleaning up ... deleting " + path)
	if err := os.Remove(path); err != nil {
		logError("Could not delete file: " + path)
	}
}

// notifySlack sends a JSON formatted message to Slack over a webhook url
// It takes the content of the message (what changes helmsman is going to do or have done separated by \n)
// and the webhook URL as well as a flag specifying if this is a failure message or not
// It returns true if the sending of the message is successful, otherwise returns false
func notifySlack(content string, url string, failure bool, executing bool) bool {
	logs.Info("Posting notifications to slack ... ")

	color := "#36a64f" // green
	if failure {
		color = "#FF0000" // red
	}

	var pretext string
	if content == "" {
		pretext = "No actions to perform!"
	} else if failure {
		pretext = "Failed to generate/execute a plan: "
	} else if executing && !failure {
		pretext = "Here is what I have done: "
	} else {
		pretext = "Here is what I am going to do:"
	}

	t := time.Now().UTC()

	var jsonStr = []byte(`{
		"attachments": [
			{
				"fallback": "Helmsman results.",
				"color": "` + color + `" ,
				"pretext": "` + pretext + `",
				"title": "` + content + `",
				"footer": "Helmsman ` + appVersion + `",
				"ts": ` + strconv.FormatInt(t.Unix(), 10) + `
			}
		]
	}`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logError("while sending notifications to slack" + err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true
	}
	return false
}

// logError sends a notification on slack if a webhook URL is provided and logs the error before terminating.
func logError(msg string) {
	if _, err := url.ParseRequestURI(s.Settings.SlackWebhook); err == nil {
		notifySlack(msg, s.Settings.SlackWebhook, true, apply)
	}
	logs.Fatal(msg)
}

// getBucketElements returns a map containing the bucket name and the file path inside the bucket
// this func works for S3, Azure and GCS bucket links of the format:
// s3 or gs://bucketname/dir.../file.ext
func getBucketElements(link string) map[string]string {

	tmp := strings.SplitAfterN(link, "//", 2)[1]
	m := make(map[string]string)
	m["bucketName"] = strings.SplitN(tmp, "/", 2)[0]
	m["filePath"] = strings.SplitN(tmp, "/", 2)[1]
	return m
}

// replaceStringInFile takes a map of keys and values and replaces the keys with values within a given file.
// It saves the modified content in a new file
func replaceStringInFile(input []byte, outfile string, replacements map[string]string) {
	output := input
	for k, v := range replacements {
		output = bytes.Replace(output, []byte(k), []byte(v), -1)
	}

	if err := ioutil.WriteFile(outfile, output, 0666); err != nil {
		logError(err.Error())
	}
}

// Indent inserts prefix at the beginning of each non-empty line of s. The
// end-of-line marker is NL.
func Indent(s, prefix string) string {
	var res []byte
	bol := true
	for _, c := range []byte(s) {
		if bol && c != '\n' {
			res = append(res, []byte(prefix)...)
		}
		res = append(res, c)
		bol = c == '\n'
	}
	return string(res)
}

// isLocalChart checks if a chart specified in the DSF is a local directory or not
func isLocalChart(chart string) bool {
	_, err := os.Stat(chart)
	if err == nil {
		return true
	}
	return false
}

// concat appends all slices to a single slice
func concat(slices ...[]string) []string {
	slice := []string{}
	for _, item := range slices {
		slice = append(slice, item...)
	}
	return slice
}

func writeStringToFile(filename string, data string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	if err != nil {
		return err
	}
	return file.Sync()
}
