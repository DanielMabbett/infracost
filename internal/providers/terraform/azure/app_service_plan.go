package azure

import (
	"fmt"

	"github.com/infracost/infracost/internal/schema"

	"github.com/shopspring/decimal"
)

func GetAzureRMAppServicePlanRegistryItem() *schema.RegistryItem {
	return &schema.RegistryItem{
		Name:  "azurerm_app_service_plan",
		RFunc: NewAzureRMAppServicePlan,
		Notes: []string{
			"Costs associated with running an app service plan in Azure",
		},
	}
}

func NewAzureRMAppServicePlan(d *schema.ResourceData, u *schema.UsageData) *schema.Resource {
	return &schema.Resource{
		Name:           d.Address,
		CostComponents: AppServicePlanCostComponent(d),
	}
}

func AppServicePlanCostComponent(d *schema.ResourceData) []*schema.CostComponent {
	sku := d.Get("sku.0.size").String()
	purchaseOption := "Consumption"

	kind := d.Get("kind").String()

	costComponents := make([]*schema.CostComponent, 0)

	if kind == "linux" || kind == "Linux" {
		costComponents = append(costComponents, &schema.CostComponent{
			Name:           fmt.Sprintf("Consumption usage (%s, %s)", purchaseOption, sku),
			Unit:           "hours",
			UnitMultiplier: 1,
			HourlyQuantity: decimalPtr(decimal.NewFromInt(1)),
			ProductFilter: &schema.ProductFilter{
				VendorName:    strPtr("azure"),
				Region:        strPtr(d.Get("location").String()),
				Service:       strPtr("Azure App Service"),
				ProductFamily: strPtr("Compute"),
				AttributeFilters: []*schema.AttributeFilter{
					{Key: "skuName", Value: strPtr(sku)},
					{Key: "type", ValueRegex: strPtr(regexMustContain(purchaseOption))},
					{Key: "productName", ValueRegex: strPtr(regexMustContain("linux"))},
				},
			},
			PriceFilter: &schema.PriceFilter{
				PurchaseOption: strPtr(purchaseOption),
				Unit:           strPtr("1 Hour"),
			},
		})
	} else {
		costComponents = append(costComponents, &schema.CostComponent{
			Name:           fmt.Sprintf("Consumption usage (%s, %s)", purchaseOption, sku),
			Unit:           "hours",
			UnitMultiplier: 1,
			HourlyQuantity: decimalPtr(decimal.NewFromInt(1)),
			ProductFilter: &schema.ProductFilter{
				VendorName:    strPtr("azure"),
				Region:        strPtr(d.Get("location").String()),
				Service:       strPtr("Azure App Service"),
				ProductFamily: strPtr("Compute"),
				AttributeFilters: []*schema.AttributeFilter{
					{Key: "skuName", Value: strPtr(sku)},
					{Key: "type", Value: strPtr(purchaseOption)},
					{Key: "productName", ValueRegex: strPtr(regexDoesNotContain("linux"))},
				},
			},
			PriceFilter: &schema.PriceFilter{
				PurchaseOption: strPtr(purchaseOption),
				Unit:           strPtr("1 Hour"),
			},
		})
	}

	return costComponents
}

// func AppServicePlanAttributeFilters(d *schema.ResourceData) []*schema.AttributeFilter {
// 	purchaseOption := "Consumption"
//
// 	attributeFilter := make([]*schema.AttributeFilter, 0)
//
// 	attributeFilter = append(attributeFilter, &schema.AttributeFilter{
// 		Key:   "skuName",
// 		Value: strPtr(d.Get("sku.0.size").String()),
// 	})
//
// 	attributeFilter = append(attributeFilter, &schema.AttributeFilter{
// 		Key:   "type",
// 		Value: strPtr(purchaseOption),
// 	})
//
// 	attributeFilter = append(attributeFilter, &schema.AttributeFilter{
// 		Key:   "productName",
// 		Value: strPtr(regexDoesNotContain("linux")),
// 	})
//
// 	return attributeFilter
// }
//
