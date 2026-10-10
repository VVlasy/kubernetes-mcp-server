package helm

import (
	"fmt"

	"github.com/containers/kubernetes-mcp-server/pkg/helm"
	"github.com/google/jsonschema-go/jsonschema"
	"k8s.io/utils/ptr"

	"github.com/containers/kubernetes-mcp-server/pkg/api"
)

func initHelm() []api.ServerTool {
	return []api.ServerTool{
		{Tool: api.Tool{
			Name:        "helm_install",
			Description: "Install (deploy) a Helm chart to create a release in the current or provided namespace",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"chart": {
						Type:        "string",
						Description: "Chart reference to install (for example: stable/grafana, oci://ghcr.io/nginxinc/charts/nginx-ingress)",
					},
					"values": {
						Type:        "object",
						Description: "Values to pass to the Helm chart (Optional)",
						Properties:  make(map[string]*jsonschema.Schema),
					},
					"name": {
						Type:        "string",
						Description: "Name of the Helm release (Optional, random name if not provided)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to install the Helm chart in (Optional, current namespace if not provided)",
					},
				},
				Required: []string{"chart"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: Install",
				DestructiveHint: ptr.To(false),
				IdempotentHint:  nil, // TODO: consider replacing implementation with equivalent to: helm upgrade --install
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACUnbounded("Permissions depend on resources contained in the referenced Helm chart"), Handler: helmInstall},
		{Tool: api.Tool{
			Name:        "helm_list",
			Description: "List all the Helm releases in the current or provided namespace (or in all namespaces if specified)",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"namespace": {
						Type:        "string",
						Description: "Namespace to list Helm releases from (Optional, all namespaces if not provided)",
					},
					"all_namespaces": {
						Type:        "boolean",
						Description: "If true, lists all Helm releases in all namespaces ignoring the namespace argument (Optional)",
					},
				},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: List",
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(
			api.RBACRequirement{
				Verbs:     []string{"list"},
				Target:    api.RBACTarget{Resource: &api.RBACResourceTarget{Resource: "secrets"}},
				Namespace: &api.RBACNamespace{AllNamespaces: true},
			},
			api.RBACRequirement{
				Verbs:     []string{"list"},
				Target:    api.RBACTarget{Resource: &api.RBACResourceTarget{Resource: "configmaps"}},
				Namespace: &api.RBACNamespace{AllNamespaces: true},
			},
		), Handler: helmList},
		{Tool: api.Tool{
			Name:        "helm_uninstall",
			Description: "Uninstall a Helm release in the current or provided namespace",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "Name of the Helm release to uninstall",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace to uninstall the Helm release from (Optional, current namespace if not provided)",
					},
				},
				Required: []string{"name"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: Uninstall",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(true),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACUnbounded("Permissions depend on resources contained in the installed Helm release"), Handler: helmUninstall},
		{Tool: api.Tool{
			Name: "helm_upgrade",
			Description: "Upgrade an existing Helm release to a chart version and/or new values in the current or provided namespace. " +
				"By default the release's current values are kept and the provided values are merged over them (like helm upgrade --reuse-values)",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "Name of the Helm release to upgrade",
					},
					"chart": {
						Type:        "string",
						Description: "Chart reference to upgrade to (for example: stable/grafana, oci://ghcr.io/nginxinc/charts/nginx-ingress, https://example.com/charts/app-1.2.3.tgz)",
					},
					"version": {
						Type:        "string",
						Description: "Chart version to upgrade to, for repository and OCI references (Optional, latest if not provided)",
					},
					"values": {
						Type:        "object",
						Description: "Values to set on the release (Optional). Merged over the current values unless reuse_values is false",
						Properties:  make(map[string]*jsonschema.Schema),
					},
					"reuse_values": {
						Type:        "boolean",
						Description: "Keep the release's current values and merge the provided values over them (Optional, default true). If false, only the chart defaults plus the provided values are used",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace of the Helm release (Optional, current namespace if not provided)",
					},
				},
				Required: []string{"name", "chart"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: Upgrade",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACUnbounded("Permissions depend on resources contained in the referenced Helm chart"), Handler: helmUpgrade},
		{Tool: api.Tool{
			Name:        "helm_rollback",
			Description: "Roll back a Helm release to a previous revision in the current or provided namespace",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "Name of the Helm release to roll back",
					},
					"revision": {
						Type:        "integer",
						Description: "Revision to roll back to (Optional, the previous revision if not provided). See helm_history",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace of the Helm release (Optional, current namespace if not provided)",
					},
				},
				Required: []string{"name"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: Rollback",
				DestructiveHint: ptr.To(true),
				IdempotentHint:  ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACUnbounded("Permissions depend on resources contained in the Helm release"), Handler: helmRollback},
		{Tool: api.Tool{
			Name:        "helm_history",
			Description: "List the revisions of a Helm release (chart version, status and description of each) in the current or provided namespace",
			InputSchema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"name": {
						Type:        "string",
						Description: "Name of the Helm release",
					},
					"max": {
						Type:        "integer",
						Description: "Maximum number of most recent revisions to return (Optional, all if not provided)",
					},
					"namespace": {
						Type:        "string",
						Description: "Namespace of the Helm release (Optional, current namespace if not provided)",
					},
				},
				Required: []string{"name"},
			},
			Annotations: api.ToolAnnotations{
				Title:           "Helm: History",
				ReadOnlyHint:    ptr.To(true),
				DestructiveHint: ptr.To(false),
				OpenWorldHint:   ptr.To(true),
			},
		}, RBAC: api.RBACBounded(
			api.RBACRequirement{
				Verbs:  []string{"list"},
				Target: api.RBACTarget{Resource: &api.RBACResourceTarget{Resource: "secrets"}},
			},
		), Handler: helmHistory},
	}
}

func newHelmClient(params api.ToolHandlerParams) *helm.Helm {
	var cfg *helm.Config
	if c, ok := params.GetToolsetConfig("helm"); ok {
		if hc, ok := c.(*helm.Config); ok {
			cfg = hc
		}
	}
	return helm.NewHelm(params, cfg)
}

func helmInstall(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	var chart string
	ok := false
	if chart, ok = params.GetArguments()["chart"].(string); !ok {
		return api.NewToolCallResult("", fmt.Errorf("failed to install helm chart, missing argument chart")), nil
	}
	values := map[string]interface{}{}
	if v, ok := params.GetArguments()["values"].(map[string]interface{}); ok {
		values = v
	}
	name := ""
	if v, ok := params.GetArguments()["name"].(string); ok {
		name = v
	}
	namespace := ""
	if v, ok := params.GetArguments()["namespace"].(string); ok {
		namespace = v
	}
	ret, err := newHelmClient(params).Install(params, chart, values, name, namespace)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to install helm chart '%s': %w", chart, err)), nil
	}
	return api.NewToolCallResult(ret, err), nil
}

