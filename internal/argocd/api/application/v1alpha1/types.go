//nolint:unused
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/watch"
)

// Application is a definition of Application resource.
// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=applications,shortName=app;apps
// +kubebuilder:printcolumn:name="Sync Status",type=string,JSONPath=`.status.sync.status`
// +kubebuilder:printcolumn:name="Health Status",type=string,JSONPath=`.status.health.status`
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.status.sync.revision`,priority=10
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata" protobuf:"bytes,1,opt,name=metadata"`
	Spec              ApplicationSpec   `json:"spec" protobuf:"bytes,2,opt,name=spec"`
	Status            ApplicationStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
	Operation         *Operation        `json:"operation,omitempty" protobuf:"bytes,4,opt,name=operation"`
}

// ApplicationSpec represents desired application state. Contains link to repository with application definition and additional parameters link definition revision.
type ApplicationSpec struct {
	// Source is a reference to the location of the application's manifests or chart
	Source *ApplicationSource `json:"source,omitempty" protobuf:"bytes,1,opt,name=source"`
	// Destination is a reference to the target Kubernetes server and namespace
	Destination ApplicationDestination `json:"destination" protobuf:"bytes,2,name=destination"`
	// Project is a reference to the project this application belongs to.
	// The empty string means that application belongs to the 'default' project.
	Project string `json:"project" protobuf:"bytes,3,name=project"`
	// SyncPolicy controls when and how a sync will be performed
	SyncPolicy *SyncPolicy `json:"syncPolicy,omitempty" protobuf:"bytes,4,name=syncPolicy"`
	// IgnoreDifferences is a list of resources and their fields which should be ignored during comparison
	IgnoreDifferences IgnoreDifferences `json:"ignoreDifferences,omitempty" protobuf:"bytes,5,name=ignoreDifferences"`
	// Info contains a list of information (URLs, email addresses, and plain text) that relates to the application
	Info []Info `json:"info,omitempty" protobuf:"bytes,6,name=info"`
	// RevisionHistoryLimit limits the number of items kept in the application's revision history, which is used for informational purposes as well as for rollbacks to previous versions.
	// This should only be changed in exceptional circumstances.
	// Setting to zero will store no history. This will reduce storage used.
	// Increasing will increase the space used to store the history, so we do not recommend increasing it.
	// Default is 10.
	RevisionHistoryLimit *int64 `json:"revisionHistoryLimit,omitempty" protobuf:"bytes,7,name=revisionHistoryLimit"`

	// Sources is a reference to the location of the application's manifests or chart
	Sources ApplicationSources `json:"sources,omitempty" protobuf:"bytes,8,opt,name=sources"`
}

type IgnoreDifferences []ResourceIgnoreDifferences

type TrackingMethod string

// ResourceIgnoreDifferences contains resource filter and list of json paths which should be ignored during comparison with live state.
type ResourceIgnoreDifferences struct {
	Group             string   `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Kind              string   `json:"kind" protobuf:"bytes,2,opt,name=kind"`
	Name              string   `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`
	Namespace         string   `json:"namespace,omitempty" protobuf:"bytes,4,opt,name=namespace"`
	JSONPointers      []string `json:"jsonPointers,omitempty" protobuf:"bytes,5,opt,name=jsonPointers"`
	JQPathExpressions []string `json:"jqPathExpressions,omitempty" protobuf:"bytes,6,opt,name=jqPathExpressions"`
	// ManagedFieldsManagers is a list of trusted managers. Fields mutated by those managers will take precedence over the
	// desired state defined in the SCM and won't be displayed in diffs
	ManagedFieldsManagers []string `json:"managedFieldsManagers,omitempty" protobuf:"bytes,7,opt,name=managedFieldsManagers"`
}

// EnvEntry represents an entry in the application's environment
type EnvEntry struct {
	// Name is the name of the variable, usually expressed in uppercase
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// Value is the value of the variable
	Value string `json:"value" protobuf:"bytes,2,opt,name=value"`
}

// Env is a list of environment variable entries
type Env []*EnvEntry

// ApplicationSource contains all required information about the source of an application
type ApplicationSource struct {
	// RepoURL is the URL to the repository (Git or Helm) that contains the application manifests
	RepoURL string `json:"repoURL" protobuf:"bytes,1,opt,name=repoURL"`
	// Path is a directory path within the Git repository, and is only valid for applications sourced from Git.
	Path string `json:"path,omitempty" protobuf:"bytes,2,opt,name=path"`
	// TargetRevision defines the revision of the source to sync the application to.
	// In case of Git, this can be commit, tag, or branch. If omitted, will equal to HEAD.
	// In case of Helm, this is a semver tag for the Chart's version.
	TargetRevision string `json:"targetRevision,omitempty" protobuf:"bytes,4,opt,name=targetRevision"`
	// Helm holds helm specific options
	Helm *ApplicationSourceHelm `json:"helm,omitempty" protobuf:"bytes,7,opt,name=helm"`
	// Kustomize holds kustomize specific options
	Kustomize *ApplicationSourceKustomize `json:"kustomize,omitempty" protobuf:"bytes,8,opt,name=kustomize"`
	// Directory holds path/directory specific options
	Directory *ApplicationSourceDirectory `json:"directory,omitempty" protobuf:"bytes,10,opt,name=directory"`
	// Plugin holds config management plugin specific options
	Plugin *ApplicationSourcePlugin `json:"plugin,omitempty" protobuf:"bytes,11,opt,name=plugin"`
	// Chart is a Helm chart name, and must be specified for applications sourced from a Helm repo.
	Chart string `json:"chart,omitempty" protobuf:"bytes,12,opt,name=chart"`
	// Ref is reference to another source within sources field. This field will not be used if used with a `source` tag.
	Ref string `json:"ref,omitempty" protobuf:"bytes,13,opt,name=ref"`
}

// ApplicationSources contains list of required information about the sources of an application
type ApplicationSources []ApplicationSource

// ApplicationSourceType specifies the type of the application's source
type ApplicationSourceType string

const (
	ApplicationSourceTypeHelm      ApplicationSourceType = "Helm"
	ApplicationSourceTypeKustomize ApplicationSourceType = "Kustomize"
	ApplicationSourceTypeDirectory ApplicationSourceType = "Directory"
	ApplicationSourceTypePlugin    ApplicationSourceType = "Plugin"
)

// RefreshType specifies how to refresh the sources of a given application
type RefreshType string

const (
	RefreshTypeNormal RefreshType = "normal"
	RefreshTypeHard   RefreshType = "hard"
)

type RefTarget struct {
	Repo           Repository `protobuf:"bytes,1,opt,name=repo"`
	TargetRevision string     `protobuf:"bytes,2,opt,name=targetRevision"`
	Chart          string     `protobuf:"bytes,3,opt,name=chart"`
}

type RefTargetRevisionMapping map[string]*RefTarget

// ApplicationSourceHelm holds helm specific options
type ApplicationSourceHelm struct {
	// ValuesFiles is a list of Helm value files to use when generating a template
	ValueFiles []string `json:"valueFiles,omitempty" protobuf:"bytes,1,opt,name=valueFiles"`
	// Parameters is a list of Helm parameters which are passed to the helm template command upon manifest generation
	Parameters []HelmParameter `json:"parameters,omitempty" protobuf:"bytes,2,opt,name=parameters"`
	// ReleaseName is the Helm release name to use. If omitted it will use the application name
	ReleaseName string `json:"releaseName,omitempty" protobuf:"bytes,3,opt,name=releaseName"`
	// Values specifies Helm values to be passed to helm template, typically defined as a block. ValuesObject takes precedence over Values, so use one or the other.
	// +patchStrategy=replace
	Values string `json:"values,omitempty" patchStrategy:"replace" protobuf:"bytes,4,opt,name=values"`
	// FileParameters are file parameters to the helm template
	FileParameters []HelmFileParameter `json:"fileParameters,omitempty" protobuf:"bytes,5,opt,name=fileParameters"`
	// Version is the Helm version to use for templating ("3")
	Version string `json:"version,omitempty" protobuf:"bytes,6,opt,name=version"`
	// PassCredentials pass credentials to all domains (Helm's --pass-credentials)
	PassCredentials bool `json:"passCredentials,omitempty" protobuf:"bytes,7,opt,name=passCredentials"`
	// IgnoreMissingValueFiles prevents helm template from failing when valueFiles do not exist locally by not appending them to helm template --values
	IgnoreMissingValueFiles bool `json:"ignoreMissingValueFiles,omitempty" protobuf:"bytes,8,opt,name=ignoreMissingValueFiles"`
	// SkipCrds skips custom resource definition installation step (Helm's --skip-crds)
	SkipCrds bool `json:"skipCrds,omitempty" protobuf:"bytes,9,opt,name=skipCrds"`
	// ValuesObject specifies Helm values to be passed to helm template, defined as a map. This takes precedence over Values.
	// +kubebuilder:pruning:PreserveUnknownFields
	ValuesObject *runtime.RawExtension `json:"valuesObject,omitempty" protobuf:"bytes,10,opt,name=valuesObject"`
}

// HelmParameter is a parameter that's passed to helm template during manifest generation
type HelmParameter struct {
	// Name is the name of the Helm parameter
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Value is the value for the Helm parameter
	Value string `json:"value,omitempty" protobuf:"bytes,2,opt,name=value"`
	// ForceString determines whether to tell Helm to interpret booleans and numbers as strings
	ForceString bool `json:"forceString,omitempty" protobuf:"bytes,3,opt,name=forceString"`
}

// HelmFileParameter is a file parameter that's passed to helm template during manifest generation
type HelmFileParameter struct {
	// Name is the name of the Helm parameter
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Path is the path to the file containing the values for the Helm parameter
	Path string `json:"path,omitempty" protobuf:"bytes,2,opt,name=path"`
}

// KustomizeImage represents a Kustomize image definition in the format [old_image_name=]<image_name>:<image_tag>
type KustomizeImage string

// KustomizeImages is a list of Kustomize images
type KustomizeImages []KustomizeImage

// ApplicationSourceKustomize holds options specific to an Application source specific to Kustomize
type ApplicationSourceKustomize struct {
	// NamePrefix is a prefix appended to resources for Kustomize apps
	NamePrefix string `json:"namePrefix,omitempty" protobuf:"bytes,1,opt,name=namePrefix"`
	// NameSuffix is a suffix appended to resources for Kustomize apps
	NameSuffix string `json:"nameSuffix,omitempty" protobuf:"bytes,2,opt,name=nameSuffix"`
	// Images is a list of Kustomize image override specifications
	Images KustomizeImages `json:"images,omitempty" protobuf:"bytes,3,opt,name=images"`
	// CommonLabels is a list of additional labels to add to rendered manifests
	CommonLabels map[string]string `json:"commonLabels,omitempty" protobuf:"bytes,4,opt,name=commonLabels"`
	// Version controls which version of Kustomize to use for rendering manifests
	Version string `json:"version,omitempty" protobuf:"bytes,5,opt,name=version"`
	// CommonAnnotations is a list of additional annotations to add to rendered manifests
	CommonAnnotations map[string]string `json:"commonAnnotations,omitempty" protobuf:"bytes,6,opt,name=commonAnnotations"`
	// ForceCommonLabels specifies whether to force applying common labels to resources for Kustomize apps
	ForceCommonLabels bool `json:"forceCommonLabels,omitempty" protobuf:"bytes,7,opt,name=forceCommonLabels"`
	// ForceCommonAnnotations specifies whether to force applying common annotations to resources for Kustomize apps
	ForceCommonAnnotations bool `json:"forceCommonAnnotations,omitempty" protobuf:"bytes,8,opt,name=forceCommonAnnotations"`
	// Namespace sets the namespace that Kustomize adds to all resources
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,9,opt,name=namespace"`
	// CommonAnnotationsEnvsubst specifies whether to apply env variables substitution for annotation values
	CommonAnnotationsEnvsubst bool `json:"commonAnnotationsEnvsubst,omitempty" protobuf:"bytes,10,opt,name=commonAnnotationsEnvsubst"`
	// Replicas is a list of Kustomize Replicas override specifications
	Replicas KustomizeReplicas `json:"replicas,omitempty" protobuf:"bytes,11,opt,name=replicas"`
	// Patches is a list of Kustomize patches
	Patches KustomizePatches `json:"patches,omitempty" protobuf:"bytes,12,opt,name=patches"`
	// Components specifies a list of kustomize components to add to the kustomization before building
	Components []string `json:"components,omitempty" protobuf:"bytes,13,rep,name=components"`
}

