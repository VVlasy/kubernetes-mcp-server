package mcp

import (
	"context"
	"encoding/base64"
	"flag"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/containers/kubernetes-mcp-server/internal/test"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/suite"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/yaml"
)

type HelmSuite struct {
	BaseMcpSuite
	klogState klog.State
	logBuffer test.SyncBuffer
}

func (s *HelmSuite) SetupTest() {
	s.BaseMcpSuite.SetupTest()
	s.Cfg.Toolsets = append(s.Cfg.Toolsets, "helm")
	clearHelmReleases(s.T().Context(), kubernetes.NewForConfigOrDie(test.EnvTestRestConfig()))

	// Capture log output to verify denied resource messages
	s.klogState = klog.CaptureState()
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	klog.InitFlags(flags)
	_ = flags.Set("v", strconv.Itoa(5))
	klog.SetLogger(textlogger.NewLogger(textlogger.NewConfig(textlogger.Verbosity(5), textlogger.Output(&s.logBuffer))))
}

func (s *HelmSuite) TearDownTest() {
	s.BaseMcpSuite.TearDownTest()
	s.klogState.Restore()
}

func (s *HelmSuite) TestHelmInstall() {
	s.InitMcpClient()
	s.Run("helm_install(chart=helm-chart-no-op)", func() {
		_, file, _, _ := runtime.Caller(0)
		chartPath := filepath.Join(filepath.Dir(file), "testdata", "helm-chart-no-op")
		toolResult, err := s.CallTool("helm_install", map[string]interface{}{
			"chart": chartPath,
		})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns installed chart", func() {
			var decoded []map[string]interface{}
			err = yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded)
			s.Run("has yaml content", func() {
				s.Nilf(err, "invalid tool result content %v", err)
			})
			s.Run("has 1 item", func() {
				s.Lenf(decoded, 1, "invalid helm install count, expected 1, got %v", len(decoded))
			})
			s.Run("has valid name", func() {
				s.Truef(strings.HasPrefix(decoded[0]["name"].(string), "helm-chart-no-op-"), "invalid helm install name, expected no-op-*, got %v", decoded[0]["name"])
			})
			s.Run("has valid namespace", func() {
				s.Equalf("default", decoded[0]["namespace"], "invalid helm install namespace, expected default, got %v", decoded[0]["namespace"])
			})
			s.Run("has valid chart", func() {
				s.Equalf("no-op", decoded[0]["chart"], "invalid helm install name, expected release name, got empty")
			})
			s.Run("has valid chartVersion", func() {
				s.Equalf("1.33.7", decoded[0]["chartVersion"], "invalid helm install version, expected 1.33.7, got empty")
			})
			s.Run("has valid status", func() {
				s.Equalf("deployed", decoded[0]["status"], "invalid helm install status, expected deployed, got %v", decoded[0]["status"])
			})
			s.Run("has valid revision", func() {
				s.Equalf(float64(1), decoded[0]["revision"], "invalid helm install revision, expected 1, got %v", decoded[0]["revision"])
			})
		})
	})
}

