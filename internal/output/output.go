package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/infracost/infracost/internal/providers/terraform"
	"github.com/infracost/infracost/internal/schema"
	"github.com/shopspring/decimal"
)

type Root struct {
	Resources        []Resource       `json:"resources"`        // Keeping for backward compatibility.
	TotalHourlyCost  *decimal.Decimal `json:"totalHourlyCost"`  // Keeping for backward compatibility.
	TotalMonthlyCost *decimal.Decimal `json:"totalMonthlyCost"` // Keeping for backward compatibility.
	Projects         []Project        `json:"projects"`
	TimeGenerated    time.Time        `json:"timeGenerated"`
	ResourceSummary  *ResourceSummary `json:"resourceSummary"`
}

type Project struct {
	PastBreakdown *Breakdown `json:"pastBreakdown"`
	Breakdown     *Breakdown `json:"breakdown"`
	Diff          *Breakdown `json:"diff"`
}

type Breakdown struct {
	Resources        []Resource       `json:"resources"`
	TotalHourlyCost  *decimal.Decimal `json:"totalHourlyCost"`
	TotalMonthlyCost *decimal.Decimal `json:"totalMonthlyCost"`
}

type CostComponent struct {
	Name            string           `json:"name"`
	Unit            string           `json:"unit"`
	HourlyQuantity  *decimal.Decimal `json:"hourlyQuantity"`
	MonthlyQuantity *decimal.Decimal `json:"monthlyQuantity"`
	Price           decimal.Decimal  `json:"price"`
	HourlyCost      *decimal.Decimal `json:"hourlyCost"`
	MonthlyCost     *decimal.Decimal `json:"monthlyCost"`
}

type Resource struct {
	Name           string            `json:"name"`
	Tags           map[string]string `json:"tags,omitempty"`
	Metadata       map[string]string `json:"metadata"`
	HourlyCost     *decimal.Decimal  `json:"hourlyCost"`
	MonthlyCost    *decimal.Decimal  `json:"monthlyCost"`
	CostComponents []CostComponent   `json:"costComponents,omitempty"`
	SubResources   []Resource        `json:"subresources,omitempty"`
}

type ResourceSummary struct {
	SupportedCounts   *map[string]int `json:"supportedCounts,omitempty"`
	UnsupportedCounts *map[string]int `json:"unsupportedCounts,omitempty"`
	TotalSupported    *int            `json:"totalSupported,omitempty"`
	TotalUnsupported  *int            `json:"totalUnsupported,omitempty"`
	TotalNoPrice      *int            `json:"totalNoPrice,omitempty"`
	Total             *int            `json:"total,omitempty"`
}

type ResourceSummaryOptions struct {
	IncludeUnsupportedProviders bool
	OnlyFields                  []string
}

type Options struct {
	NoColor     bool
	ShowSkipped bool
	GroupLabel  string
	GroupKey    string
}

type ReportInput struct {
	Metadata map[string]string
	Root     Root
}

func outputBreakdown(resources []*schema.Resource) *Breakdown {
	arr := make([]Resource, 0, len(resources))

	for _, r := range resources {
		if r.IsSkipped {
			continue
		}
		arr = append(arr, outputResource(r))
	}

	sortResources(arr)

	totalMonthlyCost, totalHourlyCost := calculateTotalCosts(arr)

	return &Breakdown{
		Resources:        arr,
		TotalHourlyCost:  totalMonthlyCost,
		TotalMonthlyCost: totalHourlyCost,
	}
}

func outputResource(r *schema.Resource) Resource {
	comps := make([]CostComponent, 0, len(r.CostComponents))
	for _, c := range r.CostComponents {

		comps = append(comps, CostComponent{
			Name:            c.Name,
			Unit:            c.UnitWithMultiplier(),
			HourlyQuantity:  c.UnitMultiplierHourlyQuantity(),
			MonthlyQuantity: c.UnitMultiplierMonthlyQuantity(),
			Price:           c.UnitMultiplierPrice(),
			HourlyCost:      c.HourlyCost,
			MonthlyCost:     c.MonthlyCost,
		})
	}

	subresources := make([]Resource, 0, len(r.SubResources))
	for _, s := range r.SubResources {
		subresources = append(subresources, outputResource(s))
	}

	return Resource{
		Name:           r.Name,
		Tags:           r.Tags,
		HourlyCost:     r.HourlyCost,
		MonthlyCost:    r.MonthlyCost,
		CostComponents: comps,
		SubResources:   subresources,
	}
}

