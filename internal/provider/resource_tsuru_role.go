// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

var roleContextTypes = []string{
	"global",
	"app",
	"job",
	"team",
	"user",
	"pool",
	"service",
	"service-instance",
	"volume",
	"router",
}

type tsuruRole struct {
	Name               string   `json:"name"`
	Context            string   `json:"context"`
	Description        string   `json:"description"`
	SchemeNames        []string `json:"scheme_names,omitempty"`
	DynamicSchemeNames []string `json:"dynamic_scheme_names,omitempty"`
}

type roleAPIError struct {
	StatusCode int
	Body       string
}

func (e *roleAPIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("tsuru API returned status code %d", e.StatusCode)
	}
	return fmt.Sprintf("tsuru API returned status code %d: %s", e.StatusCode, e.Body)
}

func resourceTsuruRole() *schema.Resource {
	return &schema.Resource{
		Description:   "Manages a tsuru role and its complete set of permissions.",
		CreateContext: resourceTsuruRoleCreate,
		ReadContext:   resourceTsuruRoleRead,
		UpdateContext: resourceTsuruRoleUpdate,
		DeleteContext: resourceTsuruRoleDelete,
		CustomizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ any) error {
			oldDescription, newDescription := d.GetChange("description")
			if oldDescription.(string) != "" && newDescription.(string) == "" {
				return fmt.Errorf("cannot clear role description: the tsuru API does not support changing a non-empty description to empty")
			}
			return nil
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "Unique role name",
				StateFunc:    normalizeRoleValue,
				ValidateFunc: validation.StringIsNotWhiteSpace,
			},
			"context": {
				Type:         schema.TypeString,
				Required:     true,
				Description:  "Context type associated with the role. Valid values are: global, app, job, team, user, pool, service, service-instance, volume, and router",
				ValidateFunc: validation.StringInSlice(roleContextTypes, false),
			},
			"description": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "",
				Description: "Description associated with the role",
			},
			"permissions": {
				Type:        schema.TypeSet,
				Optional:    true,
				Description: "Complete set of permissions associated with the role",
				Elem: &schema.Schema{
					Type:         schema.TypeString,
					StateFunc:    normalizeRoleValue,
					ValidateFunc: validation.StringIsNotWhiteSpace,
				},
			},
		},
	}
}

func normalizeRoleValue(value any) string {
	return strings.TrimSpace(value.(string))
}

func resourceTsuruRoleCreate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	role := tsuruRole{
		Name:        d.Get("name").(string),
		Context:     d.Get("context").(string),
		Description: d.Get("description").(string),
	}

	if err := roleRequest(ctx, meta.(*tsuruProvider), http.MethodPost, "1.0", "/roles", role, nil); err != nil {
		return diag.Errorf("Could not create tsuru role %q: %s", role.Name, err)
	}
	d.SetId(role.Name)

	if err := addRolePermissions(ctx, meta.(*tsuruProvider), role.Name, permissionSet(d.Get("permissions"))); err != nil {
		return diag.Errorf("Could not add permissions to tsuru role %q: %s", role.Name, err)
	}

	return resourceTsuruRoleRead(ctx, d, meta)
}

func resourceTsuruRoleRead(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	provider := meta.(*tsuruProvider)
	var role tsuruRole
	err := roleRequest(ctx, provider, http.MethodGet, "1.0", rolePath(d.Id()), nil, &role)
	if err != nil {
		if isRoleAPIStatus(err, http.StatusNotFound) {
			d.SetId("")
			return nil
		}
		return diag.Errorf("Could not read tsuru role %q: %s", d.Id(), err)
	}

	permissions := append([]string{}, role.SchemeNames...)
	permissions = append(permissions, role.DynamicSchemeNames...)
	if err := d.Set("name", role.Name); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("context", role.Context); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("description", role.Description); err != nil {
		return diag.FromErr(err)
	}
	if err := d.Set("permissions", permissions); err != nil {
		return diag.FromErr(err)
	}
	d.SetId(role.Name)
	return nil
}