type KustomizeReplica struct {
	// Name of Deployment or StatefulSet
	Name string `json:"name" protobuf:"bytes,1,name=name"`
	// Number of replicas
	Count intstr.IntOrString `json:"count" protobuf:"bytes,2,name=count"`
}

type KustomizeReplicas []KustomizeReplica

type KustomizePatches []KustomizePatch

type KustomizePatch struct {
	Path    string             `json:"path,omitempty" yaml:"path,omitempty" protobuf:"bytes,1,opt,name=path"`
	Patch   string             `json:"patch,omitempty" yaml:"patch,omitempty" protobuf:"bytes,2,opt,name=patch"`
	Target  *KustomizeSelector `json:"target,omitempty" yaml:"target,omitempty" protobuf:"bytes,3,opt,name=target"`
	Options map[string]bool    `json:"options,omitempty" yaml:"options,omitempty" protobuf:"bytes,4,opt,name=options"`
}

type KustomizeSelector struct {
	KustomizeResId     `json:",inline,omitempty" yaml:",inline,omitempty" protobuf:"bytes,1,opt,name=resId"`
	AnnotationSelector string `json:"annotationSelector,omitempty" yaml:"annotationSelector,omitempty" protobuf:"bytes,2,opt,name=annotationSelector"`
	LabelSelector      string `json:"labelSelector,omitempty" yaml:"labelSelector,omitempty" protobuf:"bytes,3,opt,name=labelSelector"`
}

type KustomizeResId struct {
	KustomizeGvk `json:",inline,omitempty" yaml:",inline,omitempty" protobuf:"bytes,1,opt,name=gvk"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty" protobuf:"bytes,2,opt,name=name"`
	Namespace    string `json:"namespace,omitempty" yaml:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`
}

type KustomizeGvk struct {
	Group   string `json:"group,omitempty" yaml:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Version string `json:"version,omitempty" yaml:"version,omitempty" protobuf:"bytes,2,opt,name=version"`
	Kind    string `json:"kind,omitempty" yaml:"kind,omitempty" protobuf:"bytes,3,opt,name=kind"`
}

// JsonnetVar represents a variable to be passed to jsonnet during manifest generation
type JsonnetVar struct {
	Name  string `json:"name" protobuf:"bytes,1,opt,name=name"`
	Value string `json:"value" protobuf:"bytes,2,opt,name=value"`
	Code  bool   `json:"code,omitempty" protobuf:"bytes,3,opt,name=code"`
}

// ApplicationSourceJsonnet holds options specific to applications of type Jsonnet
type ApplicationSourceJsonnet struct {
	// ExtVars is a list of Jsonnet External Variables
	ExtVars []JsonnetVar `json:"extVars,omitempty" protobuf:"bytes,1,opt,name=extVars"`
	// TLAS is a list of Jsonnet Top-level Arguments
	TLAs []JsonnetVar `json:"tlas,omitempty" protobuf:"bytes,2,opt,name=tlas"`
	// Additional library search dirs
	Libs []string `json:"libs,omitempty" protobuf:"bytes,3,opt,name=libs"`
}

// ApplicationSourceDirectory holds options for applications of type plain YAML or Jsonnet
type ApplicationSourceDirectory struct {
	// Recurse specifies whether to scan a directory recursively for manifests
	Recurse bool `json:"recurse,omitempty" protobuf:"bytes,1,opt,name=recurse"`
	// Jsonnet holds options specific to Jsonnet
	Jsonnet ApplicationSourceJsonnet `json:"jsonnet,omitempty" protobuf:"bytes,2,opt,name=jsonnet"`
	// Exclude contains a glob pattern to match paths against that should be explicitly excluded from being used during manifest generation
	Exclude string `json:"exclude,omitempty" protobuf:"bytes,3,opt,name=exclude"`
	// Include contains a glob pattern to match paths against that should be explicitly included during manifest generation
	Include string `json:"include,omitempty" protobuf:"bytes,4,opt,name=include"`
}

type OptionalMap struct {
	// Map is the value of a map type parameter.
	// +optional
	Map map[string]string `json:"map" protobuf:"bytes,1,rep,name=map"`
	// We need the explicit +optional so that kube-builder generates the CRD without marking this as required.
}

type OptionalArray struct {
	// Array is the value of an array type parameter.
	// +optional
	Array []string `json:"array" protobuf:"bytes,1,rep,name=array"`
	// We need the explicit +optional so that kube-builder generates the CRD without marking this as required.
}

type ApplicationSourcePluginParameter struct {
	// We use pointers to structs because go-to-protobuf represents pointers to arrays/maps as repeated fields.
	// These repeated fields have no way to represent "present but empty." So we would have no way to distinguish
	// {name: parameters, array: []} from {name: parameter}
	// By wrapping the array/map in a struct, we can use a pointer to the struct to represent "present but empty."

	// Name is the name identifying a parameter.
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// String_ is the value of a string type parameter.
	String_ *string `json:"string,omitempty" protobuf:"bytes,5,opt,name=string"`
	// Map is the value of a map type parameter.
	*OptionalMap `json:",omitempty" protobuf:"bytes,3,rep,name=map"`
	// Array is the value of an array type parameter.
	*OptionalArray `json:",omitempty" protobuf:"bytes,4,rep,name=array"`
}

type ApplicationSourcePluginParameters []ApplicationSourcePluginParameter

// ApplicationSourcePlugin holds options specific to config management plugins
type ApplicationSourcePlugin struct {
	Name       string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Env        `json:"env,omitempty" protobuf:"bytes,2,opt,name=env"`
	Parameters ApplicationSourcePluginParameters `json:"parameters,omitempty" protobuf:"bytes,3,opt,name=parameters"`
}

// ApplicationDestination holds information about the application's destination
type ApplicationDestination struct {
	// Server specifies the URL of the target cluster's Kubernetes control plane API. This must be set if Name is not set.
	Server string `json:"server,omitempty" protobuf:"bytes,1,opt,name=server"`
	// Namespace specifies the target namespace for the application's resources.
	// The namespace will only be set for namespace-scoped resources that have not set a value for .metadata.namespace
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
	// Name is an alternate way of specifying the target cluster by its symbolic name. This must be set if Server is not set.
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`

	// nolint:govet
	isServerInferred bool `json:"-"`
}

