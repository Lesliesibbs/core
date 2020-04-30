package v1

import (
	"encoding/json"
	v1 "github.com/onepanelio/core/pkg/apis/core/v1"
	"github.com/onepanelio/core/pkg/util/mapping"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"strings"
	"time"

	wfv1 "github.com/argoproj/argo/pkg/apis/workflow/v1alpha1"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TypeWorkflowTemplate        string = "workflow_template"
	TypeWorkflowTemplateVersion string = "workflow_template_version"
	TypeWorkflowExecution       string = "workflow_execution"
	TypeCronWorkflow            string = "cron_workflow"
)

func TypeToTableName(value string) string {
	switch value {
	case TypeWorkflowTemplate:
		return "workflow_templates"
	case TypeWorkflowTemplateVersion:
		return "workflow_template_versions"
	case TypeWorkflowExecution:
		return "workflow_executions"
	case TypeCronWorkflow:
		return "cron_workflows"
	}

	return ""
}

type Namespace struct {
	Name   string
	Labels map[string]string
}

type Secret struct {
	Name string
	Data map[string]string
}

type ConfigMap struct {
	Name string
	Data map[string]string
}

type LogEntry struct {
	Timestamp time.Time
	Content   string
}

type Metric struct {
	Name   string
	Value  float64
	Format string `json:"omitempty"`
}

type CronWorkflow struct {
	ID                        uint64
	CreatedAt                 time.Time  `db:"created_at"`
	ModifiedAt                *time.Time `db:"modified_at"`
	UID                       string
	Name                      string
	GenerateName              string
	WorkflowExecution         *WorkflowExecution
	Labels                    map[string]string
	Version                   int64
	WorkflowTemplateVersionId uint64 `db:"workflow_template_version_id"`
	Manifest                  string
}

func (cw *CronWorkflow) GetParametersFromWorkflowSpec() ([]WorkflowExecutionParameter, error) {
	var parameters []WorkflowExecutionParameter

	mappedData := make(map[string]interface{})

	// todo workflow spec
	if err := yaml.Unmarshal([]byte(cw.Manifest), mappedData); err != nil {
		return nil, err
	}

	arguments, ok := mappedData["arguments"]
	if !ok {
		return parameters, nil
	}

	argumentsMap := arguments.(map[interface{}]interface{})
	parametersRaw, ok := argumentsMap["parameters"]
	if !ok {
		return parameters, nil
	}

	parametersArray, ok := parametersRaw.([]interface{})
	for _, parameter := range parametersArray {
		paramMap, ok := parameter.(map[interface{}]interface{})
		if !ok {
			continue
		}

		name := paramMap["name"].(string)
		value := paramMap["value"].(string)

		workflowParameter := WorkflowExecutionParameter{
			Name:  name,
			Value: &value,
		}

		parameters = append(parameters, workflowParameter)
	}

	return parameters, nil
}

func (cw *CronWorkflow) AddToManifestSpec(key, manifest string) error {
	currentManifestMapping, err := mapping.NewFromYamlString(cw.Manifest)
	if err != nil {
		return err
	}

	additionalManifest, err := mapping.NewFromYamlString(manifest)
	if err != nil {
		return err
	}

	currentManifestMapping[key] = additionalManifest

	updatedManifest, err := currentManifestMapping.ToYamlBytes()
	if err != nil {
		return err
	}

	cw.Manifest = string(updatedManifest)

	return nil
}

type WorkflowTemplate struct {
	ID                               uint64
	CreatedAt                        time.Time `db:"created_at"`
	UID                              string
	Name                             string
	Manifest                         string
	Version                          int64
	Versions                         int64 `db:"versions"` // How many versions there are of this template total.
	IsLatest                         bool
	IsArchived                       bool `db:"is_archived"`
	ArgoWorkflowTemplate             *wfv1.WorkflowTemplate
	Labels                           map[string]string
	WorkflowExecutionStatisticReport *WorkflowExecutionStatisticReport
	CronWorkflowsStatisticsReport    *CronWorkflowStatisticReport
	WorkflowTemplateVersionId        uint64 `db:"workflow_template_version_id"` // Reference to the associated workflow template version.
}