func ToOutputFormat(projects []*schema.Project) Root {
	var totalMonthlyCost, totalHourlyCost *decimal.Decimal

	outProjects := make([]Project, 0, len(projects))
	outResources := make([]Resource, 0)

	for _, project := range projects {
		var pastBreakdown, breakdown, diff *Breakdown

		breakdown = outputBreakdown(project.Resources)

		if project.HasDiff {
			pastBreakdown = outputBreakdown(project.PastResources)
			diff = outputBreakdown(project.Diff)
		}

		// Backward compatibility
		if breakdown != nil {
			outResources = append(outResources, breakdown.Resources...)
		}

		if breakdown != nil && breakdown.TotalHourlyCost != nil {
			if totalHourlyCost == nil {
				totalHourlyCost = decimalPtr(decimal.Zero)
			}

			totalHourlyCost = decimalPtr(totalHourlyCost.Add(*breakdown.TotalHourlyCost))
		}

		if breakdown != nil && breakdown.TotalMonthlyCost != nil {
			if totalMonthlyCost == nil {
				totalMonthlyCost = decimalPtr(decimal.Zero)
			}

			totalMonthlyCost = decimalPtr(totalMonthlyCost.Add(*breakdown.TotalMonthlyCost))
		}

		outProjects = append(outProjects, Project{
			PastBreakdown: pastBreakdown,
			Breakdown:     breakdown,
			Diff:          diff,
		})
	}

	resourceSummary := BuildResourceSummary(schema.AllProjectResources(projects), ResourceSummaryOptions{
		OnlyFields: []string{"UnsupportedCounts"},
	})

	sortResources(outResources)

	out := Root{
		Resources:        outResources,
		TotalHourlyCost:  totalHourlyCost,
		TotalMonthlyCost: totalMonthlyCost,
		Projects:         outProjects,
		TimeGenerated:    time.Now(),
		ResourceSummary:  resourceSummary,
	}

	return out
}

func Load(data []byte) (Root, error) {
	var out Root
	err := json.Unmarshal(data, &out)
	return out, err
}

func Combine(inputs []ReportInput, opts Options) Root {
	var combined Root

	var totalHourlyCost *decimal.Decimal
	var totalMonthlyCost *decimal.Decimal
	summaries := make([]*ResourceSummary, 0, len(inputs))

	for _, input := range inputs {

		for _, r := range input.Root.Resources {
			for k, v := range input.Metadata {
				if r.Metadata == nil {
					r.Metadata = make(map[string]string)
				}
				r.Metadata[k] = v
			}

			combined.Resources = append(combined.Resources, r)
		}

		if input.Root.TotalHourlyCost != nil {
			if totalHourlyCost == nil {
				totalHourlyCost = decimalPtr(decimal.Zero)
			}

			totalHourlyCost = decimalPtr(totalHourlyCost.Add(*input.Root.TotalHourlyCost))
		}
		if input.Root.TotalMonthlyCost != nil {
			if totalMonthlyCost == nil {
				totalMonthlyCost = decimalPtr(decimal.Zero)
			}

			totalMonthlyCost = decimalPtr(totalMonthlyCost.Add(*input.Root.TotalMonthlyCost))
		}

		summaries = append(summaries, input.Root.ResourceSummary)
	}

	combined.sortResources(opts.GroupKey)

	combined.TotalHourlyCost = totalHourlyCost
	combined.TotalMonthlyCost = totalMonthlyCost
	combined.TimeGenerated = time.Now()
	combined.ResourceSummary = combinedResourceSummaries(summaries)

	return combined
}

func (r *Root) sortResources(groupKey string) {
	sort.Slice(r.Resources, func(i, j int) bool {
		if r.Resources[i].Metadata[groupKey] == r.Resources[j].Metadata[groupKey] {
			return r.Resources[i].Name < r.Resources[j].Name
		}
		return r.Resources[i].Metadata[groupKey] < r.Resources[j].Metadata[groupKey]
	})
}

func (r *Root) unsupportedResourcesMessage(showSkipped bool) string {
	if r.ResourceSummary.UnsupportedCounts == nil || len(*r.ResourceSummary.UnsupportedCounts) == 0 {
		return ""
	}

	unsupportedTypeCount := len(*r.ResourceSummary.UnsupportedCounts)

	unsupportedMsg := "resource types weren't estimated as they're not supported yet"
	if unsupportedTypeCount == 1 {
		unsupportedMsg = "resource type wasn't estimated as it's not supported yet"
	}

	showSkippedMsg := ", rerun with --show-skipped to see"
	if showSkipped {
		showSkippedMsg = ""
	}

	msg := fmt.Sprintf("%d %s%s.\n%s",
		unsupportedTypeCount,
		unsupportedMsg,
		showSkippedMsg,
		"Please watch/star https://github.com/infracost/infracost as new resources are added regularly.",
	)

	if showSkipped {
		for t, c := range *r.ResourceSummary.UnsupportedCounts {
			msg += fmt.Sprintf("\n%d x %s", c, t)
		}
	}

	return msg
}

