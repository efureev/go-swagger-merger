package merger

func mergeComponents(origin, items map[string]interface{}) map[string]interface{} {
	for key, item := range items {
		switch key {
		case `schemas`:
			originSchemas, ok := origin[`schemas`].(map[string]interface{})
			if !ok {
				originSchemas = map[string]interface{}{}
			}

			origin[`schemas`] = mergeComponentsSchemas(originSchemas, item.(map[string]interface{}))

			continue
		case `responses`:
			originResponses, ok := origin[`responses`].(map[string]any)
			if !ok {
				originResponses = map[string]any{}
			}

			origin[`responses`] = mergeComponentsResponses(originResponses, item.(map[string]interface{}))

			continue
		}

		origin[key] = item
	}

	return origin
}

func mergeComponentsSchemas(origin, schemas map[string]any) map[string]any {
	for k, schema := range schemas {
		if _, ok := origin[k]; !ok {
			origin[k] = schema
		}
	}

	return origin
}

func mergeComponentsResponses(origin map[string]interface{}, responses map[string]interface{}) map[string]interface{} {
	for k, response := range responses {
		kStr := ToString(k)
		if _, ok := origin[kStr]; !ok {
			origin[kStr] = response
		}
	}

	return origin
}