type Label struct {
	ID         uint64
	CreatedAt  time.Time `db:"created_at"`
	Key        string
	Value      string
	Resource   string
	ResourceId uint64 `db:"resource_id"`
}

type WorkflowExecutionStatisticReport struct {
	WorkflowTemplateId uint64 `db:"workflow_template_id"`
	Total              int32
	LastExecuted       time.Time `db:"last_executed"`
	Running            int32
	Completed          int32
	Failed             int32
}

type CronWorkflowStatisticReport struct {
	WorkflowTemplateId uint64 `db:"workflow_template_id"`
	Total              int32
}

type WorkflowTemplateVersion struct {
	ID               uint64
	UID              string
	Version          int64
	IsLatest         bool `db:"is_latest"`
	Manifest         string
	CreatedAt        time.Time         `db:"created_at"`
	WorkflowTemplate *WorkflowTemplate `db:"workflow_template"`
	Labels           map[string]string
}

type WorkflowExecutionStatistic struct {
	ID                 uint64
	WorkflowTemplateId uint64
	Name               string
	Namespace          string
	//Interface to support null values for timestamps, when scanning from db into structs
	CreatedAt  *time.Time `db:"created_at"`
	FinishedAt *time.Time `db:"finished_at"`
	FailedAt   *time.Time `db:"failed_at"`
}

func (wt *WorkflowTemplate) GetManifestBytes() []byte {
	return []byte(wt.Manifest)
}

func (wt *WorkflowTemplate) GetParametersKeyString() (map[string]string, error) {
	root := make(map[interface{}]interface{})

	if err := yaml.Unmarshal(wt.GetManifestBytes(), root); err != nil {
		return nil, err
	}

	arguments, ok := root["arguments"]
	if !ok {
		return nil, nil
	}

	argumentsMap, ok := arguments.(map[interface{}]interface{})
	if !ok {
		return nil, nil
	}

	parameters, ok := argumentsMap["parameters"]
	if !ok {
		return nil, nil
	}

	parametersAsArray, ok := parameters.([]interface{})
	if !ok {
		return nil, nil
	}

	if len(parametersAsArray) == 0 {
		delete(root, arguments)
	}

	result := make(map[string]string)
	for index, parameter := range parametersAsArray {
		parameterMap, ok := parameter.(map[interface{}]interface{})
		if !ok {
			continue
		}

		key := parameterMap["name"]
		keyAsString, ok := key.(string)
		if !ok {
			continue
		}

		parameterMap["order"] = index
		remainingParameters, err := yaml.Marshal(parameterMap)
		if err != nil {
			continue
		}

		result[keyAsString] = string(remainingParameters)
	}

	return result, nil
}

func (wt *WorkflowTemplate) GenerateUID() (string, error) {
	uid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	wt.UID = uid.String()

	return wt.UID, nil
}

func (wt *WorkflowTemplate) UpdateManifestParameters(params []WorkflowExecutionParameter) error {
	manifestMap, err := mapping.NewFromYamlString(wt.Manifest)
	if err != nil {
		return err
	}

	arguments, err := manifestMap.GetChildMap("arguments")
	if err != nil {
		return err
	}

	arguments["parameters"] = params

	manifestBytes, err := manifestMap.ToYamlBytes()
	if err != nil {
		return err
	}

	wt.Manifest = string(manifestBytes)

	return nil
}

func (wt *WorkflowTemplate) GetWorkflowManifestBytes() ([]byte, error) {
	if wt.ArgoWorkflowTemplate == nil {
		return []byte{}, nil
	}

	wt.ArgoWorkflowTemplate.TypeMeta.Kind = "Workflow"
	wt.ArgoWorkflowTemplate.ObjectMeta = metav1.ObjectMeta{
		GenerateName: wt.ArgoWorkflowTemplate.ObjectMeta.GenerateName,
		Labels:       wt.ArgoWorkflowTemplate.ObjectMeta.Labels,
	}

	return json.Marshal(wt.ArgoWorkflowTemplate)
}