type ResourceHealthLocation string

var (
	ResourceHealthLocationInline  ResourceHealthLocation = ""
	ResourceHealthLocationAppTree ResourceHealthLocation = "appTree"
)

// ApplicationStatus contains status information for the application
type ApplicationStatus struct {
	// Resources is a list of Kubernetes resources managed by this application
	Resources []ResourceStatus `json:"resources,omitempty" protobuf:"bytes,1,opt,name=resources"`
	// Sync contains information about the application's current sync status
	Sync SyncStatus `json:"sync,omitempty" protobuf:"bytes,2,opt,name=sync"`
	// Health contains information about the application's current health status
	Health HealthStatus `json:"health,omitempty" protobuf:"bytes,3,opt,name=health"`
	// History contains information about the application's sync history
	History RevisionHistories `json:"history,omitempty" protobuf:"bytes,4,opt,name=history"`
	// Conditions is a list of currently observed application conditions
	Conditions []ApplicationCondition `json:"conditions,omitempty" protobuf:"bytes,5,opt,name=conditions"`
	// ReconciledAt indicates when the application state was reconciled using the latest git version
	ReconciledAt *metav1.Time `json:"reconciledAt,omitempty" protobuf:"bytes,6,opt,name=reconciledAt"`
	// OperationState contains information about any ongoing operations, such as a sync
	OperationState *OperationState `json:"operationState,omitempty" protobuf:"bytes,7,opt,name=operationState"`
	// ObservedAt indicates when the application state was updated without querying latest git state
	// Deprecated: controller no longer updates ObservedAt field
	ObservedAt *metav1.Time `json:"observedAt,omitempty" protobuf:"bytes,8,opt,name=observedAt"`
	// SourceType specifies the type of this application
	SourceType ApplicationSourceType `json:"sourceType,omitempty" protobuf:"bytes,9,opt,name=sourceType"`
	// Summary contains a list of URLs and container images used by this application
	Summary ApplicationSummary `json:"summary,omitempty" protobuf:"bytes,10,opt,name=summary"`
	// ResourceHealthSource indicates where the resource health status is stored: inline if not set or appTree
	ResourceHealthSource ResourceHealthLocation `json:"resourceHealthSource,omitempty" protobuf:"bytes,11,opt,name=resourceHealthSource"`
	// SourceTypes specifies the type of the sources included in the application
	SourceTypes []ApplicationSourceType `json:"sourceTypes,omitempty" protobuf:"bytes,12,opt,name=sourceTypes"`
	// ControllerNamespace indicates the namespace in which the application controller is located
	ControllerNamespace string `json:"controllerNamespace,omitempty" protobuf:"bytes,13,opt,name=controllerNamespace"`
}

// JWTTokens represents a list of JWT tokens
type JWTTokens struct {
	Items []JWTToken `json:"items,omitempty" protobuf:"bytes,1,opt,name=items"`
}

// OperationInitiator contains information about the initiator of an operation
type OperationInitiator struct {
	// Username contains the name of a user who started operation
	Username string `json:"username,omitempty" protobuf:"bytes,1,opt,name=username"`
	// Automated is set to true if operation was initiated automatically by the application controller.
	Automated bool `json:"automated,omitempty" protobuf:"bytes,2,opt,name=automated"`
}

// Operation contains information about a requested or running operation
type Operation struct {
	// Sync contains parameters for the operation
	Sync *SyncOperation `json:"sync,omitempty" protobuf:"bytes,1,opt,name=sync"`
	// InitiatedBy contains information about who initiated the operations
	InitiatedBy OperationInitiator `json:"initiatedBy,omitempty" protobuf:"bytes,2,opt,name=initiatedBy"`
	// Info is a list of informational items for this operation
	Info []*Info `json:"info,omitempty" protobuf:"bytes,3,name=info"`
	// Retry controls the strategy to apply if a sync fails
	Retry RetryStrategy `json:"retry,omitempty" protobuf:"bytes,4,opt,name=retry"`
}

// SyncOperationResource contains resources to sync.
type SyncOperationResource struct {
	Group     string `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Kind      string `json:"kind" protobuf:"bytes,2,opt,name=kind"`
	Name      string `json:"name" protobuf:"bytes,3,opt,name=name"`
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,4,opt,name=namespace"`
	// nolint:govet
	Exclude bool `json:"-"`
}

// RevisionHistories is a array of history, oldest first and newest last
type RevisionHistories []RevisionHistory

// SyncOperation contains details about a sync operation.
type SyncOperation struct {
	// Revision is the revision (Git) or chart version (Helm) which to sync the application to
	// If omitted, will use the revision specified in app spec.
	Revision string `json:"revision,omitempty" protobuf:"bytes,1,opt,name=revision"`
	// Prune specifies to delete resources from the cluster that are no longer tracked in git
	Prune bool `json:"prune,omitempty" protobuf:"bytes,2,opt,name=prune"`
	// DryRun specifies to perform a `kubectl apply --dry-run` without actually performing the sync
	DryRun bool `json:"dryRun,omitempty" protobuf:"bytes,3,opt,name=dryRun"`
	// SyncStrategy describes how to perform the sync
	SyncStrategy *SyncStrategy `json:"syncStrategy,omitempty" protobuf:"bytes,4,opt,name=syncStrategy"`
	// Resources describes which resources shall be part of the sync
	Resources []SyncOperationResource `json:"resources,omitempty" protobuf:"bytes,6,opt,name=resources"`
	// Source overrides the source definition set in the application.
	// This is typically set in a Rollback operation and is nil during a Sync operation
	Source *ApplicationSource `json:"source,omitempty" protobuf:"bytes,7,opt,name=source"`
	// Manifests is an optional field that overrides sync source with a local directory for development
	Manifests []string `json:"manifests,omitempty" protobuf:"bytes,8,opt,name=manifests"`
	// SyncOptions provide per-sync sync-options, e.g. Validate=false
	SyncOptions SyncOptions `json:"syncOptions,omitempty" protobuf:"bytes,9,opt,name=syncOptions"`
	// Sources overrides the source definition set in the application.
	// This is typically set in a Rollback operation and is nil during a Sync operation
	Sources ApplicationSources `json:"sources,omitempty" protobuf:"bytes,10,opt,name=sources"`
	// Revisions is the list of revision (Git) or chart version (Helm) which to sync each source in sources field for the application to
	// If omitted, will use the revision specified in app spec.
	Revisions []string `json:"revisions,omitempty" protobuf:"bytes,11,opt,name=revisions"`
}

// OperationState contains information about state of a running operation
type OperationState struct {
	// Operation is the original requested operation
	Operation Operation `json:"operation" protobuf:"bytes,1,opt,name=operation"`

	// TODO: (killianmuldoon) Need to see if this is a safe change. This is a field we have to import from the rest of argo so we change the type.
	// Changed synccommon.OperationPhase to string.
	// Phase is the current phase of the operation
	Phase string `json:"phase" protobuf:"bytes,2,opt,name=phase"`
	// Message holds any pertinent messages when attempting to perform operation (typically errors).
	Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
	// SyncResult is the result of a Sync operation
	SyncResult *SyncOperationResult `json:"syncResult,omitempty" protobuf:"bytes,4,opt,name=syncResult"`
	// StartedAt contains time of operation start
	StartedAt metav1.Time `json:"startedAt" protobuf:"bytes,6,opt,name=startedAt"`
	// FinishedAt contains time of operation completion
	FinishedAt *metav1.Time `json:"finishedAt,omitempty" protobuf:"bytes,7,opt,name=finishedAt"`
	// RetryCount contains time of operation retries
	RetryCount int64 `json:"retryCount,omitempty" protobuf:"bytes,8,opt,name=retryCount"`
}

type Info struct {
	Name  string `json:"name" protobuf:"bytes,1,name=name"`
	Value string `json:"value" protobuf:"bytes,2,name=value"`
}

type SyncOptions []string

type ManagedNamespaceMetadata struct {
	Labels      map[string]string `json:"labels,omitempty" protobuf:"bytes,1,opt,name=labels"`
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,2,opt,name=annotations"`
}

