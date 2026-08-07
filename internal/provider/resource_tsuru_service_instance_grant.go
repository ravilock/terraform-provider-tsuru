// Copyright 2021 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	tsuru_client "github.com/tsuru/go-tsuruclient/pkg/tsuru"
)

func resourceTsuruServiceInstanceGrant() *schema.Resource {
	return &schema.Resource{
		Description:   "Tsuru Service Instance Grant",
		CreateContext: resourceTsuruServiceInstanceGrantCreate,
		ReadContext:   resourceTsuruServiceInstanceGrantRead,
		DeleteContext: resourceTsuruServiceInstanceGrantDelete,
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(60 * time.Minute),
			Delete: schema.DefaultTimeout(60 * time.Minute),
		},
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		Schema: map[string]*schema.Schema{
			"service_name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of service kind",
			},
			"service_instance": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "Name of service instance",
			},
			"team": {
				Type:        schema.TypeString,
				Description: "Team name",
				ForceNew:    true,
				Required:    true,
			},
		},
	}
}

func resourceTsuruServiceInstanceGrantCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	service := d.Get("service_name").(string)
	instance := d.Get("service_instance").(string)
	team := d.Get("team").(string)

	err := resource.RetryContext(ctx, d.Timeout(schema.TimeoutCreate), func() *resource.RetryError {
		_, err := provider.TsuruClient.ServiceApi.ServiceInstanceGrant(ctx, service, instance, team)
		if err != nil {
			if apiError, ok := errors.AsType[tsuru_client.GenericOpenAPIError](err); ok {
				if isRetryableError(apiError.Body()) {
					return resource.RetryableError(err)
				}
			}
			return resource.NonRetryableError(fmt.Errorf("unable to grant permission to team %s on %s %s: %w", team, service, instance, err))
		}
		return nil
	})
	if err != nil {
		return diag.FromErr(err)
	}

	d.SetId(createID([]string{service, instance, team}))

	return resourceTsuruServiceInstanceGrantRead(ctx, d, meta)
}

func resourceTsuruServiceInstanceGrantRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	parts, err := IDtoParts(d.Id(), 3)
	if err != nil {
		return diag.FromErr(err)
	}
	service := parts[0]
	instanceName := parts[1]
	team := parts[2]

	instance, _, err := provider.TsuruClient.ServiceApi.InstanceGet(ctx, service, instanceName)
	if err != nil {
		return diag.Errorf("unable to read bind %s %s: %v", service, instanceName, err)
	}

	for _, t := range instance.Teams {
		if team == t {
			d.Set("team", t)
			d.Set("service_name", service)
			d.Set("service_instance", instanceName)
			return nil
		}
	}
	d.SetId("")
	return nil
}

func resourceTsuruServiceInstanceGrantDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	provider := meta.(*tsuruProvider)

	service := d.Get("service_name").(string)
	instance := d.Get("service_instance").(string)
	team := d.Get("team").(string)

	_, err := provider.TsuruClient.ServiceApi.ServiceInstanceRevoke(ctx, service, instance, team)
	if err != nil {
		return diag.Errorf("unable to revoke permission to team %s on %s %s: %v", team, service, instance, err)
	}

	return nil
}
