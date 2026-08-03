// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"errors"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/go-tsuruclient/pkg/tsuru"
)

func TestServiceManifestExpandAndFlatten(t *testing.T) {
	operationSet := schema.NewSet(hashServiceManifestOperation, []any{
		map[string]any{
			"method": " post ",
			"path":   "/rules/{ruleId}/sync",
			"action": "rules.sync",
		},
	})

	manifest := expandServiceManifest([]any{map[string]any{
		"enabled":        true,
		"strict_actions": true,
		"legacy_compat":  false,
		"operations":     operationSet,
	}})

	assert.Equal(t, &tsuru.ServiceManifest{
		Enabled:       true,
		StrictActions: true,
		LegacyCompat:  false,
		Operations: []tsuru.ServiceManifestOperation{{
			Method: http.MethodPost,
			Path:   "/rules/{ruleId}/sync",
			Action: "rules.sync",
		}},
	}, manifest)
	assert.Equal(t, []any{map[string]any{
		"enabled":        true,
		"strict_actions": true,
		"legacy_compat":  false,
		"operations": []any{map[string]any{
			"method": http.MethodPost,
			"path":   "/rules/{ruleId}/sync",
			"action": "rules.sync",
		}},
	}}, flattenServiceManifest(manifest))
}

func TestServiceManifestAPIError(t *testing.T) {
	err := serviceManifestAPIError(&http.Response{StatusCode: http.StatusConflict}, errors.New("Conflict"))

	require.EqualError(t, err, "service manifest request failed with status 409: Conflict")
}

func TestServiceManifestSchemaValidationAndNormalization(t *testing.T) {
	resourceSchema := resourceTsuruService().Schema
	manifestSchema := resourceSchema["manifest"].Elem.(*schema.Resource).Schema
	operationSchema := manifestSchema["operations"].Elem.(*schema.Resource).Schema

	assert.True(t, manifestSchema["enabled"].Required)
	assert.Equal(t, "POST", operationSchema["method"].StateFunc(" post "))

	_, validationErrors := operationSchema["method"].ValidateFunc("TRACE", "manifest.operations.method")
	require.Len(t, validationErrors, 1)

	_, validationErrors = operationSchema["path"].ValidateFunc("   ", "manifest.operations.path")
	require.Len(t, validationErrors, 1)

	_, validationErrors = operationSchema["action"].ValidateFunc("Rules.Sync", "manifest.operations.action")
	require.Len(t, validationErrors, 1)

	_, validationErrors = operationSchema["action"].ValidateFunc("rules.sync-v2", "manifest.operations.action")
	require.Empty(t, validationErrors)
}