func (wt *WorkflowTemplate) FormatManifest() (string, error) {
	manifestMap, err := mapping.NewFromYamlString(wt.Manifest)
	if err != nil {
		log.WithFields(log.Fields{
			"Method": "FormatManifest",
			"Step":   "NewFromYamlString",
			"Error":  err.Error(),
		}).Error("FormatManifest Workflow Template failed.")

		return "", nil
	}

	manifestMap, err = manifestMap.GetChildMap("spec")
	if err != nil {
		log.WithFields(log.Fields{
			"Method": "FormatManifest",
			"Step":   "GetChildMap",
			"Error":  err.Error(),
		}).Error("GetChildMap Workflow Template failed.")

		return "", nil
	}
	manifestMap.PruneEmpty()

	wt.AddWorkflowTemplateParametersFromAnnotations(manifestMap)

	manifestBytes, err := manifestMap.ToYamlBytes()
	if err != nil {
		log.WithFields(log.Fields{
			"Method": "FormatManifest",
			"Step":   "ToYamlBytes",
			"Error":  err.Error(),
		}).Error("ToYamlBytes Workflow Template failed.")
	}

	return string(manifestBytes), nil
}

// Take the manifest from the workflow template, which is just the "spec" contents
// and wrap it so we have
// {
//    metadata: {},
//    spec: spec_data
// }
// the above wrapping is what is returned.
func (wt *WorkflowTemplate) WrapSpec() ([]byte, error) {
	data := wt.GetManifestBytes()

	mapping := make(map[interface{}]interface{})

	if err := yaml.Unmarshal(data, mapping); err != nil {
		return nil, err
	}

	contentMap := map[interface{}]interface{}{
		"metadata": make(map[interface{}]interface{}),
		"spec":     mapping,
	}

	finalBytes, err := yaml.Marshal(contentMap)
	if err != nil {
		return nil, nil
	}

	return finalBytes, nil
}

func (wt *WorkflowTemplate) AddWorkflowTemplateParametersFromAnnotations(spec mapping.Mapping) {
	if wt.ArgoWorkflowTemplate == nil {
		return
	}

	annotations := wt.ArgoWorkflowTemplate.Annotations
	if spec == nil || len(annotations) == 0 {
		return
	}

	arguments, err := spec.GetChildMap("arguments")
	if err != nil {
		return
	}

	arguments["parameters"] = make([]interface{}, 0)
	parameters := make([]interface{}, len(annotations))

	for _, value := range annotations {
		data, err := mapping.NewFromYamlString(value)
		if err != nil {
			log.WithFields(log.Fields{
				"Method": "AddWorkflowTemplateParametersFromAnnotations",
				"Step":   "NewFromYamlString",
				"Error":  err.Error(),
			}).Error("Error with AddWorkflowTemplateParametersFromAnnotations")
			continue
		}

		order := 0
		orderValue, ok := data["order"]
		if ok {
			order = orderValue.(int)
			delete(data, "order")

			if order >= 0 && order < len(parameters) {
				parameters[order] = data
			}
		}
	}

	arguments["parameters"] = parameters
}

type WorkflowExecution struct {
	ID               uint64
	CreatedAt        time.Time `db:"created_at"`
	UID              string
	Name             string
	GenerateName     string
	Parameters       []WorkflowExecutionParameter
	Manifest         string
	Phase            wfv1.NodePhase
	StartedAt        *time.Time        `db:"started_at"`
	FinishedAt       *time.Time        `db:"finished_at"`
	WorkflowTemplate *WorkflowTemplate `db:"workflow_template"`
	Labels           map[string]string
}

// TODO: Using an alias so we can refactor out WorkflowExecutionParameter
type WorkflowExecutionParameter = v1.Parameter

type ListOptions = metav1.ListOptions

