package google

import (
	"fmt"
	"strings"

	"github.com/infracost/infracost/internal/schema"
	"github.com/shopspring/decimal"
)

func GetStorageBucketRegistryItem() *schema.RegistryItem {
	return &schema.RegistryItem{
		Name:                "google_storage_bucket",
		RFunc:               NewStorageBucket,
		ReferenceAttributes: []string{},
	}
}

func NewStorageBucket(d *schema.ResourceData, u *schema.UsageData) *schema.Resource {
	components := []*schema.CostComponent{
		dataStorage(d, u),
	}
	data := dataRetrieval(d, u)
	if data != nil {
		components = append(components, data)
	}
	components = append(components, operations(d, u)...)
	return &schema.Resource{
		Name:           d.Address,
		CostComponents: components,
	}
}

func getDSRegionResourceGroup(location, storageClass string) (string, string) {
	// Storage buckets have a location field that indicates the location of data. You can
	// take a look at https://cloud.google.com/storage/docs/locations for list of locations.
	// There is a complicated mapping for Standard storage class. If the location is a single
	// region, then the resource group is named "RegionalStorage" and for multi-region regions
	// its "MultiRegionalStorage". For other storage classes they are fixed.

	// Convert the location field in terraform to a valid region name
	region := strings.ToLower(location)

	// Get the right resourceGroup for api query
	var resourceGroup string
	switch storageClass {
	case "NEARLINE":
		resourceGroup = "NearlineStorage"
	case "COLDLINE":
		resourceGroup = "ColdlineStorage"
	case "ARCHIVE":
		resourceGroup = "ArchiveStorage"
	default:
		resourceGroup = "RegionalStorage"
	}
	// Set the resource group to the right value if the location is a multi-region region
	if resourceGroup == "RegionalStorage" {
		switch location {
		// Multi-region locations
		case "ASIA", "EU", "US":
			resourceGroup = "MultiRegionalStorage"
		// Dual-region locations
		case "ASIA1", "EUR4", "NAM4":
			// The pricing api treats a dual-region as a multi-region
			resourceGroup = "MultiRegionalStorage"
		}
	}

	return region, resourceGroup
}

func dataStorage(d *schema.ResourceData, u *schema.UsageData) *schema.CostComponent {
	location := d.Get("location").String()
	var quantity *decimal.Decimal
	if u != nil && u.Get("storage_gb").Exists() {
		quantity = decimalPtr(decimal.NewFromInt(u.Get("storage_gb").Int()))
	}
	storageClass := "STANDARD"
	if d.Get("storage_class").Exists() {
		storageClass = d.Get("storage_class").String()
	}

	region, resourceGroup := getDSRegionResourceGroup(location, storageClass)
	return &schema.CostComponent{
		Name:            fmt.Sprintf("Storage (%s)", strings.ToLower(storageClass)),
		Unit:            "GiB-months",
		UnitMultiplier:  1,
		MonthlyQuantity: quantity,
		ProductFilter: &schema.ProductFilter{
			VendorName: strPtr("gcp"),
			Region:     strPtr(region),
			Service:    strPtr("Cloud Storage"),
			AttributeFilters: []*schema.AttributeFilter{
				{Key: "resourceGroup", Value: strPtr(resourceGroup)},
				{Key: "description", ValueRegex: strPtr("/^(?!.*?\\(Early Delete\\))/")},
			},
		},
		PriceFilter: &schema.PriceFilter{
			EndUsageAmount: strPtr(""), // use the non-free tier
		},
	}
}