func (s *HelmSuite) TestHelmInstallDenied() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		denied_resources = [ { version = "v1", kind = "Secret" } ]
	`), s.Cfg), "Expected to parse denied resources config")
	s.InitMcpClient()
	s.Run("helm_install(chart=helm-chart-secret, denied)", func() {
		capture := s.StartCapturingLogNotifications()
		_, file, _, _ := runtime.Caller(0)
		chartPath := filepath.Join(filepath.Dir(file), "testdata", "helm-chart-secret")
		toolResult, err := s.CallTool("helm_install", map[string]interface{}{
			"chart": chartPath,
		})
		s.Run("has error", func() {
			s.Truef(toolResult.IsError, "call tool should fail")
			s.Nilf(err, "call tool should not return error object")
		})
		s.Run("describes denial", func() {
			msg := toolResult.Content[0].(*mcp.TextContent).Text
			s.Contains(msg, "resource not allowed:")
			s.Truef(strings.HasPrefix(msg, "failed to install helm chart"), "expected descriptive error, got %v", msg)
			expectedMessage := ": resource not allowed: /v1, Kind=Secret"
			s.Truef(strings.HasSuffix(msg, expectedMessage), "expected descriptive error '%s', got %v", expectedMessage, msg)
		})
		s.Run("does not send log notification for non-K8s error", func() {
			capture.RequireNoLogNotification(s.T(), 500*time.Millisecond)
		})
	})
}

func (s *HelmSuite) TestHelmListNoReleases() {
	s.InitMcpClient()
	s.Run("helm_list() with no releases", func() {
		toolResult, err := s.CallTool("helm_list", map[string]interface{}{})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns not found", func() {
			s.Equalf("No Helm releases found", toolResult.Content[0].(*mcp.TextContent).Text, "unexpected result %v", toolResult.Content[0].(*mcp.TextContent).Text)
		})
	})
}

func (s *HelmSuite) TestHelmList() {
	kc := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := kc.CoreV1().Secrets("default").Create(s.T().Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sh.helm.release.v1.release-to-list",
			Labels: map[string]string{"owner": "helm", "name": "release-to-list"},
		},
		Data: map[string][]byte{
			"release": []byte(base64.StdEncoding.EncodeToString([]byte("{" +
				"\"name\":\"release-to-list\"," +
				"\"info\":{\"status\":\"deployed\"}" +
				"}"))),
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
	s.Run("helm_list() with deployed release", func() {
		toolResult, err := s.CallTool("helm_list", map[string]interface{}{})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns release", func() {
			var decoded []map[string]interface{}
			err = yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded)
			s.Run("has yaml content", func() {
				s.Nilf(err, "invalid tool result content %v", err)
			})
			s.Run("has 1 item", func() {
				s.Lenf(decoded, 1, "invalid helm list count, expected 1, got %v", len(decoded))
			})
			s.Run("has valid name", func() {
				s.Equalf("release-to-list", decoded[0]["name"], "invalid helm list name, expected release-to-list, got %v", decoded[0]["name"])
			})
			s.Run("has valid status", func() {
				s.Equalf("deployed", decoded[0]["status"], "invalid helm list status, expected deployed, got %v", decoded[0]["status"])
			})
		})
	})
	s.Run("helm_list(namespace=ns-1) with deployed release in other namespaces", func() {
		toolResult, err := s.CallTool("helm_list", map[string]interface{}{"namespace": "ns-1"})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns not found", func() {
			s.Equalf("No Helm releases found", toolResult.Content[0].(*mcp.TextContent).Text, "unexpected result %v", toolResult.Content[0].(*mcp.TextContent).Text)
		})
	})
	s.Run("helm_list(namespace=ns-1, all_namespaces=true) with deployed release in all namespaces", func() {
		toolResult, err := s.CallTool("helm_list", map[string]interface{}{"namespace": "ns-1", "all_namespaces": true})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns release", func() {
			var decoded []map[string]interface{}
			err = yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded)
			s.Run("has yaml content", func() {
				s.Nilf(err, "invalid tool result content %v", err)
			})
			s.Run("has 1 item", func() {
				s.Lenf(decoded, 1, "invalid helm list count, expected 1, got %v", len(decoded))
			})
			s.Run("has valid name", func() {
				s.Equalf("release-to-list", decoded[0]["name"], "invalid helm list name, expected release-to-list, got %v", decoded[0]["name"])
			})
			s.Run("has valid status", func() {
				s.Equalf("deployed", decoded[0]["status"], "invalid helm list status, expected deployed, got %v", decoded[0]["status"])
			})
		})
	})
}

func (s *HelmSuite) TestHelmListDenied() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		denied_resources = [ { version = "v1", kind = "Secret" } ]
	`), s.Cfg), "Expected to parse denied resources config")
	kc := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := kc.CoreV1().Secrets("default").Create(s.T().Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sh.helm.release.v1.release-to-list-denied",
			Labels: map[string]string{"owner": "helm", "name": "release-to-list-denied"},
		},
		Data: map[string][]byte{
			"release": []byte(base64.StdEncoding.EncodeToString([]byte("{" +
				"\"name\":\"release-to-list-denied\"," +
				"\"info\":{\"status\":\"deployed\"}" +
				"}"))),
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
	s.Run("helm_list() with deployed release (denied)", func() {
		toolResult, err := s.CallTool("helm_list", map[string]interface{}{})
		s.Run("has error", func() {
			s.Truef(toolResult.IsError, "call tool should fail")
			s.Nilf(err, "call tool should not return error object")
		})
		s.Run("describes denial", func() {
			msg := toolResult.Content[0].(*mcp.TextContent).Text
			s.Contains(msg, "resource not allowed:")
			s.Truef(strings.HasPrefix(msg, "failed to list helm releases"), "expected descriptive error, got %v", msg)
			expectedMessage := ": resource not allowed: /v1, Kind=Secret"
			s.Truef(strings.HasSuffix(msg, expectedMessage), "expected descriptive error '%s', got %v", expectedMessage, msg)
		})
	})
}

