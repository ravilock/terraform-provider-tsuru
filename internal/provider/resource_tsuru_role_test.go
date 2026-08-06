// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRole struct {
	Name               string   `json:"name"`
	Context            string   `json:"context"`
	Description        string   `json:"description"`
	SchemeNames        []string `json:"scheme_names,omitempty"`
	DynamicSchemeNames []string `json:"dynamic_scheme_names,omitempty"`
}

func TestAccTsuruRole_lifecycle(t *testing.T) {
	var mutex sync.Mutex
	var role *fakeRole
	var permissionBatches [][]string
	var mutationCount int

	fakeServer := echo.New()
	fakeServer.POST("/1.0/roles", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		var payload struct {
			Name        string `json:"name"`
			Context     string `json:"context"`
			Description string `json:"description"`
		}
		require.NoError(t, c.Bind(&payload))
		assert.Equal(t, "my-role", payload.Name)
		assert.Equal(t, "team", payload.Context)
		assert.Equal(t, "Initial description", payload.Description)
		assert.Equal(t, "test-token", c.Request().Header.Get("Authorization"))
		role = &fakeRole{Name: payload.Name, Context: payload.Context, Description: payload.Description}
		mutationCount++
		return c.NoContent(http.StatusCreated)
	})
	fakeServer.GET("/1.0/roles/:name", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		if role == nil || role.Name != c.Param("name") {
			return c.NoContent(http.StatusNotFound)
		}
		return c.JSON(http.StatusOK, role)
	})
	fakeServer.PUT("/1.4/roles", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		var payload struct {
			Name        string `json:"name"`
			NewName     string `json:"newName"`
			ContextType string `json:"contextType"`
			Description string `json:"description"`
		}
		require.NoError(t, c.Bind(&payload))
		require.NotNil(t, role)
		assert.Equal(t, role.Name, payload.Name)
		if payload.NewName != "" {
			role.Name = payload.NewName
		}
		if payload.ContextType != "" {
			role.Context = payload.ContextType
		}
		if payload.Description != "" {
			role.Description = payload.Description
		}
		mutationCount++
		return c.NoContent(http.StatusOK)
	})
	fakeServer.POST("/1.0/roles/:name/permissions", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		var payload struct {
			Permissions []string `json:"permission"`
		}
		require.NoError(t, c.Bind(&payload))
		require.NotNil(t, role)
		assert.Equal(t, role.Name, c.Param("name"))
		permissionBatches = append(permissionBatches, append([]string(nil), payload.Permissions...))
		for _, permission := range payload.Permissions {
			if len(permission) >= len("service-action.") && permission[:len("service-action.")] == "service-action." {
				role.DynamicSchemeNames = append(role.DynamicSchemeNames, permission)
			} else {
				role.SchemeNames = append(role.SchemeNames, permission)
			}
		}
		mutationCount++
		return c.NoContent(http.StatusOK)
	})
	fakeServer.DELETE("/1.0/roles/:name/permissions/:permission", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		require.NotNil(t, role)
		assert.Equal(t, role.Name, c.Param("name"))
		role.SchemeNames = removeString(role.SchemeNames, c.Param("permission"))
		role.DynamicSchemeNames = removeString(role.DynamicSchemeNames, c.Param("permission"))
		mutationCount++
		return c.NoContent(http.StatusOK)
	})
	fakeServer.DELETE("/1.0/roles/:name", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		if role == nil || role.Name != c.Param("name") {
			return c.NoContent(http.StatusNotFound)
		}
		role = nil
		mutationCount++
		return c.NoContent(http.StatusOK)
	})
	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		if httpError, ok := err.(*echo.HTTPError); ok {
			_ = c.NoContent(httpError.Code)
			return
		}
		t.Errorf("method=%s, path=%s, err=%s", c.Request().Method, c.Path(), err)
	}

	server := httptest.NewServer(fakeServer)
	defer server.Close()
	os.Setenv("TSURU_TARGET", server.URL)

	resourceName := "tsuru_role.test"
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		IDRefreshName:     resourceName,
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTsuruRoleConfig(server.URL, "my-role", "team", "Initial description", []string{"service-action.svc.rules", "app.create"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", "my-role"),
					resource.TestCheckResourceAttr(resourceName, "context", "team"),
					resource.TestCheckResourceAttr(resourceName, "description", "Initial description"),
					resource.TestCheckResourceAttr(resourceName, "permissions.#", "2"),
					func(_ *terraform.State) error {
						mutex.Lock()
						defer mutex.Unlock()
						require.Len(t, permissionBatches, 2)
						assert.Equal(t, []string{"app.create"}, permissionBatches[0])
						assert.Equal(t, []string{"service-action.svc.rules"}, permissionBatches[1])
						return nil
					},
				),
			},
			{
				Config: testAccTsuruRoleConfig(server.URL, "renamed-role", "service", "Updated description", []string{"service-action.svc.rules", "service.read"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", "renamed-role"),
					resource.TestCheckResourceAttr(resourceName, "context", "service"),
					resource.TestCheckResourceAttr(resourceName, "description", "Updated description"),
					resource.TestCheckResourceAttr(resourceName, "permissions.#", "2"),
				),
			},
			{
				Config: testAccTsuruRoleConfig(server.URL, "renamed-role", "service", "Updated description", []string{"service.read", "service-action.svc.rules"}),
				Check: func(_ *terraform.State) error {
					mutex.Lock()
					defer mutex.Unlock()
					assert.Equal(t, 6, mutationCount, "reordering permissions must not produce mutations")
					return nil
				},
			},
			{
				Config:      testAccTsuruRoleConfig(server.URL, "renamed-role", "service", "", []string{"service.read", "service-action.svc.rules"}),
				ExpectError: regexp.MustCompile("cannot clear role description"),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateId:     "renamed-role",
				ImportStateVerify: true,
			},
		},
	})
}

func removeString(values []string, target string) []string {
	result := values[:0]
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func testAccTsuruRoleConfig(serverURL, name, contextType, description string, permissions []string) string {
	quotedPermissions := make([]string, len(permissions))
	for index, permission := range permissions {
		quotedPermissions[index] = fmt.Sprintf("%q", permission)
	}
	return fmt.Sprintf(`
provider "tsuru" {
  host  = %q
  token = "test-token"
}

resource "tsuru_role" "test" {
  name        = %q
  context     = %q
  description = %q
  permissions = [%s]
}
`, serverURL, name, contextType, description, strings.Join(quotedPermissions, ", "))
}