// SyncPolicy controls when a sync will be performed in response to updates in git
type SyncPolicy struct {
	// Automated will keep an application synced to the target revision
	Automated *SyncPolicyAutomated `json:"automated,omitempty" protobuf:"bytes,1,opt,name=automated"`
	// Options allow you to specify whole app sync-options
	SyncOptions SyncOptions `json:"syncOptions,omitempty" protobuf:"bytes,2,opt,name=syncOptions"`
	// Retry controls failed sync retry behavior
	Retry *RetryStrategy `json:"retry,omitempty" protobuf:"bytes,3,opt,name=retry"`
	// ManagedNamespaceMetadata controls metadata in the given namespace (if CreateNamespace=true)
	ManagedNamespaceMetadata *ManagedNamespaceMetadata `json:"managedNamespaceMetadata,omitempty" protobuf:"bytes,4,opt,name=managedNamespaceMetadata"`
	// If you add a field here, be sure to update IsZero.
}

// RetryStrategy contains information about the strategy to apply when a sync failed
type RetryStrategy struct {
	// Limit is the maximum number of attempts for retrying a failed sync. If set to 0, no retries will be performed.
	Limit int64 `json:"limit,omitempty" protobuf:"bytes,1,opt,name=limit"`
	// Backoff controls how to backoff on subsequent retries of failed syncs
	Backoff *Backoff `json:"backoff,omitempty" protobuf:"bytes,2,opt,name=backoff,casttype=Backoff"`
}

// Backoff is the backoff strategy to use on subsequent retries for failing syncs
type Backoff struct {
	// Duration is the amount to back off. Default unit is seconds, but could also be a duration (e.g. "2m", "1h")
	Duration string `json:"duration,omitempty" protobuf:"bytes,1,opt,name=duration"`
	// Factor is a factor to multiply the base duration after each failed retry
	Factor *int64 `json:"factor,omitempty" protobuf:"bytes,2,name=factor"`
	// MaxDuration is the maximum amount of time allowed for the backoff strategy
	MaxDuration string `json:"maxDuration,omitempty" protobuf:"bytes,3,opt,name=maxDuration"`
}

// SyncPolicyAutomated controls the behavior of an automated sync
type SyncPolicyAutomated struct {
	// Prune specifies whether to delete resources from the cluster that are not found in the sources anymore as part of automated sync (default: false)
	Prune bool `json:"prune,omitempty" protobuf:"bytes,1,opt,name=prune"`
	// SelfHeal specifies whether to revert resources back to their desired state upon modification in the cluster (default: false)
	SelfHeal bool `json:"selfHeal,omitempty" protobuf:"bytes,2,opt,name=selfHeal"`
	// AllowEmpty allows apps have zero live resources (default: false)
	AllowEmpty bool `json:"allowEmpty,omitempty" protobuf:"bytes,3,opt,name=allowEmpty"`
}

// SyncStrategy controls the manner in which a sync is performed
type SyncStrategy struct {
	// Apply will perform a `kubectl apply` to perform the sync.
	Apply *SyncStrategyApply `json:"apply,omitempty" protobuf:"bytes,1,opt,name=apply"`
	// Hook will submit any referenced resources to perform the sync. This is the default strategy
	Hook *SyncStrategyHook `json:"hook,omitempty" protobuf:"bytes,2,opt,name=hook"`
}

// SyncStrategyApply uses `kubectl apply` to perform the apply
type SyncStrategyApply struct {
	// Force indicates whether or not to supply the --force flag to `kubectl apply`.
	// The --force flag deletes and re-create the resource, when PATCH encounters conflict and has
	// retried for 5 times.
	Force bool `json:"force,omitempty" protobuf:"bytes,1,opt,name=force"`
}

// SyncStrategyHook will perform a sync using hooks annotations.
// If no hook annotation is specified falls back to `kubectl apply`.
type SyncStrategyHook struct {
	// Embed SyncStrategyApply type to inherit any `apply` options
	// +optional
	SyncStrategyApply `json:",inline" protobuf:"bytes,1,opt,name=syncStrategyApply"`
}

// RevisionMetadata contains metadata for a specific revision in a Git repository
type RevisionMetadata struct {
	// who authored this revision,
	// typically their name and email, e.g. "John Doe <john_doe@my-company.com>",
	// but might not match this example
	Author string `json:"author,omitempty" protobuf:"bytes,1,opt,name=author"`
	// Date specifies when the revision was authored
	Date metav1.Time `json:"date" protobuf:"bytes,2,opt,name=date"`
	// Tags specifies any tags currently attached to the revision
	// Floating tags can move from one revision to another
	Tags []string `json:"tags,omitempty" protobuf:"bytes,3,opt,name=tags"`
	// Message contains the message associated with the revision, most likely the commit message.
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`
	// SignatureInfo contains a hint on the signer if the revision was signed with GPG, and signature verification is enabled.
	SignatureInfo string `json:"signatureInfo,omitempty" protobuf:"bytes,5,opt,name=signatureInfo"`
}

// ChartDetails contains helm chart metadata for a specific version
type ChartDetails struct {
	Description string `json:"description,omitempty" protobuf:"bytes,1,opt,name=description"`
	// The URL of this projects home page, e.g. "http://example.com"
	Home string `json:"home,omitempty" protobuf:"bytes,2,opt,name=home"`
	// List of maintainer details, name and email, e.g. ["John Doe <john_doe@my-company.com>"]
	Maintainers []string `json:"maintainers,omitempty" protobuf:"bytes,3,opt,name=maintainers"`
}

// SyncOperationResult represent result of sync operation
type SyncOperationResult struct {
	// Resources contains a list of sync result items for each individual resource in a sync operation
	Resources ResourceResults `json:"resources,omitempty" protobuf:"bytes,1,opt,name=resources"`
	// Revision holds the revision this sync operation was performed to
	Revision string `json:"revision" protobuf:"bytes,2,opt,name=revision"`
	// Source records the application source information of the sync, used for comparing auto-sync
	Source ApplicationSource `json:"source,omitempty" protobuf:"bytes,3,opt,name=source"`
	// Source records the application source information of the sync, used for comparing auto-sync
	Sources ApplicationSources `json:"sources,omitempty" protobuf:"bytes,4,opt,name=sources"`
	// Revisions holds the revision this sync operation was performed for respective indexed source in sources field
	Revisions []string `json:"revisions,omitempty" protobuf:"bytes,5,opt,name=revisions"`
	// ManagedNamespaceMetadata contains the current sync state of managed namespace metadata
	ManagedNamespaceMetadata *ManagedNamespaceMetadata `json:"managedNamespaceMetadata,omitempty" protobuf:"bytes,6,opt,name=managedNamespaceMetadata"`
}

// ResourceResult holds the operation result details of a specific resource
type ResourceResult struct {
	// Group specifies the API group of the resource
	Group string `json:"group" protobuf:"bytes,1,opt,name=group"`
	// Version specifies the API version of the resource
	Version string `json:"version" protobuf:"bytes,2,opt,name=version"`
	// Kind specifies the API kind of the resource
	Kind string `json:"kind" protobuf:"bytes,3,opt,name=kind"`
	// Namespace specifies the target namespace of the resource
	Namespace string `json:"namespace" protobuf:"bytes,4,opt,name=namespace"`
	// Name specifies the name of the resource
	Name    string `json:"name" protobuf:"bytes,5,opt,name=name"`
	Message string `json:"message,omitempty" protobuf:"bytes,7,opt,name=message"`
	// HookType specifies the type of the hook. Empty for non-hook resources

	// TODO: (killianmuldoon) Need to see if this is a safe change. This is a field we have to import from the rest of argo so we change the type.
	// The following are changed from the upstream implementation.
	// Status -> string
	// HookType -> string
	// HookPhase -> string
	// SyncPhase -> string
	// Status holds the final result of the sync. Will be empty if the resources is yet to be applied/pruned and is always zero-value for hooks
	Status string `json:"status,omitempty" protobuf:"bytes,6,opt,name=status"`
	// Message contains an informational or error message for the last sync OR operation
	HookType string `json:"hookType,omitempty" protobuf:"bytes,8,opt,name=hookType"`
	// HookPhase contains the state of any operation associated with this resource OR hook
	// This can also contain values for non-hook resources.
	HookPhase string `json:"hookPhase,omitempty" protobuf:"bytes,9,opt,name=hookPhase"`
	// SyncPhase indicates the particular phase of the sync that this result was acquired in
	SyncPhase string `json:"syncPhase,omitempty" protobuf:"bytes,10,opt,name=syncPhase"`
}

// ResourceResults defines a list of resource results for a given operation
type ResourceResults []*ResourceResult

// RevisionHistory contains history information about a previous sync
type RevisionHistory struct {
	// Revision holds the revision the sync was performed against
	Revision string `json:"revision,omitempty" protobuf:"bytes,2,opt,name=revision"`
	// DeployedAt holds the time the sync operation completed
	DeployedAt metav1.Time `json:"deployedAt" protobuf:"bytes,4,opt,name=deployedAt"`
	// ID is an auto incrementing identifier of the RevisionHistory
	ID int64 `json:"id" protobuf:"bytes,5,opt,name=id"`
	// Source is a reference to the application source used for the sync operation
	Source ApplicationSource `json:"source,omitempty" protobuf:"bytes,6,opt,name=source"`
	// DeployStartedAt holds the time the sync operation started
	DeployStartedAt *metav1.Time `json:"deployStartedAt,omitempty" protobuf:"bytes,7,opt,name=deployStartedAt"`
	// Sources is a reference to the application sources used for the sync operation
	Sources ApplicationSources `json:"sources,omitempty" protobuf:"bytes,8,opt,name=sources"`
	// Revisions holds the revision of each source in sources field the sync was performed against
	Revisions []string `json:"revisions,omitempty" protobuf:"bytes,9,opt,name=revisions"`
	// InitiatedBy contains information about who initiated the operations
	InitiatedBy OperationInitiator `json:"initiatedBy,omitempty" protobuf:"bytes,10,opt,name=initiatedBy"`
}

// ApplicationWatchEvent contains information about application change.
type ApplicationWatchEvent struct {
	Type watch.EventType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=k8s.io/apimachinery/pkg/watch.EventType"`

	// Application is:
	//  * If Type is Added or Modified: the new state of the object.
	//  * If Type is Deleted: the state of the object immediately before deletion.
	//  * If Type is Error: *api.Status is recommended; other types may make sense
	//    depending on context.
	Application Application `json:"application" protobuf:"bytes,2,opt,name=application"`
}

