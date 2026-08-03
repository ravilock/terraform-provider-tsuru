// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/tsuru/go-tsuruclient/pkg/tsuru"
)

func expandServiceManifest(value any) *tsuru.ServiceManifest {
	values, ok := value.([]any)
	if !ok || len(values) == 0 || values[0] == nil {
		return nil
	}

	raw, ok := values[0].(map[string]any)
	if !ok {
		return nil
	}

	manifest := &tsuru.ServiceManifest{
		Enabled:       raw["enabled"].(bool),
		StrictActions: raw["strict_actions"].(bool),
		LegacyCompat:  raw["legacy_compat"].(bool),
		Operations:    []tsuru.ServiceManifestOperation{},
	}

	if operations, ok := raw["operations"].(*schema.Set); ok {
		for _, value := range operations.List() {
			operation := value.(map[string]any)
			manifest.Operations = append(manifest.Operations, tsuru.ServiceManifestOperation{
				Method: normalizeServiceManifestMethod(operation["method"]),
				Path:   operation["path"].(string),
				Action: operation["action"].(string),
			})
		}
	}

	return manifest
}

func flattenServiceManifest(manifest *tsuru.ServiceManifest) []any {
	if manifest == nil {
		return nil
	}

	operations := make([]any, 0, len(manifest.Operations))
	for _, operation := range manifest.Operations {
		operations = append(operations, map[string]any{
			"method": operation.Method,
			"path":   operation.Path,
			"action": operation.Action,
		})
	}

	return []any{map[string]any{
		"enabled":        manifest.Enabled,
		"strict_actions": manifest.StrictActions,
		"legacy_compat":  manifest.LegacyCompat,
		"operations":     operations,
	}}
}

func serviceManifestAPIError(response *http.Response, err error) error {
	if err == nil {
		return nil
	}

	statusCode := 0
	if response != nil {
		statusCode = response.StatusCode
	}
	if apiErr, ok := err.(interface {
		Body() []byte
		StatusCode() int
	}); ok {
		if apiErr.StatusCode() != 0 {
			statusCode = apiErr.StatusCode()
		}
		if body := strings.TrimSpace(string(apiErr.Body())); body != "" {
			return fmt.Errorf("service manifest request failed with status %d: %s", statusCode, body)
		}
	}
	if statusCode != 0 {
		return fmt.Errorf("service manifest request failed with status %d: %w", statusCode, err)
	}
	return fmt.Errorf("service manifest request failed: %w", err)
}
