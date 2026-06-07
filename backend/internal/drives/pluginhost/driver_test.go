package pluginhost

import (
	"reflect"
	"testing"
)

func TestConfigFromCredentialsParsesArgsAndParams(t *testing.T) {
	cfg, err := ConfigFromCredentials("drive-id", "root-id", map[string]string{
		"command":     "/opt/plugins/static-drive",
		"args":        `["--config","/opt/drive config.json"]`,
		"plugin_kind": "staticdrive",
		"token":       "secret",
		"params_json": `{"url":"https://media.example/video.mp4","size":123}`,
	})
	if err != nil {
		t.Fatalf("ConfigFromCredentials: %v", err)
	}
	if cfg.ID != "drive-id" || cfg.RootID != "root-id" || cfg.Kind != "staticdrive" || cfg.Command != "/opt/plugins/static-drive" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"--config", "/opt/drive config.json"}) {
		t.Fatalf("args = %#v", cfg.Args)
	}
	wantParams := map[string]string{
		"token": "secret",
		"url":   "https://media.example/video.mp4",
		"size":  "123",
	}
	if !reflect.DeepEqual(cfg.Params, wantParams) {
		t.Fatalf("params = %#v, want %#v", cfg.Params, wantParams)
	}
}

func TestConfigFromCredentialsUsesPluginReferenceWithoutCommand(t *testing.T) {
	cfg, err := ConfigFromCredentials("drive-id", "root-id", map[string]string{
		"plugin_kind": "staticdrive",
		"params_json": `{"url":"https://media.example/video.mp4"}`,
	})
	if err != nil {
		t.Fatalf("ConfigFromCredentials: %v", err)
	}
	if cfg.Plugin != "staticdrive" {
		t.Fatalf("plugin = %q, want staticdrive", cfg.Plugin)
	}
	if cfg.Command != "" {
		t.Fatalf("command = %q, want empty", cfg.Command)
	}
}

func TestSplitArgsSupportsQuotedValues(t *testing.T) {
	got, err := splitArgs(`--name "drive one" --path 'C:\media files'`)
	if err != nil {
		t.Fatalf("splitArgs: %v", err)
	}
	want := []string{"--name", "drive one", "--path", `C:\media files`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