func (s *HelmSuite) TestHelmUninstallNoReleases() {
	s.InitMcpClient()
	s.Run("helm_uninstall(name=release-to-uninstall) with no releases", func() {
		toolResult, err := s.CallTool("helm_uninstall", map[string]interface{}{
			"name": "release-to-uninstall",
		})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns not found", func() {
			s.Equalf("Release release-to-uninstall not found", toolResult.Content[0].(*mcp.TextContent).Text, "unexpected result %v", toolResult.Content[0].(*mcp.TextContent).Text)
		})
	})
}

func (s *HelmSuite) TestHelmUninstall() {
	kc := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := kc.CoreV1().Secrets("default").Create(s.T().Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sh.helm.release.v1.existent-release-to-uninstall.v0",
			Labels: map[string]string{"owner": "helm", "name": "existent-release-to-uninstall"},
		},
		Data: map[string][]byte{
			"release": []byte(base64.StdEncoding.EncodeToString([]byte("{" +
				"\"name\":\"existent-release-to-uninstall\"," +
				"\"info\":{\"status\":\"deployed\"}" +
				"}"))),
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
	s.Run("helm_uninstall(name=existent-release-to-uninstall) with deployed release", func() {
		toolResult, err := s.CallTool("helm_uninstall", map[string]interface{}{
			"name": "existent-release-to-uninstall",
		})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed")
		})
		s.Run("returns uninstalled", func() {
			s.Truef(strings.HasPrefix(toolResult.Content[0].(*mcp.TextContent).Text, "Uninstalled release existent-release-to-uninstall"), "unexpected result %v", toolResult.Content[0].(*mcp.TextContent).Text)
			_, err = kc.CoreV1().Secrets("default").Get(s.T().Context(), "sh.helm.release.v1.existent-release-to-uninstall.v0", metav1.GetOptions{})
			s.Truef(errors.IsNotFound(err), "expected release to be deleted, but it still exists")
		})

	})
}

func (s *HelmSuite) TestHelmUninstallDenied() {
	s.Require().NoError(toml.Unmarshal([]byte(`
		denied_resources = [ { version = "v1", kind = "ConfigMap" } ]
	`), s.Cfg), "Expected to parse denied resources config")
	kc := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_, err := kc.CoreV1().Secrets("default").Create(s.T().Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "sh.helm.release.v1.existent-release-to-uninstall.v0",
			Labels: map[string]string{"owner": "helm", "name": "existent-release-to-uninstall"},
		},
		Data: map[string][]byte{
			"release": []byte(base64.StdEncoding.EncodeToString([]byte("{" +
				"\"name\":\"existent-release-to-uninstall\"," +
				"\"info\":{\"status\":\"deployed\"}," +
				"\"manifest\":\"apiVersion: v1\\nkind: ConfigMap\\nmetadata:\\n  name: config-map-to-deny\\n  namespace: default\\n\"" +
				"}"))),
		},
	}, metav1.CreateOptions{})
	s.Require().NoError(err)
	s.InitMcpClient()
	s.Run("helm_uninstall(name=existent-release-to-uninstall) with deployed release (denied)", func() {
		toolResult, err := s.CallTool("helm_uninstall", map[string]interface{}{
			"name": "existent-release-to-uninstall",
		})
		s.Run("has error", func() {
			s.Truef(toolResult.IsError, "call tool should fail")
			s.Nilf(err, "call tool should not return error object")
		})
		s.Run("describes failure to uninstall", func() {
			s.Contains(toolResult.Content[0].(*mcp.TextContent).Text,
				"failed to uninstall helm chart 'existent-release-to-uninstall': failed to delete release: existent-release-to-uninstall")
		})
		s.Run("describes denial (in log)", func() {
			msg := s.logBuffer.String()
			s.Contains(msg, "resource not allowed:")
			expectedMessage := "uninstall: Failed to delete release:(.+:)? resource not allowed: /v1, Kind=ConfigMap"
			s.Regexpf(expectedMessage, msg,
				"expected descriptive error '%s', got %v", expectedMessage, msg)
		})
	})
}

