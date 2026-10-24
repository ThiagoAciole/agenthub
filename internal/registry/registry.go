package registry

import (
	"github.com/ThiagoAciole/agenthub/internal/discovery"
	"github.com/ThiagoAciole/agenthub/internal/models"
	"sort"
)

func Build(items []models.Item) models.Inventory {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Provider != items[j].Provider {
			return items[i].Provider < items[j].Provider
		}
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Path < items[j].Path
	})
	byHash := map[string][]int{}
	for i, item := range items {
		if item.Type == models.MCP {
			continue
		}
		h, err := discovery.Hash(item.Path)
		if err == nil {
			byHash[h] = append(byHash[h], i)
		}
	}
	groups := []models.DuplicateGroup{}
	for h, indexes := range byHash {
		if len(indexes) < 2 {
			continue
		}
		paths := []string{}
		for _, i := range indexes {
			paths = append(paths, items[i].Path)
		}
		sort.Strings(paths)
		groups = append(groups, models.DuplicateGroup{Hash: h, Paths: paths})
		for _, i := range indexes {
			items[i].Duplicates = []string{}
			for _, path := range paths {
				if path != items[i].Path {
					items[i].Duplicates = append(items[i].Duplicates, path)
				}
			}
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Hash < groups[j].Hash })
	return models.Inventory{Items: items, DuplicateGroups: groups}
}