func operations(d *schema.ResourceData, u *schema.UsageData) []*schema.CostComponent {
	var classAQuantity *decimal.Decimal
	if u != nil && u.Get("monthly_class_a_operations").Exists() {
		classAQuantity = decimalPtr(decimal.NewFromInt(u.Get("monthly_class_a_operations").Int()))
	}
	var classBQuantity *decimal.Decimal
	if u != nil && u.Get("monthly_class_b_operations").Exists() {
		classBQuantity = decimalPtr(decimal.NewFromInt(u.Get("monthly_class_b_operations").Int()))
	}
	storageClass := "STANDARD"
	if d.Get("storage_class").Exists() {
		storageClass = d.Get("storage_class").String()
	}

	storageClassResourceGroupMap := map[string]string{
		"STANDARD":       "RegionalOps",
		"REGIONAL":       "RegionalOps",
		"MULTI_REGIONAL": "MultiRegionalOps",
		"NEARLINE":       "NearlineOps",
		"COLDLINE":       "ColdlineOps",
		"ARCHIVE":        "ArchiveOps",
	}

	return []*schema.CostComponent{
		{
			Name:            "Object adds, bucket/object list (class A)",
			Unit:            "operations",
			UnitMultiplier:  10000,
			MonthlyQuantity: classAQuantity,
			ProductFilter: &schema.ProductFilter{
				VendorName: strPtr("gcp"),
				Service:    strPtr("Cloud Storage"),
				AttributeFilters: []*schema.AttributeFilter{
					{Key: "resourceGroup", Value: strPtr(storageClassResourceGroupMap[storageClass])},
					{Key: "description", ValueRegex: strPtr("/Class A/")},
				},
			},
		},
		{
			Name:            "Object gets, retrieve bucket/object metadata (class B)",
			Unit:            "operations",
			UnitMultiplier:  10000,
			MonthlyQuantity: classBQuantity,
			ProductFilter: &schema.ProductFilter{
				VendorName: strPtr("gcp"),
				Service:    strPtr("Cloud Storage"),
				AttributeFilters: []*schema.AttributeFilter{
					{Key: "resourceGroup", Value: strPtr(storageClassResourceGroupMap[storageClass])},
					{Key: "description", ValueRegex: strPtr("/Class B/")},
				},
			},
		},
	}
}

func dataRetrieval(d *schema.ResourceData, u *schema.UsageData) *schema.CostComponent {
	var quantity *decimal.Decimal
	if u != nil && u.Get("monthly_data_retrieval_gb").Exists() {
		quantity = decimalPtr(decimal.NewFromInt(u.Get("monthly_data_retrieval_gb").Int()))
	}

	storageClass := "STANDARD"
	if d.Get("storage_class").Exists() {
		storageClass = d.Get("storage_class").String()
	}

	storageClassResourceGroupMap := map[string]string{
		"NEARLINE": "NearlineOps",
		"COLDLINE": "ColdlineOps",
		"ARCHIVE":  "ArchiveOps",
	}
	resourceGroup := storageClassResourceGroupMap[storageClass]
	// Skipping standard, regional and multi-regional since they are free
	if resourceGroup == "" {
		return nil
	}

	return &schema.CostComponent{
		Name:            "Data retrieval",
		Unit:            "GB",
		UnitMultiplier:  1,
		MonthlyQuantity: quantity,
		ProductFilter: &schema.ProductFilter{
			VendorName: strPtr("gcp"),
			Service:    strPtr("Cloud Storage"),
			AttributeFilters: []*schema.AttributeFilter{
				{Key: "resourceGroup", Value: strPtr(resourceGroup)},
				{Key: "description", ValueRegex: strPtr("/Retrieval/")},
			},
		},
	}
}

// TODO: Move to a separate resource, similar to what we did for AWS, also needs other data transfer prices

// infracost-usage-example.yml for this
// monthly_egress_data_transfer_gb:
//   same_continent: 550             # Estimated GB of data sent from Cloud Storage in the same continent per month.
//   worldwide: 12500                # Estimated GB of data sent from Cloud Storage to worldwide destinations (excluding Asia & Australia) per month.
//   asia: 1500                      # Estimated GB of data sent from Cloud Storage to Asia (excluding China, but including Hong Kong) per month.
//   china: 50                       # Estimated GB of data sent from Cloud Storage to China (excluding Hong Kong) per month.
//   australia: 250                  # Estimated GB of data sent from Cloud Storage to Australia per month.

// type storageBucketEgressRegionData struct {
// 	gRegion        string
// 	apiDescription string
// 	usageKey       string
// }

// type storageBucketEgressRegionUsageFilterData struct {
// 	usageNumber int64
// 	usageName   string
// }

// func networkEgress(d *schema.ResourceData, u *schema.UsageData) *schema.Resource {
// 	resource := &schema.Resource{
// 		Name:           "Network egress",
// 		CostComponents: []*schema.CostComponent{},
// 	}

