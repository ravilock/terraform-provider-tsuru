package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	echo "github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/go-tsuruclient/pkg/tsuru"
)

func TestAccTsuruService_basic(t *testing.T) {
	fakeServer := echo.New()

	fakeServer.POST("/1.0/services", func(c echo.Context) error {
		id := c.FormValue("id")
		endpoint := c.FormValue("endpoint")
		team := c.FormValue("team")
		encoding := c.FormValue("encoding")

		assert.Equal(t, "my-service", id)
		assert.Equal(t, "http://my-service.example.com", endpoint)
		assert.Equal(t, "my-team", team)
		assert.Equal(t, "form", encoding)

		return c.NoContent(http.StatusCreated)
	})

	fakeServer.GET("/1.0/services/:name", func(c echo.Context) error {
		name := c.Param("name")
		assert.Equal(t, "my-service", name)
		return c.JSON(http.StatusOK, []map[string]interface{}{
			{
				"service":   "my-service",
				"instances": []string{},
			},
		})
	})

	fakeServer.PUT("/1.0/services/:name", func(c echo.Context) error {
		name := c.Param("name")
		assert.Equal(t, "my-service", name)
		return c.NoContent(http.StatusOK)
	})

	fakeServer.DELETE("/1.0/services/:name", func(c echo.Context) error {
		name := c.Param("name")
		assert.Equal(t, "my-service", name)
		return c.NoContent(http.StatusNoContent)
	})

	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}
	server := httptest.NewServer(fakeServer)
	os.Setenv("TSURU_TARGET", server.URL)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      nil,
		Steps: []resource.TestStep{
			{
				Config: testAccTsuruServiceConfig_basic(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tsuru_service.my_service", "name", "my-service"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "endpoint", "http://my-service.example.com"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "team", "my-team"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "multi_cluster", "false"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "encoding", "form"),
				),
			},
		},
	})
}

func TestAccTsuruService_withEncoding(t *testing.T) {
	fakeServer := echo.New()

	fakeServer.POST("/1.0/services", func(c echo.Context) error {
		id := c.FormValue("id")
		encoding := c.FormValue("encoding")

		assert.Equal(t, "my-service", id)
		assert.Equal(t, "json", encoding)

		return c.NoContent(http.StatusCreated)
	})

	fakeServer.GET("/1.0/services/:name", func(c echo.Context) error {
		return c.JSON(http.StatusOK, []map[string]interface{}{
			{
				"service":   "my-service",
				"instances": []string{},
			},
		})
	})

	fakeServer.PUT("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	fakeServer.DELETE("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}
	server := httptest.NewServer(fakeServer)
	os.Setenv("TSURU_TARGET", server.URL)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      nil,
		Steps: []resource.TestStep{
			{
				Config: testAccTsuruServiceConfig_withEncoding(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tsuru_service.my_service", "encoding", "json"),
				),
			},
		},
	})
}

func testAccTsuruServiceConfig_basic() string {
	return `
resource "tsuru_service" "my_service" {
	name     = "my-service"
	endpoint = "http://my-service.example.com"
	team     = "my-team"
}
`
}

func testAccTsuruServiceConfig_withEncoding() string {
	return `
resource "tsuru_service" "my_service" {
	name     = "my-service"
	endpoint = "http://my-service.example.com"
	team     = "my-team"
	encoding = "json"
}
`
}

func TestAccTsuruService_withEndpointsMap(t *testing.T) {
	fakeServer := echo.New()

	fakeServer.POST("/1.0/services", func(c echo.Context) error {
		assert.Equal(t, "my-service", c.FormValue("id"))
		assert.Equal(t, "my-team", c.FormValue("team"))
		assert.Equal(t, "", c.FormValue("endpoint"))
		assert.Equal(t, "https://prod.example.com", c.FormValue("endpoints.production"))
		assert.Equal(t, "https://c1.example.com", c.FormValue("endpoints.cluster1"))
		return c.NoContent(http.StatusCreated)
	})

	fakeServer.GET("/1.0/services/:name", func(c echo.Context) error {
		return c.JSON(http.StatusOK, []map[string]interface{}{
			{"service": "my-service", "instances": []string{}},
		})
	})

	fakeServer.PUT("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	fakeServer.DELETE("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}
	server := httptest.NewServer(fakeServer)
	os.Setenv("TSURU_TARGET", server.URL)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		CheckDestroy:      nil,
		Steps: []resource.TestStep{
			{
				Config: testAccTsuruServiceConfig_withEndpointsMap(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tsuru_service.my_service", "name", "my-service"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "endpoints.production", "https://prod.example.com"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "endpoints.cluster1", "https://c1.example.com"),
					resource.TestCheckNoResourceAttr("tsuru_service.my_service", "endpoint"),
				),
			},
		},
	})
}

func testAccTsuruServiceConfig_withEndpointsMap() string {
	return `
resource "tsuru_service" "my_service" {
	name = "my-service"
	team = "my-team"
	endpoints = {
		production = "https://prod.example.com"
		cluster1   = "https://c1.example.com"
	}
}
`
}