func (s *HelmSuite) TestHelmListForbidden() {
	s.InitMcpClient()
	defer restoreAuth(s.T().Context())
	client := kubernetes.NewForConfigOrDie(test.EnvTestRestConfig())
	_ = client.RbacV1().ClusterRoles().Delete(s.T().Context(), "allow-all", metav1.DeleteOptions{})

	s.Run("helm_list (forbidden)", func() {
		capture := s.StartCapturingLogNotifications()
		toolResult, _ := s.CallTool("helm_list", map[string]interface{}{})
		s.Run("returns error", func() {
			s.Truef(toolResult.IsError, "call tool should fail")
			s.Contains(toolResult.Content[0].(*mcp.TextContent).Text, "forbidden",
				"error message should indicate forbidden")
		})
		s.Run("sends log notification", func() {
			logNotification := capture.RequireLogNotification(s.T(), 2*time.Second)
			s.Equal("error", logNotification.Level, "forbidden errors should log at error level")
			s.Contains(logNotification.Data, "Permission denied", "log message should indicate permission denied")
		})
	})
}

func (s *HelmSuite) installNoOp(name string) string {
	_, file, _, _ := runtime.Caller(0)
	chartPath := filepath.Join(filepath.Dir(file), "testdata", "helm-chart-no-op")
	toolResult, err := s.CallTool("helm_install", map[string]interface{}{
		"chart":  chartPath,
		"name":   name,
		"values": map[string]interface{}{"keep": "original", "replace": "old"},
	})
	s.Require().NoError(err)
	s.Require().Falsef(toolResult.IsError, "install failed: %v", toolResult.Content)
	return chartPath
}

func (s *HelmSuite) releaseValues(name string, revision int) map[string]interface{} {
	secrets := driver.NewSecrets(kubernetes.NewForConfigOrDie(test.EnvTestRestConfig()).CoreV1().Secrets("default"))
	rel, err := secrets.Get("sh.helm.release.v1." + name + ".v" + strconv.Itoa(revision))
	s.Require().NoError(err)
	return rel.Config
}

func (s *HelmSuite) TestHelmUpgrade() {
	s.InitMcpClient()
	chartPath := s.installNoOp("release-to-upgrade")
	s.Run("helm_upgrade(name=release-to-upgrade) merges values over the current ones", func() {
		toolResult, err := s.CallTool("helm_upgrade", map[string]interface{}{
			"name":   "release-to-upgrade",
			"chart":  chartPath,
			"values": map[string]interface{}{"replace": "new", "added": "yes"},
		})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed: %v", toolResult.Content)
		})
		s.Run("returns upgraded release at revision 2", func() {
			var decoded []map[string]interface{}
			s.Require().NoError(yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded))
			s.Require().Len(decoded, 1)
			s.Equal("release-to-upgrade", decoded[0]["name"])
			s.Equal(float64(2), decoded[0]["revision"])
			s.Equal("deployed", decoded[0]["status"])
		})
		s.Run("keeps unchanged values", func() {
			s.Equal(map[string]interface{}{"keep": "original", "replace": "new", "added": "yes"},
				s.releaseValues("release-to-upgrade", 2))
		})
	})
	s.Run("helm_upgrade(reuse_values=false) replaces the values", func() {
		toolResult, err := s.CallTool("helm_upgrade", map[string]interface{}{
			"name":         "release-to-upgrade",
			"chart":        chartPath,
			"values":       map[string]interface{}{"only": "this"},
			"reuse_values": false,
		})
		s.Nilf(err, "call tool failed %v", err)
		s.Falsef(toolResult.IsError, "call tool failed: %v", toolResult.Content)
		s.Equal(map[string]interface{}{"only": "this"}, s.releaseValues("release-to-upgrade", 3))
	})
}

func (s *HelmSuite) TestHelmUpgradeMissingRelease() {
	s.InitMcpClient()
	_, file, _, _ := runtime.Caller(0)
	toolResult, err := s.CallTool("helm_upgrade", map[string]interface{}{
		"name":  "not-installed",
		"chart": filepath.Join(filepath.Dir(file), "testdata", "helm-chart-no-op"),
	})
	s.Nilf(err, "call tool should not return error object")
	s.Truef(toolResult.IsError, "upgrading a missing release should fail")
	s.Contains(toolResult.Content[0].(*mcp.TextContent).Text, "failed to upgrade helm release 'not-installed'")
}

