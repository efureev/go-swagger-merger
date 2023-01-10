package merger

var servers = make(map[string]bool)

func mergeServers(origin, items []interface{}) []interface{} {
	for _, item := range items {
		if server, ok := item.(map[string]interface{}); ok {
			url := server[`url`].(string)
			if _, ok := servers[url]; !ok {
				servers[url] = true
				origin = append(origin, item)
			}
		}
	}

	return origin
}