// ApplicationList is list of Application resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Application `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// ComponentParameter contains information about component parameter value
type ComponentParameter struct {
	Component string `json:"component,omitempty" protobuf:"bytes,1,opt,name=component"`
	Name      string `json:"name" protobuf:"bytes,2,opt,name=name"`
	Value     string `json:"value" protobuf:"bytes,3,opt,name=value"`
}

// SyncStatusCode is a type which represents possible comparison results
type SyncStatusCode string

// Possible comparison results
const (
	// SyncStatusCodeUnknown indicates that the status of a sync could not be reliably determined
	SyncStatusCodeUnknown SyncStatusCode = "Unknown"
	// SyncStatusCodeOutOfSync indicates that desired and live states match
	SyncStatusCodeSynced SyncStatusCode = "Synced"
	// SyncStatusCodeOutOfSync indicates that there is a drift between desired and live states
	SyncStatusCodeOutOfSync SyncStatusCode = "OutOfSync"
)

// Copied from: https://github.com/argoproj/gitops-engine/blob/55bb49480a68ca9653b88ddbbc66584d52267081/pkg/health/health.go
// Represents resource health status
type HealthStatusCode string

const (
	// Indicates that health assessment failed and actual health status is unknown
	HealthStatusUnknown HealthStatusCode = "Unknown"
	// Progressing health status means that resource is not healthy but still have a chance to reach healthy state
	HealthStatusProgressing HealthStatusCode = "Progressing"
	// Resource is 100% healthy
	HealthStatusHealthy HealthStatusCode = "Healthy"
	// Assigned to resources that are suspended or paused. The typical example is a
	// [suspended](https://kubernetes.io/docs/tasks/job/automated-tasks-with-cron-jobs/#suspend) CronJob.
	HealthStatusSuspended HealthStatusCode = "Suspended"
	// Degrade status is used if resource status indicates failure or resource could not reach healthy state
	// within some timeout.
	HealthStatusDegraded HealthStatusCode = "Degraded"
	// Indicates that resource is missing in the cluster.
	HealthStatusMissing HealthStatusCode = "Missing"
)

// ApplicationConditionType represents type of application condition. Type name has following convention:
// prefix "Error" means error condition
// prefix "Warning" means warning condition
// prefix "Info" means informational condition
type ApplicationConditionType = string

const (
	// ApplicationConditionDeletionError indicates that controller failed to delete application
	ApplicationConditionDeletionError = "DeletionError"
	// ApplicationConditionInvalidSpecError indicates that application source is invalid
	ApplicationConditionInvalidSpecError = "InvalidSpecError"
	// ApplicationConditionComparisonError indicates controller failed to compare application state
	ApplicationConditionComparisonError = "ComparisonError"
	// ApplicationConditionSyncError indicates controller failed to automatically sync the application
	ApplicationConditionSyncError = "SyncError"
	// ApplicationConditionUnknownError indicates an unknown controller error
	ApplicationConditionUnknownError = "UnknownError"
	// ApplicationConditionSharedResourceWarning indicates that controller detected resources which belongs to more than one application
	ApplicationConditionSharedResourceWarning = "SharedResourceWarning"
	// ApplicationConditionRepeatedResourceWarning indicates that application source has resource with same Group, Kind, Name, Namespace multiple times
	ApplicationConditionRepeatedResourceWarning = "RepeatedResourceWarning"
	// ApplicationConditionExcludedResourceWarning indicates that application has resource which is configured to be excluded
	ApplicationConditionExcludedResourceWarning = "ExcludedResourceWarning"
	// ApplicationConditionOrphanedResourceWarning indicates that application has orphaned resources
	ApplicationConditionOrphanedResourceWarning = "OrphanedResourceWarning"
)

// ApplicationCondition contains details about an application condition, which is usually an error or warning
type ApplicationCondition struct {
	// Type is an application condition type
	Type ApplicationConditionType `json:"type" protobuf:"bytes,1,opt,name=type"`
	// Message contains human-readable message indicating details about condition
	Message string `json:"message" protobuf:"bytes,2,opt,name=message"`
	// LastTransitionTime is the time the condition was last observed
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`
}

// ComparedTo contains application source and target which was used for resources comparison
type ComparedTo struct {
	// Source is a reference to the application's source used for comparison
	Source ApplicationSource `json:"source,omitempty" protobuf:"bytes,1,opt,name=source"`
	// Destination is a reference to the application's destination used for comparison
	Destination ApplicationDestination `json:"destination" protobuf:"bytes,2,opt,name=destination"`
	// Sources is a reference to the application's multiple sources used for comparison
	Sources ApplicationSources `json:"sources,omitempty" protobuf:"bytes,3,opt,name=sources"`
	// IgnoreDifferences is a reference to the application's ignored differences used for comparison
	IgnoreDifferences IgnoreDifferences `json:"ignoreDifferences,omitempty" protobuf:"bytes,4,opt,name=ignoreDifferences"`
}

// SyncStatus contains information about the currently observed live and desired states of an application
type SyncStatus struct {
	// Status is the sync state of the comparison
	Status SyncStatusCode `json:"status" protobuf:"bytes,1,opt,name=status,casttype=SyncStatusCode"`
	// ComparedTo contains information about what has been compared
	ComparedTo ComparedTo `json:"comparedTo,omitempty" protobuf:"bytes,2,opt,name=comparedTo"`
	// Revision contains information about the revision the comparison has been performed to
	Revision string `json:"revision,omitempty" protobuf:"bytes,3,opt,name=revision"`
	// Revisions contains information about the revisions of multiple sources the comparison has been performed to
	Revisions []string `json:"revisions,omitempty" protobuf:"bytes,4,opt,name=revisions"`
}

// HealthStatus contains information about the currently observed health state of an application or resource
type HealthStatus struct {
	// Status holds the status code of the application or resource
	Status HealthStatusCode `json:"status,omitempty" protobuf:"bytes,1,opt,name=status"`
	// Message is a human-readable informational message describing the health status
	Message string `json:"message,omitempty" protobuf:"bytes,2,opt,name=message"`
}

// InfoItem contains arbitrary, human readable information about an application
type InfoItem struct {
	// Name is a human readable title for this piece of information.
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Value is human readable content.
	Value string `json:"value,omitempty" protobuf:"bytes,2,opt,name=value"`
}

// ResourceNetworkingInfo holds networking resource related information
// TODO: describe members of this type
type ResourceNetworkingInfo struct {
	TargetLabels map[string]string            `json:"targetLabels,omitempty" protobuf:"bytes,1,opt,name=targetLabels"`
	TargetRefs   []ResourceRef                `json:"targetRefs,omitempty" protobuf:"bytes,2,opt,name=targetRefs"`
	Labels       map[string]string            `json:"labels,omitempty" protobuf:"bytes,3,opt,name=labels"`
	Ingress      []corev1.LoadBalancerIngress `json:"ingress,omitempty" protobuf:"bytes,4,opt,name=ingress"`
	// ExternalURLs holds list of URLs which should be available externally. List is populated for ingress resources using rules hostnames.
	ExternalURLs []string `json:"externalURLs,omitempty" protobuf:"bytes,5,opt,name=externalURLs"`
}

// TODO: describe this type
type HostResourceInfo struct {
	ResourceName         corev1.ResourceName `json:"resourceName,omitempty" protobuf:"bytes,1,name=resourceName"`
	RequestedByApp       int64               `json:"requestedByApp,omitempty" protobuf:"bytes,2,name=requestedByApp"`
	RequestedByNeighbors int64               `json:"requestedByNeighbors,omitempty" protobuf:"bytes,3,name=requestedByNeighbors"`
	Capacity             int64               `json:"capacity,omitempty" protobuf:"bytes,4,name=capacity"`
}

// HostInfo holds host name and resources metrics
// TODO: describe purpose of this type
// TODO: describe members of this type
type HostInfo struct {
	Name          string                `json:"name,omitempty" protobuf:"bytes,1,name=name"`
	ResourcesInfo []HostResourceInfo    `json:"resourcesInfo,omitempty" protobuf:"bytes,2,name=resourcesInfo"`
	SystemInfo    corev1.NodeSystemInfo `json:"systemInfo,omitempty" protobuf:"bytes,3,opt,name=systemInfo"`
}

// ApplicationTree holds nodes which belongs to the application
// TODO: describe purpose of this type
type ApplicationTree struct {
	// Nodes contains list of nodes which either directly managed by the application and children of directly managed nodes.
	Nodes []ResourceNode `json:"nodes,omitempty" protobuf:"bytes,1,rep,name=nodes"`
	// OrphanedNodes contains if or orphaned nodes: nodes which are not managed by the app but in the same namespace. List is populated only if orphaned resources enabled in app project.
	OrphanedNodes []ResourceNode `json:"orphanedNodes,omitempty" protobuf:"bytes,2,rep,name=orphanedNodes"`
	// Hosts holds list of Kubernetes nodes that run application related pods
	Hosts []HostInfo `json:"hosts,omitempty" protobuf:"bytes,3,rep,name=hosts"`
}

// ApplicationSummary contains information about URLs and container images used by an application
type ApplicationSummary struct {
	// ExternalURLs holds all external URLs of application child resources.
	ExternalURLs []string `json:"externalURLs,omitempty" protobuf:"bytes,1,opt,name=externalURLs"`
	// Images holds all images of application child resources.
	Images []string `json:"images,omitempty" protobuf:"bytes,2,opt,name=images"`
}

// ResourceRef includes fields which uniquely identify a resource
type ResourceRef struct {
	Group     string `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Version   string `json:"version,omitempty" protobuf:"bytes,2,opt,name=version"`
	Kind      string `json:"kind,omitempty" protobuf:"bytes,3,opt,name=kind"`
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,4,opt,name=namespace"`
	Name      string `json:"name,omitempty" protobuf:"bytes,5,opt,name=name"`
	UID       string `json:"uid,omitempty" protobuf:"bytes,6,opt,name=uid"`
}