func (s *HelmSuite) TestHelmUpgradeBlockedScheme() {
	s.InitMcpClient()
	toolResult, err := s.CallTool("helm_upgrade", map[string]interface{}{
		"name":  "any",
		"chart": "http://example.com/chart.tgz",
	})
	s.Nilf(err, "call tool should not return error object")
	s.Truef(toolResult.IsError, "http charts should be refused")
	s.Contains(toolResult.Content[0].(*mcp.TextContent).Text, "http:// scheme is blocked")
}

func (s *HelmSuite) TestHelmRollbackAndHistory() {
	s.InitMcpClient()
	chartPath := s.installNoOp("release-to-roll-back")
	toolResult, err := s.CallTool("helm_upgrade", map[string]interface{}{
		"name":   "release-to-roll-back",
		"chart":  chartPath,
		"values": map[string]interface{}{"replace": "broken"},
	})
	s.Require().NoError(err)
	s.Require().Falsef(toolResult.IsError, "upgrade failed: %v", toolResult.Content)

	s.Run("helm_rollback(name=release-to-roll-back) returns to the previous revision", func() {
		toolResult, err := s.CallTool("helm_rollback", map[string]interface{}{"name": "release-to-roll-back"})
		s.Run("no error", func() {
			s.Nilf(err, "call tool failed %v", err)
			s.Falsef(toolResult.IsError, "call tool failed: %v", toolResult.Content)
		})
		s.Run("creates revision 3 with revision 1's values", func() {
			var decoded []map[string]interface{}
			s.Require().NoError(yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded))
			s.Require().Len(decoded, 1)
			s.Equal(float64(3), decoded[0]["revision"])
			s.Equal("deployed", decoded[0]["status"])
			s.Equal(map[string]interface{}{"keep": "original", "replace": "old"},
				s.releaseValues("release-to-roll-back", 3))
		})
	})
	s.Run("helm_history(name=release-to-roll-back) lists every revision in order", func() {
		toolResult, err := s.CallTool("helm_history", map[string]interface{}{"name": "release-to-roll-back"})
		s.Nilf(err, "call tool failed %v", err)
		s.Falsef(toolResult.IsError, "call tool failed: %v", toolResult.Content)
		var decoded []map[string]interface{}
		s.Require().NoError(yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded))
		s.Require().Len(decoded, 3)
		for i, rev := range decoded {
			s.Equal(float64(i+1), rev["revision"])
		}
		s.Equal("superseded", decoded[0]["status"])
		s.Equal("Upgrade complete", decoded[1]["description"])
		s.Equal("Rollback to 1", decoded[2]["description"])
		s.Equal("deployed", decoded[2]["status"])
	})
	s.Run("helm_history(max=1) returns only the latest revision", func() {
		toolResult, err := s.CallTool("helm_history", map[string]interface{}{"name": "release-to-roll-back", "max": 1})
		s.Nilf(err, "call tool failed %v", err)
		var decoded []map[string]interface{}
		s.Require().NoError(yaml.Unmarshal([]byte(toolResult.Content[0].(*mcp.TextContent).Text), &decoded))
		s.Require().Len(decoded, 1)
		s.Equal(float64(3), decoded[0]["revision"])
	})
	s.Run("helm_rollback(revision=-1) is refused", func() {
		toolResult, err := s.CallTool("helm_rollback", map[string]interface{}{"name": "release-to-roll-back", "revision": -1})
		s.Nilf(err, "call tool should not return error object")
		s.Truef(toolResult.IsError, "negative revision should fail")
	})
}

func (s *HelmSuite) TestHelmHistoryMissingRelease() {
	s.InitMcpClient()
	toolResult, err := s.CallTool("helm_history", map[string]interface{}{"name": "never-installed"})
	s.Nilf(err, "call tool should not return error object")
	s.Truef(toolResult.IsError, "history of a missing release should fail")
	s.Contains(toolResult.Content[0].(*mcp.TextContent).Text, "release: not found")
}

func clearHelmReleases(ctx context.Context, kc *kubernetes.Clientset) {
	secrets, _ := kc.CoreV1().Secrets("default").List(ctx, metav1.ListOptions{})
	for _, secret := range secrets.Items {
		if strings.HasPrefix(secret.Name, "sh.helm.release.v1.") {
			_ = kc.CoreV1().Secrets("default").Delete(ctx, secret.Name, metav1.DeleteOptions{})
		}
	}
}

func TestHelm(t *testing.T) {
	suite.Run(t, new(HelmSuite))
}