type PodGCStrategy = wfv1.PodGCStrategy

type WorkflowExecutionOptions struct {
	Name           string
	GenerateName   string
	Entrypoint     string
	Parameters     []WorkflowExecutionParameter
	ServiceAccount string
	Labels         *map[string]string
	ListOptions    *ListOptions
	PodGCStrategy  *PodGCStrategy
}

type File struct {
	Path         string
	Name         string
	Size         int64
	Extension    string
	ContentType  string
	LastModified time.Time
	Directory    bool
}

type ArtifactRepositoryS3Config struct {
	S3 struct {
		Bucket          string
		Endpoint        string
		Insecure        string
		Region          string
		AccessKeySecret struct {
			Name string
			Key  string
		}
		SecretKeySecret struct {
			Name string
			Key  string
		}
		Key string
	}
}

// Given a path, returns the parent path, asssuming a '/' delimitor
// Result does not have a trailing slash.
// -> a/b/c/d would return a/b/c
// -> a/b/c/d/ would return a/b/c
// If path is empty string, it is returned.
// If path is '/' (root) it is returned as is.
// If there is no '/', '/' is returned.
func FilePathToParentPath(path string) string {
	separator := "/"
	if path == "" || path == separator {
		return path
	}

	if strings.HasSuffix(path, "/") {
		path = path[0 : len(path)-1]
	}

	lastIndexOfForwardSlash := strings.LastIndex(path, separator)
	if lastIndexOfForwardSlash <= 0 {
		return separator
	}

	return path[0:lastIndexOfForwardSlash]
}

func FilePathToExtension(path string) string {
	dotIndex := strings.LastIndex(path, ".")

	if dotIndex == -1 {
		return ""
	}

	if dotIndex == (len(path) - 1) {
		return ""
	}

	return path[dotIndex+1:]
}

func FilePathToName(path string) string {
	if strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	lastSlashIndex := strings.LastIndex(path, "/")
	if lastSlashIndex < 0 {
		return path
	}

	return path[lastSlashIndex+1:]
}

func WorkflowTemplatesToIds(workflowTemplates []*WorkflowTemplate) (ids []uint64) {
	mappedIds := make(map[uint64]bool)

	// This is to make sure we don't have duplicates
	for _, workflowTemplate := range workflowTemplates {
		mappedIds[workflowTemplate.ID] = true
	}

	for id := range mappedIds {
		ids = append(ids, id)
	}

	return
}

func WorkflowTemplatesToVersionIds(workflowTemplates []*WorkflowTemplate) (ids []uint64) {
	mappedIds := make(map[uint64]bool)

	// This is to make sure we don't have duplicates
	for _, workflowTemplate := range workflowTemplates {
		mappedIds[workflowTemplate.WorkflowTemplateVersionId] = true
	}

	for id := range mappedIds {
		ids = append(ids, id)
	}

	return
}

func WorkflowTemplateVersionsToIds(resources []*WorkflowTemplateVersion) (ids []uint64) {
	mappedIds := make(map[uint64]bool)

	// This is to make sure we don't have duplicates
	for _, resource := range resources {
		mappedIds[resource.ID] = true
	}

	for id := range mappedIds {
		ids = append(ids, id)
	}

	return
}

func CronWorkflowsToIds(resources []*CronWorkflow) (ids []uint64) {
	mappedIds := make(map[uint64]bool)

	// This is to make sure we don't have duplicates
	for _, resource := range resources {
		mappedIds[resource.ID] = true
	}

	for id := range mappedIds {
		ids = append(ids, id)
	}

	return
}

type WorkspaceTemplate struct {
	ID               uint64
	UID              string
	Name             string
	Version          int64
	Manifest         string
	IsLatest         bool
	CreatedAt        time.Time `db:"created_at"`
	WorkflowTemplate *WorkflowTemplate
}

func (wt *WorkspaceTemplate) GenerateUID() (string, error) {
	uid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	wt.UID = uid.String()

	return wt.UID, nil
}
