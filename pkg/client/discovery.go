package client

import (
	"fmt"
	"github.com/iceisfun/goeip/pkg/cip"
	"github.com/iceisfun/goeip/pkg/eip"
)

// ListIdentity lists the identity of the target
func (c *Client) ListIdentity() ([]eip.ListIdentityItem, error) {
	return c.session.ListIdentity()
}

// ListServices lists the services supported by the target
func (c *Client) ListServices() ([]eip.ListServicesItem, error) {
	return c.session.ListServices()
}

// ListTags lists all tags on the PLC by iterating the Symbol Object
func (c *Client) ListTags() ([]cip.SymbolInstance, error) {
	// Step 1: Get Max Instance ID from Symbol Class (Class 0x6B, Instance 0, Attr 2)
	// We also get Revision (Attr 1) just in case.
	reqClass := cip.NewGetSymbolClassAttributesRequest()
	respClass, err := c.session.SendCIPRequest(reqClass)
	if err != nil {
		return nil, fmt.Errorf("failed to get symbol class attributes: %w", err)
	}
	if !respClass.IsSuccess() {
		return nil, respClass.Error()
	}

	_, maxInstance, err := cip.DecodeSymbolClassAttributesResponse(respClass.ResponseData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode symbol class attributes: %w", err)
	}

	c.logger.Infof("Max Symbol Instance: %d", maxInstance)

	var allSymbols []cip.SymbolInstance

	// Step 2: Iterate from 0 to Max Instance
	// Note: Instance 0 is the Class Object, so we start from 1?
	// Symbol Instances usually start at 1.
	// But let's check 0 just in case (though 0 is usually Class).
	// We'll start from 1.

	// Optimization: We could use MultipleServicePacket to batch requests?
	// For now, simple loop.

	for id := uint32(1); id <= uint32(maxInstance); id++ {
		req := cip.NewGetSymbolAttributesRequest(id)
		resp, err := c.session.SendCIPRequest(req)
		if err != nil {
			// Network error, abort? or continue?
			c.logger.Warnf("Failed to fetch attributes for instance %d: %v", id, err)
			continue
		}

		if !resp.IsSuccess() {
			// If Object Does Not Exist, skip
			if resp.GeneralStatus == cip.StatusObjectDoesNotExist || resp.GeneralStatus == cip.StatusPathDestinationUnknown {
				continue
			}
			// Other errors (e.g. Service Not Supported) -> skip
			continue
		}

		name, typeCode, err := cip.DecodeSymbolAttributesResponse(resp.ResponseData)
		if err != nil {
			c.logger.Warnf("Failed to decode attributes for instance %d: %v", id, err)
			continue
		}

		if name != "" {
			allSymbols = append(allSymbols, cip.SymbolInstance{
				InstanceID: id,
				Name:       name,
				Type:       typeCode,
			})
		}
	}

	return allSymbols, nil
}