func helmList(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	allNamespaces := p.OptionalBool("all_namespaces", false)
	namespace := p.OptionalString("namespace", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list helm releases: %w", err)), nil
	}
	ret, err := newHelmClient(params).List(params.Context, namespace, allNamespaces)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to list helm releases in namespace '%s': %w", namespace, err)), nil
	}
	return api.NewToolCallResult(ret, err), nil
}

func helmUninstall(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	var name string
	ok := false
	if name, ok = params.GetArguments()["name"].(string); !ok {
		return api.NewToolCallResult("", fmt.Errorf("failed to uninstall helm chart, missing argument name")), nil
	}
	namespace := ""
	if v, ok := params.GetArguments()["namespace"].(string); ok {
		namespace = v
	}
	ret, err := newHelmClient(params).Uninstall(params.Context, name, namespace)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to uninstall helm chart '%s': %w", name, err)), nil
	}
	return api.NewToolCallResult(ret, err), nil
}

func helmUpgrade(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	name := p.RequiredString("name")
	chart := p.RequiredString("chart")
	version := p.OptionalString("version", "")
	reuseValues := p.OptionalBool("reuse_values", true)
	namespace := p.OptionalString("namespace", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to upgrade helm release: %w", err)), nil
	}
	values := map[string]interface{}{}
	if v, ok := params.GetArguments()["values"].(map[string]interface{}); ok {
		values = v
	}
	ret, err := newHelmClient(params).Upgrade(params, name, chart, version, values, namespace, reuseValues)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to upgrade helm release '%s': %w", name, err)), nil
	}
	return api.NewToolCallResult(ret, nil), nil
}

func helmRollback(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	name := p.RequiredString("name")
	revision := p.OptionalInt64("revision", 0)
	namespace := p.OptionalString("namespace", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to roll back helm release: %w", err)), nil
	}
	if revision < 0 {
		return api.NewToolCallResult("", fmt.Errorf("failed to roll back helm release: revision must not be negative")), nil
	}
	ret, err := newHelmClient(params).Rollback(params.Context, name, int(revision), namespace)
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to roll back helm release '%s': %w", name, err)), nil
	}
	return api.NewToolCallResult(ret, nil), nil
}

func helmHistory(params api.ToolHandlerParams) (*api.ToolCallResult, error) {
	p := api.WrapParams(params)
	name := p.RequiredString("name")
	max := p.OptionalInt64("max", 0)
	namespace := p.OptionalString("namespace", "")
	if err := p.Err(); err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get helm release history: %w", err)), nil
	}
	ret, err := newHelmClient(params).History(params.Context, name, namespace, int(max))
	if err != nil {
		return api.NewToolCallResult("", fmt.Errorf("failed to get helm release history '%s': %w", name, err)), nil
	}
	return api.NewToolCallResult(ret, nil), nil
}