// ResourceNode contains information about live resource and its children
// TODO: describe members of this type
type ResourceNode struct {
	ResourceRef     `json:",inline" protobuf:"bytes,1,opt,name=resourceRef"`
	ParentRefs      []ResourceRef           `json:"parentRefs,omitempty" protobuf:"bytes,2,opt,name=parentRefs"`
	Info            []InfoItem              `json:"info,omitempty" protobuf:"bytes,3,opt,name=info"`
	NetworkingInfo  *ResourceNetworkingInfo `json:"networkingInfo,omitempty" protobuf:"bytes,4,opt,name=networkingInfo"`
	ResourceVersion string                  `json:"resourceVersion,omitempty" protobuf:"bytes,5,opt,name=resourceVersion"`
	Images          []string                `json:"images,omitempty" protobuf:"bytes,6,opt,name=images"`
	Health          *HealthStatus           `json:"health,omitempty" protobuf:"bytes,7,opt,name=health"`
	CreatedAt       *metav1.Time            `json:"createdAt,omitempty" protobuf:"bytes,8,opt,name=createdAt"`
}

// ResourceStatus holds the current sync and health status of a resource
// TODO: describe members of this type
type ResourceStatus struct {
	Group           string         `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Version         string         `json:"version,omitempty" protobuf:"bytes,2,opt,name=version"`
	Kind            string         `json:"kind,omitempty" protobuf:"bytes,3,opt,name=kind"`
	Namespace       string         `json:"namespace,omitempty" protobuf:"bytes,4,opt,name=namespace"`
	Name            string         `json:"name,omitempty" protobuf:"bytes,5,opt,name=name"`
	Status          SyncStatusCode `json:"status,omitempty" protobuf:"bytes,6,opt,name=status"`
	Health          *HealthStatus  `json:"health,omitempty" protobuf:"bytes,7,opt,name=health"`
	Hook            bool           `json:"hook,omitempty" protobuf:"bytes,8,opt,name=hook"`
	RequiresPruning bool           `json:"requiresPruning,omitempty" protobuf:"bytes,9,opt,name=requiresPruning"`
	SyncWave        int64          `json:"syncWave,omitempty" protobuf:"bytes,10,opt,name=syncWave"`
}

// ResourceDiff holds the diff of a live and target resource object
// TODO: describe members of this type
type ResourceDiff struct {
	Group     string `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Kind      string `json:"kind,omitempty" protobuf:"bytes,2,opt,name=kind"`
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`
	Name      string `json:"name,omitempty" protobuf:"bytes,4,opt,name=name"`
	// TargetState contains the JSON serialized resource manifest defined in the Git/Helm
	TargetState string `json:"targetState,omitempty" protobuf:"bytes,5,opt,name=targetState"`
	// TargetState contains the JSON live resource manifest
	LiveState string `json:"liveState,omitempty" protobuf:"bytes,6,opt,name=liveState"`
	// Diff contains the JSON patch between target and live resource
	// Deprecated: use NormalizedLiveState and PredictedLiveState to render the difference
	Diff string `json:"diff,omitempty" protobuf:"bytes,7,opt,name=diff"`
	Hook bool   `json:"hook,omitempty" protobuf:"bytes,8,opt,name=hook"`
	// NormalizedLiveState contains JSON serialized live resource state with applied normalizations
	NormalizedLiveState string `json:"normalizedLiveState,omitempty" protobuf:"bytes,9,opt,name=normalizedLiveState"`
	// PredictedLiveState contains JSON serialized resource state that is calculated based on normalized and target resource state
	PredictedLiveState string `json:"predictedLiveState,omitempty" protobuf:"bytes,10,opt,name=predictedLiveState"`
	ResourceVersion    string `json:"resourceVersion,omitempty" protobuf:"bytes,11,opt,name=resourceVersion"`
	Modified           bool   `json:"modified,omitempty" protobuf:"bytes,12,opt,name=modified"`
}

// ConnectionStatus represents the status indicator for a connection to a remote resource
type ConnectionStatus = string

const (
	// ConnectionStatusSuccessful indicates that a connection has been successfully established
	ConnectionStatusSuccessful = "Successful"
	// ConnectionStatusFailed indicates that a connection attempt has failed
	ConnectionStatusFailed = "Failed"
	// ConnectionStatusUnknown indicates that the connection status could not be reliably determined
	ConnectionStatusUnknown = "Unknown"
)

// ConnectionState contains information about remote resource connection state, currently used for clusters and repositories
type ConnectionState struct {
	// Status contains the current status indicator for the connection
	Status ConnectionStatus `json:"status" protobuf:"bytes,1,opt,name=status"`
	// Message contains human readable information about the connection status
	Message string `json:"message" protobuf:"bytes,2,opt,name=message"`
	// ModifiedAt contains the timestamp when this connection status has been determined
	ModifiedAt *metav1.Time `json:"attemptedAt" protobuf:"bytes,3,opt,name=attemptedAt"`
}

// Cluster is the definition of a cluster resource
type Cluster struct {
	// ID is an internal field cluster identifier. Not exposed via API.
	ID string `json:"-"`
	// Server is the API server URL of the Kubernetes cluster
	Server string `json:"server" protobuf:"bytes,1,opt,name=server"`
	// Name of the cluster. If omitted, will use the server address
	Name string `json:"name" protobuf:"bytes,2,opt,name=name"`
	// Config holds cluster information for connecting to a cluster
	Config ClusterConfig `json:"config" protobuf:"bytes,3,opt,name=config"`
	// DEPRECATED: use Info.ConnectionState field instead.
	// ConnectionState contains information about cluster connection state
	ConnectionState ConnectionState `json:"connectionState,omitempty" protobuf:"bytes,4,opt,name=connectionState"`
	// DEPRECATED: use Info.ServerVersion field instead.
	// The server version
	ServerVersion string `json:"serverVersion,omitempty" protobuf:"bytes,5,opt,name=serverVersion"`
	// Holds list of namespaces which are accessible in that cluster. Cluster level resources will be ignored if namespace list is not empty.
	Namespaces []string `json:"namespaces,omitempty" protobuf:"bytes,6,opt,name=namespaces"`
	// RefreshRequestedAt holds time when cluster cache refresh has been requested
	RefreshRequestedAt *metav1.Time `json:"refreshRequestedAt,omitempty" protobuf:"bytes,7,opt,name=refreshRequestedAt"`
	// Info holds information about cluster cache and state
	Info ClusterInfo `json:"info,omitempty" protobuf:"bytes,8,opt,name=info"`
	// Shard contains optional shard number. Calculated on the fly by the application controller if not specified.
	Shard *int64 `json:"shard,omitempty" protobuf:"bytes,9,opt,name=shard"`
	// Indicates if cluster level resources should be managed. This setting is used only if cluster is connected in a namespaced mode.
	ClusterResources bool `json:"clusterResources,omitempty" protobuf:"bytes,10,opt,name=clusterResources"`
	// Reference between project and cluster that allow you automatically to be added as item inside Destinations project entity
	Project string `json:"project,omitempty" protobuf:"bytes,11,opt,name=project"`
	// Labels for cluster secret metadata
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,12,opt,name=labels"`
	// Annotations for cluster secret metadata
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,13,opt,name=annotations"`
}

