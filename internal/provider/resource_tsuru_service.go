// Copyright 2021 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/antihax/optional"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/tsuru/go-tsuruclient/pkg/tsuru"
)

var serviceManifestActionRegexp = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)*$`)

func resourceTsuruService() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceTsuruServiceCreate,
		ReadContext:   resourceTsuruServiceRead,
		UpdateContext: resourceTsuruServiceUpdate,
		DeleteContext: resourceTsuruServiceDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(60 * time.Minute),
			Update: schema.DefaultTimeout(60 * time.Minute),
			Delete: schema.DefaultTimeout(60 * time.Minute),
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Service name",
			},
			"username": {
				Type:        schema.TypeString,
				Optional:    true,
				Description: "Username for service authentication",
			},
			"password": {
				Type:        schema.TypeString,
				Optional:    true,
				Sensitive:   true,
				Description: "Password for service authentication",
			},
			"endpoint": {
				Type:         schema.TypeString,
				Optional:     true,
				Description:  "Single service endpoint URL. The server stores it as endpoints[\"production\"]. Mutually exclusive with \"endpoints\".",
				ExactlyOneOf: []string{"endpoint", "endpoints"},
			},
			"endpoints": {
				Type:         schema.TypeMap,
				Optional:     true,
				Elem:         &schema.Schema{Type: schema.TypeString},
				Description:  "Map of endpoint name to URL, e.g. { production = \"https://prod...\" }. Mutually exclusive with \"endpoint\".",
				ExactlyOneOf: []string{"endpoint", "endpoints"},
			},
			"multi_cluster": {
				Type:        schema.TypeBool,
				Optional:    true,
				Default:     false,
				Description: "Whether the service supports multi-cluster",
			},
			"team": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Team owner of this service",
			},
			"encoding": {
				Type:        schema.TypeString,
				Optional:    true,
				Default:     "form",
				Description: "Encoding format used to communicate with the service API backend. Valid options are \"form\" (default) and \"json\".",
			},
			"manifest": {
				Type:        schema.TypeList,
				Optional:    true,
				MaxItems:    1,
				Description: "Fine-grained authorization manifest. When omitted, an existing manifest is left unmanaged. Requires tsuru API 1.31 or newer.",
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"enabled": {
							Type:        schema.TypeBool,
							Required:    true,
							Description: "Whether fine-grained manifest authorization is enabled",
						},
						"strict_actions": {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "Whether requests that do not match an operation are denied",
						},
						"legacy_compat": {
							Type:        schema.TypeBool,
							Optional:    true,
							Default:     false,
							Description: "Whether the legacy service instance update proxy permission remains accepted",
						},
						"operations": {
							Type:        schema.TypeSet,
							Optional:    true,
							Description: "Service API operations and their fine-grained authorization actions",
							Set:         hashServiceManifestOperation,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"method": {
										Type:             schema.TypeString,
										Required:         true,
										Description:      "HTTP method matched by this operation",
										StateFunc:        normalizeServiceManifestMethod,
										DiffSuppressFunc: suppressEquivalentServiceManifestMethod,
										ValidateFunc:     validation.StringInSlice([]string{http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut}, true),
									},
									"path": {
										Type:         schema.TypeString,
										Required:     true,
										Description:  "Service API path pattern matched by this operation",
										ValidateFunc: validation.StringIsNotWhiteSpace,
									},
									"action": {
										Type:         schema.TypeString,
										Required:     true,
										Description:  "Dotted action name used to create the dynamic permission",
										ValidateFunc: validation.StringMatch(serviceManifestActionRegexp, "must contain lowercase letters, numbers, or hyphens separated by dots"),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func normalizeServiceManifestMethod(value interface{}) string {
	return strings.ToUpper(strings.TrimSpace(value.(string)))
}

func suppressEquivalentServiceManifestMethod(_, old, new string, _ *schema.ResourceData) bool {
	return strings.EqualFold(strings.TrimSpace(old), strings.TrimSpace(new))
}

func hashServiceManifestOperation(value interface{}) int {
	operation := value.(map[string]interface{})
	return schema.HashString(strings.Join([]string{
		normalizeServiceManifestMethod(operation["method"]),
		operation["path"].(string),
		operation["action"].(string),
	}, "\x00"))
}

func resourceTsuruServiceCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	name := d.Get("name").(string)

	opts := &tsuru.ServiceCreateOpts{
		Id:   optional.NewString(name),
		Team: optional.NewString(d.Get("team").(string)),
	}

	if v, ok := d.GetOk("endpoint"); ok {
		opts.Endpoint = optional.NewString(v.(string))
	}

	if v, ok := d.GetOk("endpoints"); ok {
		opts.Endpoints = expandServiceEndpoints(v)
	}

	if v, ok := d.GetOk("username"); ok {
		opts.Username = optional.NewString(v.(string))
	}

	if v, ok := d.GetOk("password"); ok {
		opts.Password = optional.NewString(v.(string))
	}

	if d.Get("multi_cluster").(bool) {
		opts.MultiCluster = optional.NewString("true")
	}

	if v, ok := d.GetOk("encoding"); ok {
		opts.Encoding = optional.NewString(v.(string))
	}

	_, err := provider.TsuruClient.ServiceApi.ServiceCreate(ctx, opts)
	if err != nil {
		return diag.Errorf("Could not create tsuru service %q, err: %s", name, err.Error())
	}

	d.SetId(name)

	if manifest := expandServiceManifest(d.Get("manifest")); manifest != nil {
		response, err := provider.TsuruClient.ServiceApi.ServiceManifestSet(ctx, name, *manifest)
		if err := serviceManifestAPIError(response, err); err != nil {
			return diag.Errorf("Could not set manifest for tsuru service %q, err: %s", name, err.Error())
		}
	}

	return resourceTsuruServiceRead(ctx, d, meta)
}

func resourceTsuruServiceRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	name := d.Id()

	_, resp, err := provider.TsuruClient.ServiceApi.ServiceInfo(ctx, name)
	if err != nil {
		return diag.Errorf("Could not get tsuru service %q, err: %s", name, err.Error())
	}

	if resp.StatusCode == http.StatusNotFound {
		d.SetId("")
		return nil
	}

	if resp.StatusCode == http.StatusOK {
		d.SetId(name)
		if expandServiceManifest(d.Get("manifest")) != nil {
			manifest, manifestResponse, err := provider.TsuruClient.ServiceApi.ServiceManifestGet(ctx, name)
			if err := serviceManifestAPIError(manifestResponse, err); err != nil {
				return diag.Errorf("Could not get manifest for tsuru service %q, err: %s", name, err.Error())
			}
			if err := d.Set("manifest", flattenServiceManifest(&manifest)); err != nil {
				return diag.Errorf("Could not set manifest state for tsuru service %q, err: %s", name, err.Error())
			}
		}
		return nil
	}

	return diag.Errorf("Unexpected response code %d when getting tsuru service %q", resp.StatusCode, name)
}

func resourceTsuruServiceUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	name := d.Id()

	opts := &tsuru.ServiceUpdateOpts{
		Id:   optional.NewString(name),
		Team: optional.NewString(d.Get("team").(string)),
	}

	if v, ok := d.GetOk("endpoint"); ok {
		opts.Endpoint = optional.NewString(v.(string))
	}

	if v, ok := d.GetOk("endpoints"); ok {
		opts.Endpoints = expandServiceEndpoints(v)
	}

	if v, ok := d.GetOk("username"); ok {
		opts.Username = optional.NewString(v.(string))
	}

	if v, ok := d.GetOk("password"); ok {
		opts.Password = optional.NewString(v.(string))
	}

	if d.Get("multi_cluster").(bool) {
		opts.MultiCluster = optional.NewString("true")
	} else {
		opts.MultiCluster = optional.NewString("false")
	}

	if v, ok := d.GetOk("encoding"); ok {
		opts.Encoding = optional.NewString(v.(string))
	}

	_, err := provider.TsuruClient.ServiceApi.ServiceUpdate(ctx, name, opts)
	if err != nil {
		return diag.Errorf("Could not update tsuru service %q, err: %s", name, err.Error())
	}

	if d.HasChange("manifest") {
		if manifest := expandServiceManifest(d.Get("manifest")); manifest != nil {
			response, err := provider.TsuruClient.ServiceApi.ServiceManifestSet(ctx, name, *manifest)
			if err := serviceManifestAPIError(response, err); err != nil {
				return diag.Errorf("Could not set manifest for tsuru service %q, err: %s", name, err.Error())
			}
		}
	}

	return resourceTsuruServiceRead(ctx, d, meta)
}

func expandServiceEndpoints(v interface{}) map[string]string {
	raw, ok := v.(map[string]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		out[k] = val.(string)
	}
	return out
}

func resourceTsuruServiceDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	name := d.Id()

	_, err := provider.TsuruClient.ServiceApi.ServiceDelete(ctx, name)
	if err != nil {
		return diag.Errorf("Could not delete tsuru service %q, err: %s", name, err.Error())
	}

	return nil
}