func TestAccTsuruService_endpointAndEndpointsAreMutuallyExclusive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called when validation fails: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()
	os.Setenv("TSURU_TARGET", server.URL)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "tsuru_service" "my_service" {
	name     = "my-service"
	team     = "my-team"
	endpoint = "https://api.example.com"
	endpoints = {
		production = "https://prod.example.com"
	}
}
`,
				ExpectError: regexp.MustCompile("only one of"),
			},
		},
	})
}

func TestAccTsuruService_withManifestLifecycle(t *testing.T) {
	fakeServer := echo.New()
	var mutex sync.Mutex
	var storedManifest *tsuru.ServiceManifest
	manifestPutCount := 0
	manifestGetCount := 0

	fakeServer.POST("/1.0/services", func(c echo.Context) error {
		return c.NoContent(http.StatusCreated)
	})
	fakeServer.GET("/1.0/services/:name", func(c echo.Context) error {
		return c.JSON(http.StatusOK, []map[string]interface{}{
			{"service": "my-service", "instances": []string{}},
		})
	})
	fakeServer.PUT("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	fakeServer.DELETE("/1.0/services/:name", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	fakeServer.PUT("/1.31/services/:name/manifest", func(c echo.Context) error {
		assert.Equal(t, "my-service", c.Param("name"))
		manifest := &tsuru.ServiceManifest{}
		require.NoError(t, json.NewDecoder(c.Request().Body).Decode(manifest))
		mutex.Lock()
		storedManifest = manifest
		manifestPutCount++
		mutex.Unlock()
		return c.NoContent(http.StatusOK)
	})
	fakeServer.GET("/1.31/services/:name/manifest", func(c echo.Context) error {
		mutex.Lock()
		defer mutex.Unlock()
		manifestGetCount++
		return c.JSON(http.StatusOK, storedManifest)
	})
	fakeServer.HTTPErrorHandler = func(err error, c echo.Context) {
		t.Errorf("methods=%s, path=%s, err=%s", c.Request().Method, c.Path(), err.Error())
	}

	server := httptest.NewServer(fakeServer)
	defer server.Close()
	t.Setenv("TSURU_TARGET", server.URL)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccTsuruServiceConfig_withManifest(true, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.enabled", "true"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.strict_actions", "true"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.legacy_compat", "false"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.operations.#", "1"),
					func(*terraform.State) error {
						mutex.Lock()
						defer mutex.Unlock()
						assert.Equal(t, 1, manifestPutCount)
						assert.Greater(t, manifestGetCount, 0)
						assert.True(t, storedManifest.Enabled)
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					mutex.Lock()
					storedManifest.Enabled = false
					storedManifest.LegacyCompat = true
					mutex.Unlock()
				},
				Config: testAccTsuruServiceConfig_withManifest(true, false),
				Check: func(*terraform.State) error {
					mutex.Lock()
					defer mutex.Unlock()
					assert.Equal(t, 2, manifestPutCount, "remote manifest drift must be restored")
					assert.True(t, storedManifest.Enabled)
					assert.False(t, storedManifest.LegacyCompat)
					return nil
				},
			},
			{
				PreConfig: func() {
					mutex.Lock()
					storedManifest = nil
					mutex.Unlock()
				},
				Config: testAccTsuruServiceConfig_withManifest(true, false),
				Check: func(*terraform.State) error {
					mutex.Lock()
					defer mutex.Unlock()
					assert.Equal(t, 3, manifestPutCount, "a missing remote manifest must be restored")
					assert.True(t, storedManifest.Enabled)
					return nil
				},
			},
			{
				Config: testAccTsuruServiceConfig_withManifest(false, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.enabled", "false"),
					resource.TestCheckResourceAttr("tsuru_service.my_service", "manifest.0.legacy_compat", "true"),
					func(*terraform.State) error {
						mutex.Lock()
						defer mutex.Unlock()
						assert.Equal(t, 4, manifestPutCount)
						assert.False(t, storedManifest.Enabled)
						return nil
					},
				),
			},
			{
				Config: testAccTsuruServiceConfig_basic(),
				Check: func(*terraform.State) error {
					mutex.Lock()
					defer mutex.Unlock()
					assert.Equal(t, 4, manifestPutCount, "removing the block must leave the manifest unmanaged")
					return nil
				},
			},
		},
	})
}

func testAccTsuruServiceConfig_withManifest(enabled, legacyCompat bool) string {
	return fmt.Sprintf(`
resource "tsuru_service" "my_service" {
	name     = "my-service"
	endpoint = "http://my-service.example.com"
	team     = "my-team"

	manifest {
		enabled        = %t
		strict_actions = true
		legacy_compat  = %t

		operations {
			method = "post"
			path   = "/rules/{ruleId}/sync"
			action = "rules.sync"
		}
	}
}
`, enabled, legacyCompat)
}