func resourceTsuruRoleUpdate(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	provider := meta.(*tsuruProvider)
	oldPermissions, newPermissions := permissionChange(d)
	removed := permissionDifference(oldPermissions, newPermissions)
	for _, permission := range removed {
		if err := roleRequest(ctx, provider, http.MethodDelete, "1.0", rolePermissionPath(d.Id(), permission), nil, nil); err != nil {
			return diag.Errorf("Could not remove permission %q from tsuru role %q: %s", permission, d.Id(), err)
		}
	}

	roleName := d.Id()
	if d.HasChanges("name", "context", "description") {
		payload := map[string]string{"name": roleName}
		if d.HasChange("name") {
			payload["newName"] = d.Get("name").(string)
		}
		if d.HasChange("context") {
			payload["contextType"] = d.Get("context").(string)
		}
		if d.HasChange("description") {
			payload["description"] = d.Get("description").(string)
		}
		if err := roleRequest(ctx, provider, http.MethodPut, "1.4", "/roles", payload, nil); err != nil {
			return diag.Errorf("Could not update tsuru role %q: %s", roleName, err)
		}
		roleName = d.Get("name").(string)
		d.SetId(roleName)
	}

	added := permissionDifference(newPermissions, oldPermissions)
	if err := addRolePermissions(ctx, provider, roleName, added); err != nil {
		return diag.Errorf("Could not add permissions to tsuru role %q: %s", roleName, err)
	}

	return resourceTsuruRoleRead(ctx, d, meta)
}

func resourceTsuruRoleDelete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	err := roleRequest(ctx, meta.(*tsuruProvider), http.MethodDelete, "1.0", rolePath(d.Id()), nil, nil)
	if err != nil && !isRoleAPIStatus(err, http.StatusNotFound) {
		return diag.Errorf("Could not delete tsuru role %q: %s", d.Id(), err)
	}
	return nil
}

func permissionChange(d *schema.ResourceData) ([]string, []string) {
	oldValue, newValue := d.GetChange("permissions")
	return permissionSet(oldValue), permissionSet(newValue)
}

func permissionSet(value any) []string {
	set, ok := value.(*schema.Set)
	if !ok || set == nil {
		return nil
	}
	result := make([]string, 0, set.Len())
	for _, item := range set.List() {
		result = append(result, item.(string))
	}
	sort.Strings(result)
	return result
}

func permissionDifference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}
	var result []string
	for _, item := range left {
		if _, found := rightSet[item]; !found {
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}

func addRolePermissions(ctx context.Context, provider *tsuruProvider, roleName string, permissions []string) error {
	var staticPermissions, dynamicPermissions []string
	for _, permission := range permissions {
		if strings.HasPrefix(permission, "service-action.") {
			dynamicPermissions = append(dynamicPermissions, permission)
		} else {
			staticPermissions = append(staticPermissions, permission)
		}
	}
	for _, permissionGroup := range [][]string{staticPermissions, dynamicPermissions} {
		if len(permissionGroup) == 0 {
			continue
		}
		payload := map[string]any{"permission": permissionGroup}
		if err := roleRequest(ctx, provider, http.MethodPost, "1.0", rolePermissionsPath(roleName), payload, nil); err != nil {
			return err
		}
	}
	return nil
}

func rolePath(roleName string) string {
	return "/roles/" + url.PathEscape(roleName)
}

func rolePermissionsPath(roleName string) string {
	return rolePath(roleName) + "/permissions"
}

func rolePermissionPath(roleName, permission string) string {
	return rolePermissionsPath(roleName) + "/" + url.PathEscape(permission)
}

func roleRequest(ctx context.Context, provider *tsuruProvider, method, version, path string, payload, result any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}

	requestURL := strings.TrimRight(provider.Host, "/") + "/" + version + path
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return err
	}
	for key, values := range provider.HTTPHeaders {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")

	response, err := provider.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &roleAPIError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			return err
		}
	}
	return nil
}

func isRoleAPIStatus(err error, statusCode int) bool {
	apiError, ok := err.(*roleAPIError)
	return ok && apiError.StatusCode == statusCode
}