// 	// Same continent
// 	var quantity *decimal.Decimal
// 	if u != nil && u.Get("monthly_egress_data_transfer_gb.same_continent").Exists() {
// 		quantity = decimalPtr(decimal.NewFromInt(u.Get("monthly_egress_data_transfer_gb.same_continent").Int()))
// 	}
// 	resource.CostComponents = append(resource.CostComponents, &schema.CostComponent{
// 		Name:            "Data transfer in same continent",
// 		Unit:            "GB",
// 		UnitMultiplier:  1,
// 		MonthlyQuantity: quantity,
// 		ProductFilter: &schema.ProductFilter{
// 			VendorName: strPtr("gcp"),
// 			Region:     strPtr("global"),
// 			Service:    strPtr("Cloud Storage"),
// 			AttributeFilters: []*schema.AttributeFilter{
// 				{Key: "description", Value: strPtr("Inter-region GCP Storage egress within EU")},
// 			},
// 		},
// 	})

// 	// General
// 	regionsData := []*storageBucketEgressRegionData{
// 		{
// 			gRegion:        "Data transfer to worldwide excluding Asia, Australia",
// 			apiDescription: "Download Worldwide Destinations (excluding Asia & Australia)",
// 			usageKey:       "monthly_egress_data_transfer_gb.worldwide",
// 		},
// 		{
// 			gRegion:        "Data transfer to Asia excluding China, but including Hong Kong",
// 			apiDescription: "Download APAC",
// 			usageKey:       "monthly_egress_data_transfer_gb.asia",
// 		},
// 		{
// 			gRegion:        "Data transfer to China excluding Hong Kong",
// 			apiDescription: "Download China",
// 			usageKey:       "monthly_egress_data_transfer_gb.china",
// 		},
// 		{
// 			gRegion:        "Data transfer to Australia",
// 			apiDescription: "Download Australia",
// 			usageKey:       "monthly_egress_data_transfer_gb.australia",
// 		},
// 	}
// 	usageFiltersData := []*storageBucketEgressRegionUsageFilterData{
// 		{
// 			usageName:   "first 1TB",
// 			usageNumber: 1024,
// 		},
// 		{
// 			usageName:   "next 9TB",
// 			usageNumber: 10240,
// 		},
// 		{
// 			usageName:   "over 10TB",
// 			usageNumber: 0,
// 		},
// 	}
// 	for _, regData := range regionsData {
// 		gRegion := regData.gRegion
// 		apiDescription := regData.apiDescription
// 		usageKey := regData.usageKey

// 		var usage int64
// 		var used int64
// 		var lastEndUsageAmount int64
// 		if u != nil && u.Get(usageKey).Exists() {
// 			usage = u.Get(usageKey).Int()
// 		}

// 		for idx, usageFilter := range usageFiltersData {
// 			usageName := usageFilter.usageName
// 			endUsageAmount := usageFilter.usageNumber
// 			var quantity *decimal.Decimal
// 			if endUsageAmount != 0 && usage >= endUsageAmount {
// 				used = endUsageAmount - used
// 				lastEndUsageAmount = endUsageAmount
// 				quantity = decimalPtr(decimal.NewFromInt(used))
// 			} else if usage > lastEndUsageAmount {
// 				used = usage - lastEndUsageAmount
// 				lastEndUsageAmount = endUsageAmount
// 				quantity = decimalPtr(decimal.NewFromInt(used))
// 			}
// 			var usageFilter string
// 			if endUsageAmount != 0 {
// 				usageFilter = fmt.Sprint(endUsageAmount)
// 			} else {
// 				usageFilter = ""
// 			}
// 			if quantity == nil && idx > 0 {
// 				continue
// 			}
// 			resource.CostComponents = append(resource.CostComponents, &schema.CostComponent{
// 				Name:            fmt.Sprintf("%v (%v)", gRegion, usageName),
// 				Unit:            "GB",
// 				UnitMultiplier:  1,
// 				MonthlyQuantity: quantity,
// 				ProductFilter: &schema.ProductFilter{
// 					VendorName: strPtr("gcp"),
// 					Service:    strPtr("Cloud Storage"),
// 					AttributeFilters: []*schema.AttributeFilter{
// 						{Key: "description", Value: strPtr(apiDescription)},
// 					},
// 				},
// 				PriceFilter: &schema.PriceFilter{
// 					EndUsageAmount: strPtr(usageFilter),
// 				},
// 			})
// 		}
// 	}

// 	return resource
// }
