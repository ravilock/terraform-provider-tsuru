// Copyright 2021 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	echo "github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestAccResourceTsuruAppDeploy(t *testing.T) {
	fakeServer := echo.New()

	fakeServer.POST("/1.0/apps/:app/deploy", func(c echo.Context) error {
		c.Response().Header().Set("X-Tsuru-Eventid", "abc-123")

		formParams, err := c.FormParams()
		if err != nil {
			return err
		}
		assert.Equal(t, url.Values{
			"image":             {"myrepo/app01:0.1.0"},
			"message":           {"deploy via terraform"},
			"new-version":       {"false"},
			"origin":            {"image"},
			"override-versions": {"false"},
		},
			formParams)

		authHeader := c.Request().Header.Get("Authorization")
		assert.Contains(t, authHeader, "testing-token")

		return c.String(http.StatusOK, "OK")
	})

	iterationCount := 0
	fakeServer.GET("/1.1/events/:eventID", func(c echo.Context) error {
		iterationCount++

		return c.JSON(http.StatusOK, map[string]interface{}{
			"Running": iterationCount < 2,
			"EndTime": "2023-01-04T19:26:20.946Z",
			"EndCustomData": map[string]interface{}{
				"Kind": 3,
				"Data": "GwAAAAJpbWFnZQALAAAAdGVzdDoxLjIuMwAA",
			},
		})
	})

	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}
	server := httptest.NewServer(fakeServer)
	t.Setenv("TSURU_TARGET", server.URL)
	t.Setenv("TSURU_TOKEN", "testing-token")

	resourceName := "tsuru_app_deploy.deploy"
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      nil,
		Steps: []resource.TestStep{
			{
				Config: testAccResourceTsuruAppDeploy_basic(server.URL),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccResourceExists(resourceName),
					resource.TestCheckResourceAttr(resourceName, "app", "app01"),
					resource.TestCheckResourceAttr(resourceName, "image", "myrepo/app01:0.1.0"),
					resource.TestCheckResourceAttr(resourceName, "status", "finished"),
					resource.TestCheckResourceAttr(resourceName, "output_image", "test:1.2.3"),
				),
			},
		},
	})
}

func TestAccResourceTsuruAppDeployFailed(t *testing.T) {
	fakeServer := echo.New()

	fakeServer.POST("/1.0/apps/:app/deploy", func(c echo.Context) error {
		c.Response().Header().Set("X-Tsuru-Eventid", "abc-123")

		formParams, err := c.FormParams()
		if err != nil {
			return err
		}
		assert.Equal(t, url.Values{
			"image":             {"myrepo/app01:0.1.0"},
			"message":           {"deploy via terraform"},
			"new-version":       {"false"},
			"origin":            {"image"},
			"override-versions": {"false"},
		},
			formParams)

		authHeader := c.Request().Header.Get("Authorization")
		assert.Contains(t, authHeader, "testing-token")

		return c.String(http.StatusOK, "OK")
	})

	fakeServer.GET("/1.1/events/:eventID", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"Running": false,
			"Error":   "deploy failed",
			"EndTime": "2023-01-04T19:26:20.946Z",
		})
	})

	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}
	server := httptest.NewServer(fakeServer)
	t.Setenv("TSURU_TARGET", server.URL)
	t.Setenv("TSURU_TOKEN", "testing-token")

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      nil,
		Steps: []resource.TestStep{
			{
				Config:      testAccResourceTsuruAppDeploy_basic(server.URL),
				ExpectError: regexp.MustCompile("deploy failed, see details of event ID: abc-123"),
			},
		},
	})
}

func TestResourceTsuruAppDeployWithoutToken(t *testing.T) {
	resource := resourceTsuruApplicationDeploy()
	data := schema.TestResourceDataRaw(t, resource.Schema, map[string]interface{}{
		"app":   "app01",
		"image": "myrepo/app01:0.1.0",
	})

	diagnostics := resourceTsuruApplicationDeployDo(context.Background(), data, &tsuruProvider{
		Host: "http://example.com",
	})

	assert.Len(t, diagnostics, 1)
	assert.Equal(t, "token not available", diagnostics[0].Summary)
}

func testAccResourceTsuruAppDeploy_basic(serverURL string) string {
	return fmt.Sprintf(`

	provider "tsuru" {
		host = "%s"
	}

	resource "tsuru_app_deploy" "deploy" {
		app = "app01"
		image = "myrepo/app01:0.1.0"
	}
`, serverURL)
}