// ClusterInfo contains information about the cluster
type ClusterInfo struct {
	// ConnectionState contains information about the connection to the cluster
	ConnectionState ConnectionState `json:"connectionState,omitempty" protobuf:"bytes,1,opt,name=connectionState"`
	// ServerVersion contains information about the Kubernetes version of the cluster
	ServerVersion string `json:"serverVersion,omitempty" protobuf:"bytes,2,opt,name=serverVersion"`
	// CacheInfo contains information about the cluster cache
	CacheInfo ClusterCacheInfo `json:"cacheInfo,omitempty" protobuf:"bytes,3,opt,name=cacheInfo"`
	// ApplicationsCount is the number of applications managed by Argo CD on the cluster
	ApplicationsCount int64 `json:"applicationsCount" protobuf:"bytes,4,opt,name=applicationsCount"`
	// APIVersions contains list of API versions supported by the cluster
	APIVersions []string `json:"apiVersions,omitempty" protobuf:"bytes,5,opt,name=apiVersions"`
}

// ClusterCacheInfo contains information about the cluster cache
type ClusterCacheInfo struct {
	// ResourcesCount holds number of observed Kubernetes resources
	ResourcesCount int64 `json:"resourcesCount,omitempty" protobuf:"bytes,1,opt,name=resourcesCount"`
	// APIsCount holds number of observed Kubernetes API count
	APIsCount int64 `json:"apisCount,omitempty" protobuf:"bytes,2,opt,name=apisCount"`
	// LastCacheSyncTime holds time of most recent cache synchronization
	LastCacheSyncTime *metav1.Time `json:"lastCacheSyncTime,omitempty" protobuf:"bytes,3,opt,name=lastCacheSyncTime"`
}

// ClusterList is a collection of Clusters.
type ClusterList struct {
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Cluster `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// AWSAuthConfig is an AWS IAM authentication configuration
type AWSAuthConfig struct {
	// ClusterName contains AWS cluster name
	ClusterName string `json:"clusterName,omitempty" protobuf:"bytes,1,opt,name=clusterName"`

	// RoleARN contains optional role ARN. If set then AWS IAM Authenticator assume a role to perform cluster operations instead of the default AWS credential provider chain.
	RoleARN string `json:"roleARN,omitempty" protobuf:"bytes,2,opt,name=roleARN"`

	// Profile contains optional role ARN. If set then AWS IAM Authenticator uses the profile to perform cluster operations instead of the default AWS credential provider chain.
	Profile string `json:"profile,omitempty" protobuf:"bytes,3,opt,name=profile"`
}

// ExecProviderConfig is config used to call an external command to perform cluster authentication
// See: https://godoc.org/k8s.io/client-go/tools/clientcmd/api#ExecConfig
type ExecProviderConfig struct {
	// Command to execute
	Command string `json:"command,omitempty" protobuf:"bytes,1,opt,name=command"`

	// Arguments to pass to the command when executing it
	Args []string `json:"args,omitempty" protobuf:"bytes,2,rep,name=args"`

	// Env defines additional environment variables to expose to the process
	Env map[string]string `json:"env,omitempty" protobuf:"bytes,3,opt,name=env"`

	// Preferred input version of the ExecInfo
	APIVersion string `json:"apiVersion,omitempty" protobuf:"bytes,4,opt,name=apiVersion"`

	// This text is shown to the user when the executable doesn't seem to be present
	InstallHint string `json:"installHint,omitempty" protobuf:"bytes,5,opt,name=installHint"`
}

// ClusterConfig is the configuration attributes. This structure is subset of the go-client
// rest.Config with annotations added for marshaling.
type ClusterConfig struct {
	// Server requires Basic authentication
	Username string `json:"username,omitempty" protobuf:"bytes,1,opt,name=username"`
	Password string `json:"password,omitempty" protobuf:"bytes,2,opt,name=password"`

	// Server requires Bearer authentication. This client will not attempt to use
	// refresh tokens for an OAuth2 flow.
	// TODO: demonstrate an OAuth2 compatible client.
	BearerToken string `json:"bearerToken,omitempty" protobuf:"bytes,3,opt,name=bearerToken"`

	// TLSClientConfig contains settings to enable transport layer security
	TLSClientConfig `json:"tlsClientConfig" protobuf:"bytes,4,opt,name=tlsClientConfig"`

	// AWSAuthConfig contains IAM authentication configuration
	AWSAuthConfig *AWSAuthConfig `json:"awsAuthConfig,omitempty" protobuf:"bytes,5,opt,name=awsAuthConfig"`

	// ExecProviderConfig contains configuration for an exec provider
	ExecProviderConfig *ExecProviderConfig `json:"execProviderConfig,omitempty" protobuf:"bytes,6,opt,name=execProviderConfig"`
}

// TLSClientConfig contains settings to enable transport layer security
type TLSClientConfig struct {
	// Insecure specifies that the server should be accessed without verifying the TLS certificate. For testing only.
	Insecure bool `json:"insecure" protobuf:"bytes,1,opt,name=insecure"`
	// ServerName is passed to the server for SNI and is used in the client to check server
	// certificates against. If ServerName is empty, the hostname used to contact the
	// server is used.
	ServerName string `json:"serverName,omitempty" protobuf:"bytes,2,opt,name=serverName"`
	// CertData holds PEM-encoded bytes (typically read from a client certificate file).
	// CertData takes precedence over CertFile
	CertData []byte `json:"certData,omitempty" protobuf:"bytes,3,opt,name=certData"`
	// KeyData holds PEM-encoded bytes (typically read from a client certificate key file).
	// KeyData takes precedence over KeyFile
	KeyData []byte `json:"keyData,omitempty" protobuf:"bytes,4,opt,name=keyData"`
	// CAData holds PEM-encoded bytes (typically read from a root certificates bundle).
	// CAData takes precedence over CAFile
	CAData []byte `json:"caData,omitempty" protobuf:"bytes,5,opt,name=caData"`
}

// KnownTypeField contains mapping between CRD field and known Kubernetes type.
// This is mainly used for unit conversion in unknown resources (e.g. 0.1 == 100mi)
// TODO: Describe the members of this type
type KnownTypeField struct {
	Field string `json:"field,omitempty" protobuf:"bytes,1,opt,name=field"`
	Type  string `json:"type,omitempty" protobuf:"bytes,2,opt,name=type"`
}

// OverrideIgnoreDiff contains configurations about how fields should be ignored during diffs between
// the desired state and live state
type OverrideIgnoreDiff struct {
	// JSONPointers is a JSON path list following the format defined in RFC4627 (https://datatracker.ietf.org/doc/html/rfc6902#section-3)
	JSONPointers []string `json:"jsonPointers" protobuf:"bytes,1,rep,name=jSONPointers"`
	// JQPathExpressions is a JQ path list that will be evaludated during the diff process
	JQPathExpressions []string `json:"jqPathExpressions" protobuf:"bytes,2,opt,name=jqPathExpressions"`
	// ManagedFieldsManagers is a list of trusted managers. Fields mutated by those managers will take precedence over the
	// desired state defined in the SCM and won't be displayed in diffs
	ManagedFieldsManagers []string `json:"managedFieldsManagers" protobuf:"bytes,3,opt,name=managedFieldsManagers"`
}

// ResourceOverride holds configuration to customize resource diffing and health assessment
// TODO: describe the members of this type
type ResourceOverride struct {
	HealthLua             string             `protobuf:"bytes,1,opt,name=healthLua"`
	UseOpenLibs           bool               `protobuf:"bytes,5,opt,name=useOpenLibs"`
	Actions               string             `protobuf:"bytes,3,opt,name=actions"`
	IgnoreDifferences     OverrideIgnoreDiff `protobuf:"bytes,2,opt,name=ignoreDifferences"`
	IgnoreResourceUpdates OverrideIgnoreDiff `protobuf:"bytes,6,opt,name=ignoreResourceUpdates"`
	KnownTypeFields       []KnownTypeField   `protobuf:"bytes,4,opt,name=knownTypeFields"`
}

// TODO: describe this type
// TODO: describe members of this type
type ResourceActions struct {
	ActionDiscoveryLua string                     `json:"discovery.lua,omitempty" yaml:"discovery.lua,omitempty" protobuf:"bytes,1,opt,name=actionDiscoveryLua"`
	Definitions        []ResourceActionDefinition `json:"definitions,omitempty" protobuf:"bytes,2,rep,name=definitions"`
}

// TODO: describe this type
// TODO: describe members of this type
type ResourceActionDefinition struct {
	Name      string `json:"name" protobuf:"bytes,1,opt,name=name"`
	ActionLua string `json:"action.lua" yaml:"action.lua" protobuf:"bytes,2,opt,name=actionLua"`
}

// TODO: describe this type
// TODO: describe members of this type
type ResourceAction struct {
	Name        string                `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Params      []ResourceActionParam `json:"params,omitempty" protobuf:"bytes,2,rep,name=params"`
	Disabled    bool                  `json:"disabled,omitempty" protobuf:"varint,3,opt,name=disabled"`
	IconClass   string                `json:"iconClass,omitempty" protobuf:"bytes,4,opt,name=iconClass"`
	DisplayName string                `json:"displayName,omitempty" protobuf:"bytes,5,opt,name=displayName"`
}

