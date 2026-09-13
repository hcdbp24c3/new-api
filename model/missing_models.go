package model

// GetMissingModels returns configured model names that have no metadata record.
// It reads from channels.models (raw admin-entered names, typically bare) instead
// of abilities.model (which always carries the channel prefix). This ensures the
// sync model metadata wizard shows bare names that can match metadata records.
func GetMissingModels() ([]string, error) {
	// 1. Collect all configured models from channels (deduplicated, bare names).
	configured, err := GetConfiguredModelChannels()
	if err != nil {
		return nil, err
	}
	if len(configured) == 0 {
		return []string{}, nil
	}

	// 2. Query existing metadata model names.
	var existing []string
	modelNames := make([]string, 0, len(configured))
	for name := range configured {
		modelNames = append(modelNames, name)
	}
	if err := DB.Model(&Model{}).Where("model_name IN ?", modelNames).Pluck("model_name", &existing).Error; err != nil {
		return nil, err
	}

	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	// 3. Collect missing models.
	var missing []string
	for _, name := range modelNames {
		if _, ok := existingSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing, nil
}
