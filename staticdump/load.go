package staticdump

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/evepraisal/go-evepraisal"
	"github.com/evepraisal/go-evepraisal/typedb"
	"github.com/sethgrid/pester"
)

func downloadTypes(client *pester.Client, staticDumpURL string, staticDataPath string) error {
	out, err := os.Create(staticDataPath)
	defer func() {
		_ = out.Close()
	}()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", staticDumpURL, nil)
	if err != nil {
		return err
	}

	req.Header.Add("User-Agent", evepraisal.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		rmErr := os.Remove(staticDataPath)
		if rmErr != nil {
			log.Printf("ERROR: Failed to remove type db zip file after failing to fully download it: %s", rmErr)
		}
		return err
	}

	log.Printf("Successfully wrote %d bytes to %s", n, staticDataPath)
	return nil
}

// Type is an eve online type as stored in types.jsonl.
type Type struct {
	ID            int64 `json:"_key"`
	GroupID       int64 `json:"groupID"`
	MarketGroupID int64 `json:"marketGroupID"`
	Name          struct {
		En string `json:"en"`
	} `json:"name"`
	Published      bool    `json:"published"`
	Volume         float64 `json:"volume"`
	PackagedVolume float64 `json:"packagedVolume"`
	BasePrice      float64 `json:"basePrice"`
}

// BlueprintItem is a material or product line of a blueprint activity.
type BlueprintItem struct {
	Quantity int64 `json:"quantity"`
	TypeID   int64 `json:"typeID"`
}

// Blueprint is an eve online blueprint as stored in blueprints.jsonl.
type Blueprint struct {
	BlueprintTypeID int64 `json:"blueprintTypeID"`
	Activities      struct {
		Manufacturing struct {
			Materials []BlueprintItem `json:"materials"`
			Products  []BlueprintItem `json:"products"`
		} `json:"manufacturing"`
	} `json:"activities"`
}

func loadtypes(staticDataPath string) ([]typedb.EveType, error) {
	r, err := zip.OpenReader(staticDataPath)
	if err != nil {
		return nil, fmt.Errorf("unzip static data %s: %w", staticDataPath, err)
	}
	defer r.Close()

	var allTypes []Type
	err = forEachJSONLine(r, "types.jsonl", func(_ int, line []byte) error {
		var t Type
		if err := json.Unmarshal(line, &t); err != nil {
			return err
		}
		allTypes = append(allTypes, t)
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("Loaded %d types", len(allTypes))

	// File order, so that "first blueprint of a product" is deterministic.
	blueprintsByProductType := make(map[int64][]Blueprint)
	blueprintCount := 0
	err = forEachJSONLine(r, "blueprints.jsonl", func(_ int, line []byte) error {
		var bp Blueprint
		if err := json.Unmarshal(line, &bp); err != nil {
			return err
		}
		blueprintCount++
		for _, product := range bp.Activities.Manufacturing.Products {
			blueprintsByProductType[product.TypeID] = append(blueprintsByProductType[product.TypeID], bp)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	log.Printf("Loaded %d blueprints", blueprintCount)

	types := make([]typedb.EveType, 0, len(allTypes))
	for _, t := range allTypes {
		typeID := t.ID

		// Yep, there is at least one type that isn't named.
		if t.Name.En == "" {
			continue
		}

		eveType := typedb.EveType{
			ID:                typeID,
			GroupID:           t.GroupID,
			MarketGroupID:     t.MarketGroupID,
			Name:              strings.TrimSpace(t.Name.En),
			Volume:            t.Volume,
			PackagedVolume:    t.PackagedVolume,
			BasePrice:         t.BasePrice,
			Aliases:           []string{},
			BlueprintProducts: resolveBlueprintProducts(blueprintsByProductType, typeID),
			Components:        resolveComponents(blueprintsByProductType, typeID),
			BaseComponents:    flattenComponents(resolveBaseComponents(blueprintsByProductType, typeID, 1, 5)),
		}

		name, aliases := computeAliases(typeID, eveType.Name)
		eveType.Name = name
		eveType.Aliases = aliases

		types = append(types, eveType)
	}

	return types, nil
}

func resolveBlueprintProducts(blueprintsByProductType map[int64][]Blueprint, typeID int64) []typedb.Component {
	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Products {
		components = append(components, typedb.Component{Quantity: material.Quantity, TypeID: material.TypeID})
	}
	return components
}

func resolveComponents(blueprintsByProductType map[int64][]Blueprint, typeID int64) []typedb.Component {
	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Materials {
		components = append(components, typedb.Component{Quantity: material.Quantity, TypeID: material.TypeID})
	}
	return components
}

func flattenComponents(components []typedb.Component) []typedb.Component {
	m := make(map[typedb.Component]int64)
	for _, component := range components {
		qty := component.Quantity
		component.Quantity = 0
		m[component] += qty
	}

	s := make([]typedb.Component, 0, len(m))
	for component, qty := range m {
		component.Quantity = qty
		s = append(s, component)
	}
	return s
}

func resolveBaseComponents(blueprintsByProductType map[int64][]Blueprint, typeID int64, multiplier int64, left int) []typedb.Component {
	if left == 0 {
		return nil
	}

	blueprints, ok := blueprintsByProductType[typeID]
	if !ok || len(blueprints) == 0 {
		return nil
	}

	bp := blueprints[0]
	var components []typedb.Component
	for _, material := range bp.Activities.Manufacturing.Materials {
		r := resolveBaseComponents(blueprintsByProductType, material.TypeID, material.Quantity*multiplier, left-1)
		if r == nil {
			components = append(components, typedb.Component{Quantity: material.Quantity * multiplier, TypeID: material.TypeID})
		} else {
			components = append(components, r...)
		}
	}
	return components
}