// TODO: describe this type
// TODO: describe members of this type
type ResourceActionParam struct {
	Name    string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	Value   string `json:"value,omitempty" protobuf:"bytes,2,opt,name=value"`
	Type    string `json:"type,omitempty" protobuf:"bytes,3,opt,name=type"`
	Default string `json:"default,omitempty" protobuf:"bytes,4,opt,name=default"`
}

// OrphanedResourcesMonitorSettings holds settings of orphaned resources monitoring
type OrphanedResourcesMonitorSettings struct {
	// Warn indicates if warning condition should be created for apps which have orphaned resources
	Warn *bool `json:"warn,omitempty" protobuf:"bytes,1,name=warn"`
	// Ignore contains a list of resources that are to be excluded from orphaned resources monitoring
	Ignore []OrphanedResourceKey `json:"ignore,omitempty" protobuf:"bytes,2,opt,name=ignore"`
}

// OrphanedResourceKey is a reference to a resource to be ignored from
type OrphanedResourceKey struct {
	Group string `json:"group,omitempty" protobuf:"bytes,1,opt,name=group"`
	Kind  string `json:"kind,omitempty" protobuf:"bytes,2,opt,name=kind"`
	Name  string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`
}

// SignatureKey is the specification of a key required to verify commit signatures with
type SignatureKey struct {
	// The ID of the key in hexadecimal notation
	KeyID string `json:"keyID" protobuf:"bytes,1,name=keyID"`
}

// AppProjectSpec is the specification of an AppProject
type AppProjectSpec struct {
	// SourceRepos contains list of repository URLs which can be used for deployment
	SourceRepos []string `json:"sourceRepos,omitempty" protobuf:"bytes,1,name=sourceRepos"`
	// Destinations contains list of destinations available for deployment
	Destinations []ApplicationDestination `json:"destinations,omitempty" protobuf:"bytes,2,name=destination"`
	// Description contains optional project description
	Description string `json:"description,omitempty" protobuf:"bytes,3,opt,name=description"`
	// Roles are user defined RBAC roles associated with this project
	Roles []ProjectRole `json:"roles,omitempty" protobuf:"bytes,4,rep,name=roles"`
	// ClusterResourceWhitelist contains list of whitelisted cluster level resources
	ClusterResourceWhitelist []metav1.GroupKind `json:"clusterResourceWhitelist,omitempty" protobuf:"bytes,5,opt,name=clusterResourceWhitelist"`
	// NamespaceResourceBlacklist contains list of blacklisted namespace level resources
	NamespaceResourceBlacklist []metav1.GroupKind `json:"namespaceResourceBlacklist,omitempty" protobuf:"bytes,6,opt,name=namespaceResourceBlacklist"`
	// OrphanedResources specifies if controller should monitor orphaned resources of apps in this project
	OrphanedResources *OrphanedResourcesMonitorSettings `json:"orphanedResources,omitempty" protobuf:"bytes,7,opt,name=orphanedResources"`
	// SyncWindows controls when syncs can be run for apps in this project
	SyncWindows SyncWindows `json:"syncWindows,omitempty" protobuf:"bytes,8,opt,name=syncWindows"`
	// NamespaceResourceWhitelist contains list of whitelisted namespace level resources
	NamespaceResourceWhitelist []metav1.GroupKind `json:"namespaceResourceWhitelist,omitempty" protobuf:"bytes,9,opt,name=namespaceResourceWhitelist"`
	// SignatureKeys contains a list of PGP key IDs that commits in Git must be signed with in order to be allowed for sync
	SignatureKeys []SignatureKey `json:"signatureKeys,omitempty" protobuf:"bytes,10,opt,name=signatureKeys"`
	// ClusterResourceBlacklist contains list of blacklisted cluster level resources
	ClusterResourceBlacklist []metav1.GroupKind `json:"clusterResourceBlacklist,omitempty" protobuf:"bytes,11,opt,name=clusterResourceBlacklist"`
	// SourceNamespaces defines the namespaces application resources are allowed to be created in
	SourceNamespaces []string `json:"sourceNamespaces,omitempty" protobuf:"bytes,12,opt,name=sourceNamespaces"`
	// PermitOnlyProjectScopedClusters determines whether destinations can only reference clusters which are project-scoped
	PermitOnlyProjectScopedClusters bool `json:"permitOnlyProjectScopedClusters,omitempty" protobuf:"bytes,13,opt,name=permitOnlyProjectScopedClusters"`
}

// SyncWindows is a collection of sync windows in this project
type SyncWindows []*SyncWindow

// SyncWindow contains the kind, time, duration and attributes that are used to assign the syncWindows to apps
type SyncWindow struct {
	// Kind defines if the window allows or blocks syncs
	Kind string `json:"kind,omitempty" protobuf:"bytes,1,opt,name=kind"`
	// Schedule is the time the window will begin, specified in cron format
	Schedule string `json:"schedule,omitempty" protobuf:"bytes,2,opt,name=schedule"`
	// Duration is the amount of time the sync window will be open
	Duration string `json:"duration,omitempty" protobuf:"bytes,3,opt,name=duration"`
	// Applications contains a list of applications that the window will apply to
	Applications []string `json:"applications,omitempty" protobuf:"bytes,4,opt,name=applications"`
	// Namespaces contains a list of namespaces that the window will apply to
	Namespaces []string `json:"namespaces,omitempty" protobuf:"bytes,5,opt,name=namespaces"`
	// Clusters contains a list of clusters that the window will apply to
	Clusters []string `json:"clusters,omitempty" protobuf:"bytes,6,opt,name=clusters"`
	// ManualSync enables manual syncs when they would otherwise be blocked
	ManualSync bool `json:"manualSync,omitempty" protobuf:"bytes,7,opt,name=manualSync"`
	// TimeZone of the sync that will be applied to the schedule
	TimeZone string `json:"timeZone,omitempty" protobuf:"bytes,8,opt,name=timeZone"`
}

// ProjectRole represents a role that has access to a project
type ProjectRole struct {
	// Name is a name for this role
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// Description is a description of the role
	Description string `json:"description,omitempty" protobuf:"bytes,2,opt,name=description"`
	// Policies Stores a list of casbin formatted strings that define access policies for the role in the project
	Policies []string `json:"policies,omitempty" protobuf:"bytes,3,rep,name=policies"`
	// JWTTokens are a list of generated JWT tokens bound to this role
	JWTTokens []JWTToken `json:"jwtTokens,omitempty" protobuf:"bytes,4,rep,name=jwtTokens"`
	// Groups are a list of OIDC group claims bound to this role
	Groups []string `json:"groups,omitempty" protobuf:"bytes,5,rep,name=groups"`
}

// JWTToken holds the issuedAt and expiresAt values of a token
type JWTToken struct {
	IssuedAt  int64  `json:"iat" protobuf:"int64,1,opt,name=iat"`
	ExpiresAt int64  `json:"exp,omitempty" protobuf:"int64,2,opt,name=exp"`
	ID        string `json:"id,omitempty" protobuf:"bytes,3,opt,name=id"`
}

// Command holds binary path and arguments list
type Command struct {
	Command []string `json:"command,omitempty" protobuf:"bytes,1,name=command"`
	Args    []string `json:"args,omitempty" protobuf:"bytes,2,rep,name=args"`
}

// ConfigManagementPlugin contains config management plugin configuration
type ConfigManagementPlugin struct {
	Name     string   `json:"name" protobuf:"bytes,1,name=name"`
	Init     *Command `json:"init,omitempty" protobuf:"bytes,2,name=init"`
	Generate Command  `json:"generate" protobuf:"bytes,3,name=generate"`
	LockRepo bool     `json:"lockRepo,omitempty" protobuf:"bytes,4,name=lockRepo"`
}

// HelmOptions holds helm options
type HelmOptions struct {
	ValuesFileSchemes []string `protobuf:"bytes,1,opt,name=valuesFileSchemes"`
}

// KustomizeOptions are options for kustomize to use when building manifests
type KustomizeOptions struct {
	// BuildOptions is a string of build parameters to use when calling `kustomize build`
	BuildOptions string `protobuf:"bytes,1,opt,name=buildOptions"`
	// BinaryPath holds optional path to kustomize binary
	BinaryPath string `protobuf:"bytes,2,opt,name=binaryPath"`
}