func BuildResourceSummary(resources []*schema.Resource, opts ResourceSummaryOptions) *ResourceSummary {
	supportedCounts := make(map[string]int)
	unsupportedCounts := make(map[string]int)
	totalSupported := 0
	totalUnsupported := 0
	totalNoPrice := 0

	for _, r := range resources {
		if !opts.IncludeUnsupportedProviders && !terraform.HasSupportedProvider(r.ResourceType) {
			continue
		}

		if r.NoPrice {
			totalNoPrice++
		} else if r.IsSkipped {
			totalUnsupported++
			if _, ok := unsupportedCounts[r.ResourceType]; !ok {
				unsupportedCounts[r.ResourceType] = 0
			}
			unsupportedCounts[r.ResourceType]++
		} else {
			totalSupported++
			if _, ok := supportedCounts[r.ResourceType]; !ok {
				supportedCounts[r.ResourceType] = 0
			}
			supportedCounts[r.ResourceType]++
		}
	}

	total := len(resources)

	s := &ResourceSummary{}

	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "SupportedCounts") {
		s.SupportedCounts = &supportedCounts
	}
	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "UnsupportedCounts") {
		s.UnsupportedCounts = &unsupportedCounts
	}
	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "TotalSupported") {
		s.TotalSupported = &totalSupported
	}
	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "TotalUnsupported") {
		s.TotalUnsupported = &totalUnsupported
	}
	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "TotalNoPrice") {
		s.TotalNoPrice = &totalNoPrice
	}
	if len(opts.OnlyFields) == 0 || contains(opts.OnlyFields, "Total") {
		s.Total = &total
	}

	return s
}

func combinedResourceSummaries(summaries []*ResourceSummary) *ResourceSummary {
	combined := &ResourceSummary{}

	for _, s := range summaries {
		if s == nil {
			continue
		}

		combined.SupportedCounts = combineCounts(combined.SupportedCounts, s.SupportedCounts)
		combined.UnsupportedCounts = combineCounts(combined.UnsupportedCounts, s.UnsupportedCounts)
		combined.TotalSupported = addIntPtrs(combined.TotalSupported, s.TotalSupported)
		combined.TotalUnsupported = addIntPtrs(combined.TotalUnsupported, s.TotalUnsupported)
		combined.TotalNoPrice = addIntPtrs(combined.TotalNoPrice, s.TotalNoPrice)
		combined.Total = addIntPtrs(combined.Total, s.Total)
	}

	return combined
}

func combineCounts(c1 *map[string]int, c2 *map[string]int) *map[string]int {
	if c1 == nil && c2 == nil {
		return nil
	}

	res := make(map[string]int)

	if c1 != nil {
		for k, v := range *c1 {
			res[k] = v
		}
	}

	if c2 != nil {
		for k, v := range *c2 {
			res[k] += v
		}
	}

	return &res
}

func calculateTotalCosts(resources []Resource) (*decimal.Decimal, *decimal.Decimal) {
	totalHourlyCost := decimalPtr(decimal.Zero)
	totalMonthlyCost := decimalPtr(decimal.Zero)

	for _, r := range resources {
		if r.HourlyCost != nil {
			if totalHourlyCost == nil {
				totalHourlyCost = decimalPtr(decimal.Zero)
			}

			totalHourlyCost = decimalPtr(totalHourlyCost.Add(*r.HourlyCost))
		}
		if r.MonthlyCost != nil {
			if totalMonthlyCost == nil {
				totalMonthlyCost = decimalPtr(decimal.Zero)
			}

			totalMonthlyCost = decimalPtr(totalMonthlyCost.Add(*r.MonthlyCost))
		}

	}

	return totalHourlyCost, totalMonthlyCost
}

func sortResources(resources []Resource) {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Name < resources[j].Name
	})
}

func addIntPtrs(i1 *int, i2 *int) *int {
	if i1 == nil && i2 == nil {
		return nil
	}

	val1 := 0
	if i1 != nil {
		val1 = *i1
	}

	val2 := 0
	if i2 != nil {
		val2 = *i2
	}

	res := val1 + val2
	return &res
}

func formatAmount(d decimal.Decimal) string {
	f, _ := d.Float64()
	if f < 0.00005 && f != 0 {
		return fmt.Sprintf("%.g", f)
	}

	return humanize.FormatFloat("#,###.####", f)
}

func formatCost(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return formatAmount(*d)
}

func formatQuantity(q *decimal.Decimal) string {
	if q == nil {
		return "-"
	}
	f, _ := q.Float64()
	return humanize.CommafWithDigits(f, 4)
}

func contains(arr []string, e string) bool {
	for _, a := range arr {
		if a == e {
			return true
		}
	}
	return false
}

func decimalPtr(d decimal.Decimal) *decimal.Decimal {
	return &d
}
