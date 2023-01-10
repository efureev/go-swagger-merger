package merger

var tags = make(map[string]bool)

func mergeTags(origin, items []interface{}) []interface{} {
	for _, item := range items {
		if tag, ok := item.(map[string]interface{}); ok {
			name := tag[`name`].(string)
			if _, ok := tags[name]; !ok {
				tags[name] = true
				origin = append(origin, item)
			}
		}
	}

	return origin
}
