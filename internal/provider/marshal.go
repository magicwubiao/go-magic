package provider

import (
	"encoding/json"
)

// marshalArgs marshals a map of arguments to a JSON string
func marshalArgs(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}
